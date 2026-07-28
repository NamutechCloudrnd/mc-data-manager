package controllers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cloud-barista/mc-data-manager/config"
	"github.com/cloud-barista/mc-data-manager/internal/auth"
	"github.com/cloud-barista/mc-data-manager/models"
	"github.com/cloud-barista/mc-data-manager/pkg/rdbms/mysql/diagnostics"
	"github.com/cloud-barista/mc-data-manager/pkg/sysbench"
	"github.com/cloud-barista/mc-data-manager/pkg/warp"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
)

type DiagnoseHandler struct {
}

func NewDiagnoseHandler() *DiagnoseHandler {
	return &DiagnoseHandler{}
}

func (d *DiagnoseHandler) PostStatusDiagnose(ctx echo.Context) error {
	start := time.Now()
	logger, logstrings := pageLogInit(ctx, "Diagnose-task", "Diagnose MySQL status", start)
	params := models.DiagnosticTask{}
	if !getDataWithReBind(logger, start, ctx, &params) {
		errStr := "Invalid request data"
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, models.DiagnoseResponse{
			Result: logstrings.String(),
			Error:  &errStr,
		})
	}

	rdb, err := auth.GetRDMS(&params.TargetPoint)
	if err != nil {
		errStr := "Invalid request data"
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, models.DiagnoseResponse{
			Result: logstrings.String(),
			Error:  &errStr,
		})
	}

	result, err := rdb.Client.Diagnose(params.TargetPoint.DatabaseName, params.Time)
	if err != nil {
		errStr := "failed to diagnose"
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, models.DiagnoseResponse{
			Result: logstrings.String(),
			Error:  &errStr,
		})
	}

	logDiagnose(logger, result)

	return ctx.JSON(http.StatusOK, models.DiagnoseResponse{
		Result:      logstrings.String(),
		Diagnostics: result,
		Error:       nil,
	})
}

func (d *DiagnoseHandler) PostSysbenchDiagnose(ctx echo.Context) error {
	start := time.Now()
	logger, logstrings := pageLogInit(ctx, "Diagnose-task", "Diagnose MySQL sysbench", start)
	params := models.DiagnosticTask{}
	if !getDataWithReBind(logger, start, ctx, &params) {
		errStr := "Invalid request data"
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, models.SysbenchResponse{
			Result: logstrings.String(),
			Error:  &errStr,
		})
	}

	rdb, err := auth.GetRDMS(&models.ProviderConfig{
		MySQLParams: models.MySQLParams{
			Host:     params.Host,
			Port:     params.Port,
			User:     params.User,
			Password: params.Password,
		},
	})
	if err != nil {
		errStr := "Invalid request data"
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, models.SysbenchResponse{
			Result: logstrings.String(),
			Error:  &errStr,
		})
	}

	// Run the benchmark against a dedicated, throwaway database so a bad
	// databaseName can't fail sysbench and can't touch real data.
	dbName := fmt.Sprintf("sysbench_diag_%d", time.Now().UnixNano())
	if err := rdb.Put(fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
		errStr := "failed to create temporary database: " + err.Error()
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, models.SysbenchResponse{
			Result: logstrings.String(),
			Error:  &errStr,
		})
	}
	defer func() {
		if err := rdb.DeleteDB(dbName); err != nil {
			logger.Error().Err(err).Msgf("failed to drop temporary database: %s", dbName)
		}
	}()

	_, prepareErr := sysbench.RunSysbench(ctx.Request().Context(),
		"mysql",
		false,
		"oltp_read_write",
		"--db-driver=mysql",
		"--mysql-host="+params.Host,
		"--mysql-port="+params.Port,
		"--mysql-user="+params.User,
		"--mysql-password="+params.Password,
		"--mysql-db="+dbName,
		fmt.Sprintf("--tables=%d", params.TableCount),
		fmt.Sprintf("--table-size=%d", params.TableSize),
		fmt.Sprintf("--threads=%d", params.ThreadsCount),
		fmt.Sprintf("--time=%d", params.Time),
		"prepare",
	)
	if prepareErr != nil {
		errStr := prepareErr.Error()
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, models.SysbenchResponse{
			Result: logstrings.String(),
			Error:  &errStr,
		})
	}

	// 예: sysbench oltp_read_write --threads=8 --time=10 --report-interval=1 run
	res, err := sysbench.RunSysbench(ctx.Request().Context(),
		"mysql",
		true,
		"oltp_read_write",
		"--db-driver=mysql",
		"--mysql-host="+params.Host,
		"--mysql-port="+params.Port,
		"--mysql-user="+params.User,
		"--mysql-password="+params.Password,
		"--mysql-db="+dbName,
		fmt.Sprintf("--tables=%d", params.TableCount),
		fmt.Sprintf("--table-size=%d", params.TableSize),
		fmt.Sprintf("--threads=%d", params.ThreadsCount),
		fmt.Sprintf("--time=%d", params.Time),
		"run",
	)
	if err != nil {
		errStr := err.Error()
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, models.SysbenchResponse{
			Result: logstrings.String(),
			Error:  &errStr,
		})
	}

	logSysbench(logger, res)

	return ctx.JSON(http.StatusOK, models.SysbenchResponse{
		Result:         logstrings.String(),
		SysbenchResult: res,
		Error:          nil,
	})
}

func (d *DiagnoseHandler) PostWarpDiagnose(ctx echo.Context) error {
	start := time.Now()
	logger, _ := pageLogInit(ctx, "Diagnose-task", "Diagnose object storage warp", start)

	req := models.WarpDiagnosticRequest{}
	if !getDataWithReBind(logger, start, ctx, &req) {
		errStr := "Invalid request data"
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": errStr})
	}

	creds, err := config.NewAuthManager().LoadCredentialsByProvider(ctx.Request().Context(), req.Provider)
	if err != nil {
		errStr := "credential load failed: " + err.Error()
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": errStr})
	}

	accessKey, secretKey, err := warp.ResolveS3Keys(req.Provider, creds)
	if err != nil {
		errStr := err.Error()
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": errStr})
	}

	host, err := warp.Endpoint(req.Provider, req.Region)
	if err != nil {
		errStr := err.Error()
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": errStr})
	}

	bucketName, err := warp.ResolveBucketName(req.Provider, accessKey, secretKey, req.BucketId)
	if err != nil {
		errStr := err.Error()
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": errStr})
	}

	params := warp.RunParams{
		Host:        host,
		Region:      req.Region,
		AccessKey:   accessKey,
		SecretKey:   secretKey,
		Bucket:      bucketName,
		Prefix:      fmt.Sprintf("warp_diag_%d/", time.Now().UnixNano()),
		Duration:    req.DurationSec,
		ObjectSize:  req.ObjectKib,
		ObjectCount: req.ObjectCount,
		ExtraArgs:   warp.ExtraArgs(req.Provider),
	}

	defer func() {
		go func() {
			deleteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := warp.DeletePrefix(deleteCtx, params)
			if err != nil {
				logger.Error().Err(err).Msg("failed to clean up warp benchmark prefix")
			} else {
				logger.Info().Msg("Successfully delete benchmark prefix")
			}
		}()
	}()

	out, err := warp.RunWarp(ctx.Request().Context(), params)
	if err != nil {
		errStr := err.Error()
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": errStr})
	}

	result, err := warp.ParseWarpOutput(out)
	if err != nil {
		errStr := err.Error()
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": errStr})
	}
	logger.Info().Msg(result.Raw)

	jobEnd(logger, "Successfully diagnosed object storage", start)
	return ctx.JSON(http.StatusOK, result)
}

func logDiagnose(logger *zerolog.Logger, result diagnostics.TimedResult) {
	logger.Info().Msg(diagnostics.PrintBufferReport(result.Buffer))
	logger.Info().Msg(diagnostics.PrintThreadReport(result.Thread))
	logger.Info().Msg(diagnostics.PrintLockReport(result.Lock, result.Elapsed))
	logger.Info().Msg(diagnostics.PrintIOReport(result.IO, result.Elapsed))
	logger.Info().Msg(diagnostics.PrintWorkloadReport(result.Work, result.Elapsed))
	logger.Info().Msg(result.Report.String())
}

func logSysbench(logger *zerolog.Logger, result sysbench.SysbenchParsed) {
	logger.Info().Msg(result.FormatSysbenchLike())
}

func (d *DiagnoseHandler) PostConnectionHandler(ctx echo.Context) error {
	start := time.Now()
	logger, logstrings := pageLogInit(ctx, "Connection-test", "Check MySQL connection", start)
	params := models.DiagnosticTask{}
	if !getDataWithReBind(logger, start, ctx, &params) {
		errStr := "Invalid request data"
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, models.BasicResponse{
			Result: logstrings.String(),
			Error:  &errStr,
		})
	}

	_, err := auth.GetRDMS(&params.TargetPoint)
	if err != nil {
		errStr := "Invalid request data"
		logger.Error().Msg(errStr)
		return ctx.JSON(http.StatusBadRequest, models.BasicResponse{
			Result: logstrings.String(),
			Error:  &errStr,
		})
	}

	return ctx.JSON(http.StatusOK, models.BasicResponse{
		Result: logstrings.String(),
		Error:  nil,
	})
}

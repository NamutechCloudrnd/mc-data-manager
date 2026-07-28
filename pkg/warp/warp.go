package warp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloud-barista/mc-data-manager/models"
)

// RunParams holds the fully-resolved values needed to invoke the warp binary.
type RunParams struct {
	Host        string
	AccessKey   string
	SecretKey   string
	Bucket      string
	Prefix      string
	Duration    int64
	ObjectSize  int64 // in KB
	ObjectCount int64
	ExtraArgs   []string
}

// RunWarp executes a warp mixed benchmark with the given parameters and returns its combined output.
func RunWarp(ctx context.Context, params RunParams) ([]byte, error) {
	args := []string{
		"mixed",
		"--host=" + params.Host,
		"--access-key=" + params.AccessKey,
		"--secret-key=" + params.SecretKey,
		"--bucket=" + params.Bucket,
		"--prefix=" + params.Prefix,
		"--noclear",
		"--concurrent=4",
		"--analyze.v",
		"--benchdata=/",
		fmt.Sprintf("--duration=%ds", params.Duration),
		fmt.Sprintf("--obj.size=%dKiB", params.ObjectSize),
		fmt.Sprintf("--objects=%d", params.ObjectCount),
	}
	args = append(args, params.ExtraArgs...)

	cmd := exec.CommandContext(ctx, "warp", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("warp failed: %w; output: %s", err, string(out))
	}
	return out, nil
}

var (
	reportHeaderRe = regexp.MustCompile(`^Report: (\S+) \((\d+) reqs\)\. Ran Duration: (\d+)s,`)
	objectsLineRe  = regexp.MustCompile(`Objects per request: (\d+)\.(?: Size: (\d+ bytes)\.)? Concurrency: (\d+)\.`)
	reqsLineRe     = regexp.MustCompile(`Reqs: Avg: ([\d.]+)ms, 50%: ([\d.]+)ms, 90%: ([\d.]+)ms, 99%: ([\d.]+)ms, Fastest: ([\d.]+)ms, Slowest: ([\d.]+)ms, StdDev: ([\d.]+)ms`)
	ttfbLineRe     = regexp.MustCompile(`TTFB: Avg: ([\d.]+)ms, Best: ([\d.]+)ms, 25th: ([\d.]+)ms, Median: ([\d.]+)ms, 75th: ([\d.]+)ms, 90th: ([\d.]+)ms, 99th: ([\d.]+)ms, Worst: ([\d.]+)ms StdDev: ([\d.]+)ms`)
	objPSRe        = regexp.MustCompile(`(?:([\d.]+\s*(?:B|KiB|MiB|GiB|TiB)/s), )?([\d.]+) obj/s`)
)

// ParseWarpOutput parses warp's "Report: <OP>" stdout summary (run with --analyze.v)
// into one WarpOperationReport per operation. The "Report: Total" block is skipped.
func ParseWarpOutput(out []byte) (models.WarpParsed, error) {
	parsed := models.WarpParsed{Raw: string(out)}

	var current *models.WarpOperationReport
	skip := false
	inThroughputSplit := false

	flush := func() {
		if current != nil && !skip {
			parsed.Operations = append(parsed.Operations, *current)
		}
		current = nil
	}

	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		if m := reportHeaderRe.FindStringSubmatch(line); m != nil {
			flush()
			op := m[1]
			skip = op == "Total"
			inThroughputSplit = false
			if skip {
				continue
			}
			reqs, _ := strconv.ParseInt(m[2], 10, 64)
			dur, _ := strconv.ParseInt(m[3], 10, 64)
			current = &models.WarpOperationReport{
				Operation:   op,
				Requests:    reqs,
				DurationSec: dur,
			}
			continue
		}

		if skip || current == nil {
			continue
		}

		if strings.HasPrefix(line, "Throughput, split into") {
			inThroughputSplit = true
			continue
		}

		trimmed := strings.TrimPrefix(line, "* ")

		switch {
		case strings.Contains(line, "Objects per request:"):
			if m := objectsLineRe.FindStringSubmatch(line); m != nil {
				current.ObjectsPerReq, _ = strconv.ParseInt(m[1], 10, 64)
				current.Size = m[2]
				current.Concurrency, _ = strconv.ParseInt(m[3], 10, 64)
			}
		case !inThroughputSplit && strings.HasPrefix(trimmed, "Average:"):
			if m := objPSRe.FindStringSubmatch(line); m != nil {
				current.AvgThroughput = m[1]
				current.AvgObjPS, _ = strconv.ParseFloat(m[2], 64)
			}
		case strings.HasPrefix(trimmed, "Reqs:"):
			if m := reqsLineRe.FindStringSubmatch(line); m != nil {
				current.ReqAvgMs, _ = strconv.ParseFloat(m[1], 64)
				current.ReqP50Ms, _ = strconv.ParseFloat(m[2], 64)
				current.ReqP90Ms, _ = strconv.ParseFloat(m[3], 64)
				current.ReqP99Ms, _ = strconv.ParseFloat(m[4], 64)
				current.ReqFastestMs, _ = strconv.ParseFloat(m[5], 64)
				current.ReqSlowestMs, _ = strconv.ParseFloat(m[6], 64)
				current.ReqStdDevMs, _ = strconv.ParseFloat(m[7], 64)
			}
		case strings.HasPrefix(trimmed, "TTFB:"):
			if m := ttfbLineRe.FindStringSubmatch(line); m != nil {
				current.TTFBAvgMs, _ = strconv.ParseFloat(m[1], 64)
				current.TTFBBestMs, _ = strconv.ParseFloat(m[2], 64)
				current.TTFBP25Ms, _ = strconv.ParseFloat(m[3], 64)
				current.TTFBMedianMs, _ = strconv.ParseFloat(m[4], 64)
				current.TTFBP75Ms, _ = strconv.ParseFloat(m[5], 64)
				current.TTFBP90Ms, _ = strconv.ParseFloat(m[6], 64)
				current.TTFBP99Ms, _ = strconv.ParseFloat(m[7], 64)
				current.TTFBWorstMs, _ = strconv.ParseFloat(m[8], 64)
				current.TTFBStdDevMs, _ = strconv.ParseFloat(m[9], 64)
			}
		case inThroughputSplit && strings.HasPrefix(trimmed, "Fastest:"):
			if m := objPSRe.FindStringSubmatch(line); m != nil {
				current.ThroughputFastest = m[1]
				current.ThroughputFastestObjPS, _ = strconv.ParseFloat(m[2], 64)
			}
		case inThroughputSplit && strings.HasPrefix(trimmed, "50% Median:"):
			if m := objPSRe.FindStringSubmatch(line); m != nil {
				current.ThroughputMedian = m[1]
				current.ThroughputMedianObjPS, _ = strconv.ParseFloat(m[2], 64)
			}
		case inThroughputSplit && strings.HasPrefix(trimmed, "Slowest:"):
			if m := objPSRe.FindStringSubmatch(line); m != nil {
				current.ThroughputSlowest = m[1]
				current.ThroughputSlowestObjPS, _ = strconv.ParseFloat(m[2], 64)
			}
		}
	}
	flush()

	if err := sc.Err(); err != nil {
		return parsed, err
	}
	if len(parsed.Operations) == 0 {
		return parsed, fmt.Errorf("parse failed: no recognizable metrics in warp output")
	}
	return parsed, nil
}

// DeletePrefix removes all objects under params.Prefix in params.Bucket. Not yet implemented.
func DeletePrefix(ctx context.Context, params RunParams) error {
	return nil
}

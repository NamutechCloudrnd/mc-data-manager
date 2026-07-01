// Package ncp implements rdbinstance.Provider for NCP Cloud DB for MySQL.
//
// NCP Cloud DB only supports MySQL. MariaDB is not available.
// DeleteInstance takes the CloudMysqlInstanceNo (numeric string returned by
// ListInstances / CreateInstance) as the instanceID.
package ncp

import (
	"context"
	"fmt"

	"github.com/NaverCloudPlatform/ncloud-sdk-go-v2/ncloud"
	vmysql "github.com/NaverCloudPlatform/ncloud-sdk-go-v2/services/vmysql"
	"github.com/cloud-barista/mc-data-manager/config"
	"github.com/cloud-barista/mc-data-manager/models"
	"github.com/cloud-barista/mc-data-manager/pkg/rdbinstance"
)

// NCPProvider implements rdbinstance.Provider for NCP Cloud DB for MySQL.
type NCPProvider struct {
	api    *vmysql.V2ApiService
	region string
}

// New builds an NCP Cloud DB provider from static credentials and a region.
func New(accessKey, secretKey, region string) (rdbinstance.Provider, error) {
	api, err := config.NewNCPCloudMysqlClient(accessKey, secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create NCP Cloud MySQL client: %w", err)
	}
	return &NCPProvider{api: api, region: region}, nil
}

// normalizeStatus maps NCP CloudMysqlInstanceStatusName to the unified status.
func normalizeStatus(s string) string {
	switch s {
	case "running":
		return "available"
	// case "creating", "init", "setting":
	// 	return "creating"
	case "deleting", "terminating":
		return "deleting"
	default:
		return s
	}
}

// DeleteInstance deletes an NCP Cloud DB for MySQL instance.
// instanceID must be the CloudMysqlInstanceNo (numeric string).
func (p *NCPProvider) DeleteInstance(_ context.Context, instanceID string) (models.DBInstance, error) {
	resp, err := p.api.DeleteCloudMysqlInstance(&vmysql.DeleteCloudMysqlInstanceRequest{
		RegionCode:           ncloud.String(p.region),
		CloudMysqlInstanceNo: ncloud.String(instanceID),
	})
	if err != nil {
		return models.DBInstance{}, fmt.Errorf("failed to delete NCP Cloud MySQL instance: %w", err)
	}

	name := instanceID
	if resp != nil && len(resp.CloudMysqlInstanceList) > 0 {
		if sn := resp.CloudMysqlInstanceList[0].CloudMysqlServiceName; sn != nil {
			name = *sn
		}
	}

	return models.DBInstance{
		Provider:   "ncp",
		InstanceID: instanceID,
		Name:       name,
		Status:     "deleting",
		Region:     p.region,
	}, nil
}

// --- Not implemented yet (interface stubs) ---

func (p *NCPProvider) ListInstances(_ context.Context) ([]models.DBInstance, error) {
	return nil, fmt.Errorf("NCP ListInstances: not implemented yet")
}

func (p *NCPProvider) CreateInstance(_ context.Context, _ rdbinstance.CreateSpec) (models.DBInstance, error) {
	return models.DBInstance{}, fmt.Errorf("NCP CreateInstance: not implemented yet")
}

func (p *NCPProvider) ListEngineVersions(_ context.Context) ([]models.DBEngineVersion, error) {
	return nil, fmt.Errorf("NCP ListEngineVersions: not implemented yet")
}

func (p *NCPProvider) ListInstanceClasses(_ context.Context, _, _ string) ([]string, error) {
	return nil, fmt.Errorf("NCP ListInstanceClasses: not implemented yet")
}

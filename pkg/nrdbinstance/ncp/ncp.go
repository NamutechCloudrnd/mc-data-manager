// Package ncp implements nrdbinstance.Provider for NCP Cloud DB for MongoDB.
package ncp

import (
	"context"
	"fmt"

	ncpvpc "github.com/NaverCloudPlatform/ncloud-sdk-go-v2/services/vpc"
	ncpvserver "github.com/NaverCloudPlatform/ncloud-sdk-go-v2/services/vserver"
	"github.com/cloud-barista/mc-data-manager/config"
	"github.com/cloud-barista/mc-data-manager/models"
	"github.com/cloud-barista/mc-data-manager/pkg/nrdbinstance"
)

// NCPProvider implements nrdbinstance.Provider for NCP Cloud DB for MongoDB.
type NCPProvider struct {
	vpcApi     *ncpvpc.V2ApiService
	vserverApi *ncpvserver.V2ApiService
	region     string
}

// New builds an NCP MongoDB provider from static credentials and a region.
func New(accessKey, secretKey, region string) (nrdbinstance.Provider, error) {
	vpcApi, err := config.NewNCPVPCClient(accessKey, secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create NCP VPC client: %w", err)
	}
	vserverApi, err := config.NewNCPVServerClient(accessKey, secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create NCP VServer client: %w", err)
	}
	return &NCPProvider{vpcApi: vpcApi, vserverApi: vserverApi, region: region}, nil
}

func (p *NCPProvider) ListInstances(_ context.Context) ([]models.NRDBInstance, error) {
	return nil, fmt.Errorf("NCP MongoDB ListInstances: not implemented")
}

func (p *NCPProvider) CreateInstance(_ context.Context, _ nrdbinstance.CreateSpec) (models.NRDBInstance, error) {
	return models.NRDBInstance{}, fmt.Errorf("NCP MongoDB CreateInstance: not implemented")
}

func (p *NCPProvider) DeleteInstance(_ context.Context, _ string) (models.NRDBInstance, error) {
	return models.NRDBInstance{}, fmt.Errorf("NCP MongoDB DeleteInstance: not implemented")
}

func (p *NCPProvider) ListEngineVersions(_ context.Context) ([]models.NRDBEngineVersion, error) {
	return nil, fmt.Errorf("NCP MongoDB ListEngineVersions: not implemented")
}

func (p *NCPProvider) ListInstanceClasses(_ context.Context, _ string) ([]string, error) {
	return nil, fmt.Errorf("NCP MongoDB ListInstanceClasses: not implemented")
}

// Package alibaba implements rdbinstance.Provider for Alibaba ApsaraDB RDS.
//
// Alibaba differs from AWS: the master account and the public endpoint are NOT
// created by CreateDBInstance. After the instance reaches the Running state we
// additionally call CreateAccount and AllocateInstancePublicConnection. Because
// provisioning takes minutes, those two steps run in the background (see
// background.go); CreateInstance returns as soon as the instance is requested.
package alibaba

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	alibabards "github.com/alibabacloud-go/rds-20140815/v8/client"
	"github.com/alibabacloud-go/tea/tea"
	alibabavpc "github.com/alibabacloud-go/vpc-20160428/v6/client"
	"github.com/cloud-barista/mc-data-manager/config"
	"github.com/cloud-barista/mc-data-manager/models"
	"github.com/cloud-barista/mc-data-manager/pkg/rdbinstance"
)

// supportedEngines is the set of DB engines exposed by this provider, in
// Alibaba's native casing (also used as-is in responses — no lowercase mapping).
var supportedEngines = []string{"MySQL", "MariaDB"}

// instanceCategory is fixed to single-node Basic edition, matching CreateInstance.
const instanceCategory = "Basic"

// AlibabaProvider implements rdbinstance.Provider for Alibaba RDS.
type AlibabaProvider struct {
	client    *alibabards.Client
	vpcClient *alibabavpc.Client
	region    string
}

// New builds an Alibaba RDS provider from static credentials and a region.
func New(accessKey, secretKey, region string) (rdbinstance.Provider, error) {
	client, err := config.NewAlibabaRDBClient(region, accessKey, secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create Alibaba RDS client: %w", err)
	}
	vpcClient, err := config.NewAlibabaVPCClient(region, accessKey, secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create Alibaba VPC client: %w", err)
	}
	return &AlibabaProvider{client: client, vpcClient: vpcClient, region: region}, nil
}

// vSwitchInfo holds the VSwitchId and VpcId resolved for instance creation.
type vSwitchInfo struct {
	vSwitchID string
	vpcID     string
}

// vpcCIDR and vSwitchCIDR are fixed address ranges used when creating a new VPC and
// VSwitch. They are chosen to avoid overlap with common on-premises ranges.
const (
	vpcCIDR     = "172.16.0.0/16"
	vSwitchCIDR = "172.16.0.0/24"
)

// resolveVSwitch returns the first available VSwitch in the region.
// If none exists, it creates a new VPC and VSwitch and returns those IDs.
func (p *AlibabaProvider) resolveVSwitch() (vSwitchInfo, error) {
	resp, err := p.vpcClient.DescribeVSwitches(&alibabavpc.DescribeVSwitchesRequest{
		RegionId: tea.String(p.region),
	})
	if err != nil {
		return vSwitchInfo{}, fmt.Errorf("failed to describe VSwitches: %w", err)
	}
	if resp != nil && resp.Body != nil && resp.Body.VSwitches != nil && len(resp.Body.VSwitches.VSwitch) > 0 {
		first := resp.Body.VSwitches.VSwitch[0]
		return vSwitchInfo{
			vSwitchID: tea.StringValue(first.VSwitchId),
			vpcID:     tea.StringValue(first.VpcId),
		}, nil
	}
	return p.createVPCAndVSwitch()
}

// waitVPCAvailable polls DescribeVpcs until the VPC reaches "Available" status,
// timing out after 60 seconds. VPC creation is asynchronous; creating a VSwitch
// before the VPC is available returns IncorrectVpcStatus.
func (p *AlibabaProvider) waitVPCAvailable(vpcID string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := p.vpcClient.DescribeVpcs(&alibabavpc.DescribeVpcsRequest{
			RegionId: tea.String(p.region),
			VpcId:    tea.String(vpcID),
		})
		if err != nil {
			return fmt.Errorf("failed to describe VPC status: %w", err)
		}
		if resp != nil && resp.Body != nil && resp.Body.Vpcs != nil && len(resp.Body.Vpcs.Vpc) > 0 {
			if tea.StringValue(resp.Body.Vpcs.Vpc[0].Status) == "Available" {
				return nil
			}
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("VPC %s did not reach Available status within 60s", vpcID)
}

// createVPCAndVSwitch creates a new VPC and a VSwitch inside it, returning the IDs.
// The ZoneId is resolved via VPC DescribeZones so no engine context is required.
func (p *AlibabaProvider) createVPCAndVSwitch() (vSwitchInfo, error) {
	vpcResp, err := p.vpcClient.CreateVpc(&alibabavpc.CreateVpcRequest{
		RegionId:  tea.String(p.region),
		CidrBlock: tea.String(vpcCIDR),
	})
	if err != nil {
		return vSwitchInfo{}, fmt.Errorf("failed to create VPC: %w", err)
	}
	if vpcResp == nil || vpcResp.Body == nil || tea.StringValue(vpcResp.Body.VpcId) == "" {
		return vSwitchInfo{}, fmt.Errorf("create VPC returned no VPC id")
	}
	vpcID := tea.StringValue(vpcResp.Body.VpcId)

	if err := p.waitVPCAvailable(vpcID); err != nil {
		return vSwitchInfo{}, err
	}

	zonesResp, err := p.vpcClient.DescribeZones(&alibabavpc.DescribeZonesRequest{
		RegionId: tea.String(p.region),
	})
	if err != nil {
		return vSwitchInfo{}, fmt.Errorf("failed to describe zones: %w", err)
	}
	if zonesResp == nil || zonesResp.Body == nil || zonesResp.Body.Zones == nil || len(zonesResp.Body.Zones.Zone) == 0 {
		return vSwitchInfo{}, fmt.Errorf("no zones found in region %s", p.region)
	}
	var zoneID string
	for _, z := range zonesResp.Body.Zones.Zone {
		if id := tea.StringValue(z.ZoneId); isUsableZone(id) {
			zoneID = id
			break
		}
	}
	if zoneID == "" {
		return vSwitchInfo{}, fmt.Errorf("no usable single-AZ zone found in region %s", p.region)
	}

	vswResp, err := p.vpcClient.CreateVSwitch(&alibabavpc.CreateVSwitchRequest{
		VpcId:     tea.String(vpcID),
		ZoneId:    tea.String(zoneID),
		CidrBlock: tea.String(vSwitchCIDR),
	})
	if err != nil {
		return vSwitchInfo{}, fmt.Errorf("failed to create VSwitch: %w", err)
	}
	if vswResp == nil || vswResp.Body == nil || tea.StringValue(vswResp.Body.VSwitchId) == "" {
		return vSwitchInfo{}, fmt.Errorf("create VSwitch returned no VSwitch id")
	}
	return vSwitchInfo{
		vSwitchID: tea.StringValue(vswResp.Body.VSwitchId),
		vpcID:     vpcID,
	}, nil
}

// buildCreateRequest maps a CSP-agnostic CreateSpec to an Alibaba
// CreateDBInstanceRequest. Network type, pay type and the security IP list are
// fixed here (not taken from the request body).
func buildCreateRequest(spec rdbinstance.CreateSpec, region string, vsw vSwitchInfo) *alibabards.CreateDBInstanceRequest {
	return &alibabards.CreateDBInstanceRequest{
		Engine:                tea.String(spec.Engine),
		EngineVersion:         tea.String(spec.EngineVersion),
		DBInstanceClass:       tea.String(spec.InstanceClass),
		DBInstanceStorage:     tea.Int32(spec.AllocatedStorage),
		RegionId:              tea.String(region),
		DBInstanceDescription: tea.String(spec.InstanceID),
		InstanceNetworkType:   tea.String("VPC"),
		VPCId:                 tea.String(vsw.vpcID),
		VSwitchId:             tea.String(vsw.vSwitchID),
		DBInstanceNetType:     tea.String("Internet"),
		PayType:               tea.String("Postpaid"),
		SecurityIPList:        tea.String("0.0.0.0/0"),
		Category:              tea.String("Basic"),
	}
}

// toCreatedDBInstance converts the CreateDBInstance response into the CSP-agnostic
// model. Create-specific: the response lacks status/engine/version, so status is
// fixed to "Creating" and engine/version/class are taken from the spec. List uses
// a separate converter that reads the real DBInstanceStatus.
func toCreatedDBInstance(spec rdbinstance.CreateSpec, body *alibabards.CreateDBInstanceResponseBody, region string) models.DBInstance {
	return models.DBInstance{
		Provider:      "alibaba",
		InstanceID:    tea.StringValue(body.DBInstanceId),
		Name:          spec.InstanceID,
		Engine:        spec.Engine,
		EngineVersion: spec.EngineVersion,
		Status:        "creating",
		Endpoint:      tea.StringValue(body.ConnectionString),
		Port:          parsePort(body.Port),
		Region:        region,
		InstanceClass: spec.InstanceClass,
	}
}

// CreateInstance provisions a new RDS instance, then launches the background
// account + public-endpoint provisioning. Returns the instance in "Creating" state.
func (p *AlibabaProvider) CreateInstance(ctx context.Context, spec rdbinstance.CreateSpec) (models.DBInstance, error) {
	vsw, err := p.resolveVSwitch()
	if err != nil {
		return models.DBInstance{}, err
	}

	resp, err := p.client.CreateDBInstance(buildCreateRequest(spec, p.region, vsw))
	if err != nil {
		return models.DBInstance{}, fmt.Errorf("failed to create Alibaba RDS instance: %w", err)
	}
	if resp == nil || resp.Body == nil || tea.StringValue(resp.Body.DBInstanceId) == "" {
		return models.DBInstance{}, fmt.Errorf("Alibaba RDS create returned no instance id")
	}

	instance := toCreatedDBInstance(spec, resp.Body, p.region)

	// Account + public endpoint require the instance to be Running (minutes away).
	go p.provisionInBackground(instance.InstanceID, spec.MasterUsername, spec.MasterPassword)

	return instance, nil
}

// parsePort converts an Alibaba port string into int32 (0 if empty/invalid).
func parsePort(port *string) int32 {
	v, err := strconv.Atoi(tea.StringValue(port))
	if err != nil {
		return 0
	}
	return int32(v)
}


// toSnakeCase converts a CamelCase/PascalCase string to lower snake_case,
// handling acronym boundaries (e.g., DBInstanceClassChanging → db_instance_class_changing).
func toSnakeCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prev := runes[i-1]
				prevLowerOrDigit := (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9')
				prevUpper := prev >= 'A' && prev <= 'Z'
				nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
				if prevLowerOrDigit || (prevUpper && nextLower) {
					b.WriteByte('_')
				}
			}
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeStatus converts an Alibaba DBInstanceStatus to snake_case, mapping
// "Running" to AWS's "available".
func normalizeStatus(s string) string {
	out := toSnakeCase(s)
	if out == "running" {
		return "available"
	}
	return out
}

// pickEndpoint returns the connection string + port of the public endpoint if one
// exists, otherwise the first non-public (private/inner) endpoint.
func pickEndpoint(netInfos []*alibabards.DescribeDBInstanceNetInfoResponseBodyDBInstanceNetInfosDBInstanceNetInfo) (string, int32) {
	var fallbackEP string
	var fallbackPort int32
	var haveFallback bool
	for _, ni := range netInfos {
		if tea.StringValue(ni.IPType) == "Public" {
			return tea.StringValue(ni.ConnectionString), parsePort(ni.Port)
		}
		if !haveFallback {
			fallbackEP = tea.StringValue(ni.ConnectionString)
			fallbackPort = parsePort(ni.Port)
			haveFallback = true
		}
	}
	return fallbackEP, fallbackPort
}

// toListedDBInstance converts a DescribeDBInstances item into the CSP-agnostic
// model, applying status normalization and keeping the engine name as-is.
func toListedDBInstance(item *alibabards.DescribeDBInstancesResponseBodyItemsDBInstance, endpoint string, port int32, region string) models.DBInstance {
	name := tea.StringValue(item.DBInstanceDescription)
	if name == "" {
		name = tea.StringValue(item.DBInstanceId)
	}
	return models.DBInstance{
		Provider:      "alibaba",
		InstanceID:    tea.StringValue(item.DBInstanceId),
		Name:          name,
		Engine:        tea.StringValue(item.Engine),
		EngineVersion: tea.StringValue(item.EngineVersion),
		Status:        normalizeStatus(tea.StringValue(item.DBInstanceStatus)),
		Endpoint:      endpoint,
		Port:          port,
		Region:        region,
		InstanceClass: tea.StringValue(item.DBInstanceClass),
	}
}

// instanceEndpoint returns the public-or-private endpoint+port for one instance.
func (p *AlibabaProvider) instanceEndpoint(instanceID string) (string, int32, error) {
	resp, err := p.client.DescribeDBInstanceNetInfo(&alibabards.DescribeDBInstanceNetInfoRequest{
		DBInstanceId: tea.String(instanceID),
	})
	if err != nil {
		return "", 0, err
	}
	if resp == nil || resp.Body == nil || resp.Body.DBInstanceNetInfos == nil {
		return "", 0, nil
	}
	ep, port := pickEndpoint(resp.Body.DBInstanceNetInfos.DBInstanceNetInfo)
	return ep, port, nil
}

// ListInstances returns all RDS instances in the region. endpoint+port require a
// per-instance DescribeDBInstanceNetInfo call (N+1).
func (p *AlibabaProvider) ListInstances(ctx context.Context) ([]models.DBInstance, error) {
	const pageSize int32 = 100
	out := []models.DBInstance{}
	for pageNumber := int32(1); ; pageNumber++ {
		resp, err := p.client.DescribeDBInstances(&alibabards.DescribeDBInstancesRequest{
			RegionId:   tea.String(p.region),
			PageNumber: tea.Int32(pageNumber),
			PageSize:   tea.Int32(pageSize),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to describe DB instances: %w", err)
		}
		if resp == nil || resp.Body == nil || resp.Body.Items == nil {
			break
		}
		items := resp.Body.Items.DBInstance
		for _, item := range items {
			ep, port, err := p.instanceEndpoint(tea.StringValue(item.DBInstanceId))
			if err != nil {
				return nil, fmt.Errorf("failed to describe net info for %s: %w", tea.StringValue(item.DBInstanceId), err)
			}
			out = append(out, toListedDBInstance(item, ep, port, p.region))
		}
		if int32(len(items)) < pageSize {
			break
		}
	}
	return out, nil
}

// DeleteInstance deletes an RDS instance without retaining any backup. The response
// has no instance details, so the returned model is constructed locally.
func (p *AlibabaProvider) DeleteInstance(ctx context.Context, instanceID string) (models.DBInstance, error) {
	_, err := p.client.DeleteDBInstance(&alibabards.DeleteDBInstanceRequest{
		DBInstanceId:       tea.String(instanceID),
		ReleasedKeepPolicy: tea.String("None"),
	})
	if err != nil {
		return models.DBInstance{}, fmt.Errorf("failed to delete Alibaba RDS instance: %w", err)
	}
	return models.DBInstance{
		Provider:   "alibaba",
		InstanceID: instanceID,
		Name:       instanceID,
		Status:     "deleting",
		Region:     p.region,
	}, nil
}

// distinctSorted returns the unique, sorted set of non-empty strings.
func distinctSorted(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	for _, s := range items {
		if s != "" {
			seen[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// extractEngineVersions collects the engine versions for the given Alibaba engine
// from a DescribeAvailableZones response (deduplicated, engine tag kept as-is).
func extractEngineVersions(body *alibabards.DescribeAvailableZonesResponseBody, engine string) []models.DBEngineVersion {
	seen := make(map[string]struct{})
	var out []models.DBEngineVersion
	for _, zone := range body.AvailableZones {
		for _, eng := range zone.SupportedEngines {
			if tea.StringValue(eng.Engine) != engine {
				continue
			}
			for _, ver := range eng.SupportedEngineVersions {
				v := tea.StringValue(ver.Version)
				if v == "" {
					continue
				}
				if _, ok := seen[v]; ok {
					continue
				}
				seen[v] = struct{}{}
				out = append(out, models.DBEngineVersion{Engine: engine, EngineVersion: v})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EngineVersion < out[j].EngineVersion })
	return out
}

// isUsableZone returns false for multi-AZ aggregated zones (e.g. "ap-northeast-1MAZ1(a,b)"),
// which cannot be used for single-resource placement like VSwitches or instance classes.
func isUsableZone(zoneID string) bool {
	return !strings.Contains(zoneID, "(") && !strings.Contains(zoneID, "MAZ")
}

// zoneIDs returns the distinct, sorted single-AZ zone ids from a DescribeAvailableZones response.
func zoneIDs(body *alibabards.DescribeAvailableZonesResponseBody) []string {
	ids := make([]string, 0, len(body.AvailableZones))
	for _, zone := range body.AvailableZones {
		id := tea.StringValue(zone.ZoneId)
		if isUsableZone(id) {
			ids = append(ids, id)
		}
	}
	return distinctSorted(ids)
}

// ListEngineVersions returns available engine versions for the supported engines,
// queried per engine and merged.
func (p *AlibabaProvider) ListEngineVersions(ctx context.Context) ([]models.DBEngineVersion, error) {
	var out []models.DBEngineVersion
	for _, engine := range supportedEngines {
		resp, err := p.client.DescribeAvailableZones(&alibabards.DescribeAvailableZonesRequest{
			RegionId: tea.String(p.region),
			Engine:   tea.String(engine),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to describe available zones for %s: %w", engine, err)
		}
		if resp != nil && resp.Body != nil {
			out = append(out, extractEngineVersions(resp.Body, engine)...)
		}
	}
	return out, nil
}

// storageTypes is the fixed set of storage types probed for instance classes.
// Alibaba supports only these three; we brute-force (zone × storageType).
var storageTypes = []string{"cloud_ssd", "cloud_essd"}

// ListInstanceClasses returns the orderable instance classes for engine+version
// under the Basic category, gathered across all zones in the region.
func (p *AlibabaProvider) ListInstanceClasses(ctx context.Context, engine, engineVersion string) ([]string, error) {
	zonesResp, err := p.client.DescribeAvailableZones(&alibabards.DescribeAvailableZonesRequest{
		RegionId:      tea.String(p.region),
		Engine:        tea.String(engine),
		EngineVersion: tea.String(engineVersion),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe available zones: %w", err)
	}
	if zonesResp == nil || zonesResp.Body == nil {
		return nil, nil
	}
	zones := zoneIDs(zonesResp.Body)
	if len(zones) == 0 {
		return []string{}, nil
	}
	zone := zones[0]

	var classes []string
	for _, storageType := range storageTypes {
		clsResp, err := p.client.DescribeAvailableClasses(&alibabards.DescribeAvailableClassesRequest{
			RegionId:              tea.String(p.region),
			ZoneId:                tea.String(zone),
			Engine:                tea.String(engine),
			EngineVersion:         tea.String(engineVersion),
			Category:              tea.String(instanceCategory),
			DBInstanceStorageType: tea.String(storageType),
			InstanceChargeType:    tea.String("Postpaid"),
		})
		if err != nil {
			if strings.Contains(err.Error(), "InvalidCondition.NotFound") {
				continue
			}
			return nil, fmt.Errorf("failed to describe available classes (zone=%s, storage=%s): %w", zone, storageType, err)
		}
		if clsResp == nil || clsResp.Body == nil {
			continue
		}
		for _, c := range clsResp.Body.DBInstanceClasses {
			classes = append(classes, tea.StringValue(c.DBInstanceClass))
		}
	}

	return distinctSorted(classes), nil
}

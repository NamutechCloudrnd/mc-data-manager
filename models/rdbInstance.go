package models

// RDBInstanceListRequest is the POST body for listing RDB (database) instances.
// It embeds BaseParams so the json keys (provider, region, credentialId) match the
// existing convention used by other handlers. Credential is resolved by provider,
// so credentialId is currently accepted but unused.
type RDBInstanceListRequest struct {
	BaseParams
}

// RDBInstanceCreateRequest is the PUT body for creating an RDB (database) instance.
// It embeds BaseParams (provider, region, credentialId) plus the minimal fields
// required to provision an instance. Credential is resolved by provider.
type RDBInstanceCreateRequest struct {
	BaseParams
	InstanceID       string `json:"instanceId"`
	InstanceClass    string `json:"instanceClass"`
	Engine           string `json:"engine"`
	EngineVersion    string `json:"engineVersion"`
	MasterUsername   string `json:"masterUsername"`
	MasterPassword   string `json:"masterPassword"`
	AllocatedStorage int32  `json:"allocatedStorage"`
}

// RDBInstanceDeleteRequest is the DELETE body for deleting an RDB instance.
type RDBInstanceDeleteRequest struct {
	BaseParams
	InstanceID string `json:"instanceId"`
}

// RDBEngineVersionsRequest is the POST body for listing available DB engine versions.
type RDBEngineVersionsRequest struct {
	BaseParams
}

// RDBInstanceClassRequest is the POST body for listing orderable instance classes
// for a specific engine and version.
type RDBInstanceClassRequest struct {
	BaseParams
	Engine        string `json:"engine"`
	EngineVersion string `json:"engineVersion"`
}

// DBEngineVersion is a CSP-agnostic available DB engine version.
type DBEngineVersion struct {
	Engine        string `json:"engine"`
	EngineVersion string `json:"engineVersion"`
}

// DBInstance is a CSP-agnostic representation of a managed database instance.
// AWS RDS is the first provider mapped onto this shape; other CSPs reuse it.
type DBInstance struct {
	Provider      string `json:"provider"`
	InstanceID    string `json:"instanceId"`
	Name          string `json:"name"`
	Engine        string `json:"engine"`
	EngineVersion string `json:"engineVersion"`
	Status        string `json:"status"`
	Endpoint      string `json:"endpoint"`
	Port          int32  `json:"port"`
	Region        string `json:"region"`
	InstanceClass string `json:"instanceClass"`
}

// RDBInstanceRecord is the persisted record of a created RDB (database) instance.
type RDBInstanceRecord struct {
	ID               uint64 `gorm:"primaryKey;autoIncrement"`
	Provider         string `gorm:"column:provider;size:20;not null;uniqueIndex:idx_provider_region_instance"`
	Region           string `gorm:"column:region;size:50;not null;uniqueIndex:idx_provider_region_instance"`
	InstanceID       string `gorm:"column:instance_id;size:255;not null;uniqueIndex:idx_provider_region_instance"`
	InstanceName     string `gorm:"column:instance_name;size:255;not null"`
	CspInstanceName  string `gorm:"column:csp_instance_name;size:255;not null"`
	PublicNetPending bool   `gorm:"column:public_net_pending;not null;default:false"` // only for alibaba
	NamespaceID      string `gorm:"column:namespace_id;size:100;not null"`
}

func (RDBInstanceRecord) TableName() string {
	return "tbRdbInstance"
}

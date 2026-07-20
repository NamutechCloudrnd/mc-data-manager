package models

import "time"

// BackupRecord is the persisted catalog entry for a backup (Object Storage,
// RDB, or NRDB), keyed by namespace + service type + backup name. Path is
// server-managed (see docs/superpowers/specs/2026-07-15-backup-catalog-design.md).
type BackupRecord struct {
	ID           string    `gorm:"primaryKey;size:36"`
	NamespaceID  string    `gorm:"column:namespace_id;size:100;not null;uniqueIndex:idx_ns_type_name"`
	ServiceType  string    `gorm:"column:service_type;size:20;not null;uniqueIndex:idx_ns_type_name"`
	BackupName   string    `gorm:"column:backup_name;size:255;not null;uniqueIndex:idx_ns_type_name"`
	Provider     string    `gorm:"column:provider;size:20;not null"`
	Region       string    `gorm:"column:region;size:50;not null"`
	InstanceId   string    `gorm:"column:instance_id;size:255;not null"`
	InstanceName string    `gorm:"column:instance_name;size:255;not null"`
	DatabaseName *string   `gorm:"column:database_name;size:255"` // nullable, only for rdbms/nrdbms
	Path         string    `gorm:"column:path;size:500;not null"`
	Status       string    `gorm:"column:status;size:20;not null"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (BackupRecord) TableName() string {
	return "tbbackup"
}

type BackupListResponse struct {
	ID           string    `json:"id"`
	BackupName   string    `json:"backupName"`
	Provider     string    `json:"provider"`
	Region       string    `json:"region"`
	InstanceId   string    `json:"instanceId"`
	InstanceName string    `json:"instanceName"`
	DatabaseName *string    `json:"databaseName"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
}

type BackupCommonRequest struct {
	BackupName   string `json:"backupName"`
	Provider     string `json:"provider"`
	Region       string `json:"region"`
	InstanceId   string `json:"instanceId"`
	InstanceName string `json:"instanceName"`
}

type BackupObjectStorageRequest struct {
	BackupCommonRequest
	Bucket       string              `json:"bucket"`
	Endpoint     string              `json:"endpoint,omitempty"` // only used by NCP
	SourceFilter *ObjectFilterParams `json:"sourceFilter,omitempty"`
}

type BackupRDBRequest struct {
	BackupCommonRequest
	Host         string `json:"host"`
	Port         string `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	DatabaseName string `json:"databaseName"`
}

type BackupNRDBRequest struct {
	BackupCommonRequest
	Host         string `json:"host,omitempty"`
	Port         string `json:"port,omitempty"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	DatabaseName string `json:"databaseName,omitempty"`
	DatabaseID   string `json:"databaseId,omitempty"`
}

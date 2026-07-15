package models

import "time"

// BackupRecord is the persisted catalog entry for a backup (Object Storage,
// RDB, or NRDB), keyed by namespace + service type + backup name. Path is
// server-managed (see docs/superpowers/specs/2026-07-15-backup-catalog-design.md).
type BackupRecord struct {
	ID          string    `gorm:"primaryKey;size:36"`
	NamespaceID string    `gorm:"column:namespace_id;size:100;not null;uniqueIndex:idx_ns_type_name"`
	ServiceType string    `gorm:"column:service_type;size:20;not null;uniqueIndex:idx_ns_type_name"`
	BackupName  string    `gorm:"column:backup_name;size:255;not null;uniqueIndex:idx_ns_type_name"`
	Provider    string    `gorm:"column:provider;size:20;not null"`
	Region      string    `gorm:"column:region;size:50;not null"`
	InstanceId  string    `gorm:"column:instance_id;size:255;not null"`
	Path        string    `gorm:"column:path;size:500;not null"`
	Status      string    `gorm:"column:status;size:20;not null"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (BackupRecord) TableName() string {
	return "tbbackup"
}

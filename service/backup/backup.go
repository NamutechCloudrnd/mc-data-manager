// Package backup manages the backup catalog: namespace-scoped records of
// backups taken across Object Storage, RDB, and NRDB. Actual backup/restore
// execution stays in service/task; this package only tracks what exists and
// where it lives on disk.
package backup

import (
	"fmt"
	"os"

	"github.com/cloud-barista/mc-data-manager/config"
	"github.com/cloud-barista/mc-data-manager/models"
	"github.com/cloud-barista/mc-data-manager/pkg/utils"
	"github.com/cloud-barista/mc-data-manager/repository"
)

// repo returns the namespace-scoped backup repository, backed by the shared
// config.DB connection (set up by config.InitDB() at server startup).
func repo() *repository.BackupRepository {
	return repository.NewBackupRepository(config.DB)
}

// ListBackups returns the current namespace's backup catalog entries,
// optionally filtered by service type ("objectstorage" | "rdbms" | "nrdbms").
// Empty serviceType returns all types.
func ListBackups(serviceType string) ([]models.BackupRecord, error) {
	nsId := utils.GetNsId()
	return repo().FindByNamespace(nsId, serviceType)
}

// DeleteBackup removes a backup catalog entry and its backed-up files on disk.
func DeleteBackup(id string) error {
	record, err := repo().FindByID(id)
	if err != nil {
		return fmt.Errorf("failed to load backup record: %w", err)
	}

	if err := repo().DeleteByID(id); err != nil {
		return fmt.Errorf("failed to delete backup record: %w", err)
	}

	if err := os.RemoveAll(record.Path); err != nil {
		return fmt.Errorf("backup record deleted but failed to remove files at %s: %w", record.Path, err)
	}

	return nil
}

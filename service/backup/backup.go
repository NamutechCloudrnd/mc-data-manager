package backup

import (
	"fmt"
	"os"

	"github.com/cloud-barista/mc-data-manager/config"
	"github.com/cloud-barista/mc-data-manager/models"
	"github.com/cloud-barista/mc-data-manager/pkg/utils"
	"github.com/cloud-barista/mc-data-manager/repository"
)

func repo() *repository.BackupRepository {
	return repository.NewBackupRepository(config.DB)
}

func ListBackups(serviceType string) ([]models.BackupListResponse, error) {
	nsId := utils.GetNsId()
	records, err := repo().FindByNamespace(nsId, serviceType)
	if err != nil {
		return nil, err
	}

	responses := make([]models.BackupListResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, models.BackupListResponse{
			ID:           record.ID,
			BackupName:   record.BackupName,
			Provider:     record.Provider,
			Region:       record.Region,
			InstanceId:   record.InstanceId,
			InstanceName: record.InstanceName,
			DatabaseName: record.DatabaseName,
			Status:       record.Status,
			CreatedAt:    record.CreatedAt,
		})
	}
	return responses, nil
}

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

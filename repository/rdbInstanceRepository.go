package repository

import (
	"errors"

	"github.com/cloud-barista/mc-data-manager/models"

	"gorm.io/gorm"
)

var ErrDuplicateRDBInstance = errors.New("rdb instance already exists")

type RDBInstanceRepository struct {
	db *gorm.DB
}

func NewRDBInstanceRepository(db *gorm.DB) *RDBInstanceRepository {
	return &RDBInstanceRepository{
		db: db,
	}
}

func (r *RDBInstanceRepository) FindByNamespace(provider, region, namespaceID string) ([]models.RDBInstanceRecord, error) {
	var records []models.RDBInstanceRecord
	if err := r.db.Where("provider = ? AND region = ? AND namespace_id = ?", provider, region, namespaceID).Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (r *RDBInstanceRepository) CheckDuplicate(provider, region, namespaceID, instanceName string) error {
	var count int64
	r.db.Model(&models.RDBInstanceRecord{}).
		Where("provider = ? AND region = ? AND namespace_id = ? AND instance_name = ?", provider, region, namespaceID, instanceName).
		Count(&count)
	if count > 0 {
		return ErrDuplicateRDBInstance
	}
	return nil
}

func (r *RDBInstanceRepository) CreateRDBInstance(record *models.RDBInstanceRecord) error {
	return r.db.Create(record).Error
}

func (r *RDBInstanceRepository) DeleteRDBInstanceByID(provider, region, instanceID string) error {
	return r.db.Where("provider = ? AND region = ? AND instance_id = ?", provider, region, instanceID).
		Delete(&models.RDBInstanceRecord{}).Error
}

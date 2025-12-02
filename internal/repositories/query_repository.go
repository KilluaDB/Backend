package repositories

import (
	"errors"
	"my_project/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DatabaseInstanceRepository
type DatabaseInstanceRepository struct { 
	db *gorm.DB 
}

func NewDatabaseInstanceRepository(db *gorm.DB) *DatabaseInstanceRepository { 
	return &DatabaseInstanceRepository{db: db} 
}

func (r *DatabaseInstanceRepository) FindRunningByProjectID(projectID uuid.UUID) (*models.DatabaseInstance, error) {
	var inst models.DatabaseInstance
	err := r.db.Where("project_id = ? AND status = ?", projectID, "running").Order("created_at DESC").First(&inst).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) { return nil, nil }
		return nil, err
	}
	return &inst, nil
}
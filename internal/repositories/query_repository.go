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

// DatabaseCredentialRepository
type DatabaseCredentialRepository struct { 
	db *gorm.DB 
}

func NewDatabaseCredentialRepository(db *gorm.DB) *DatabaseCredentialRepository { 
	return &DatabaseCredentialRepository{db: db} 
}

func (r *DatabaseCredentialRepository) FindByInstanceID(dbInstanceID uuid.UUID) (*models.DatabaseCredential, error) {
	var cred models.DatabaseCredential
	err := r.db.Where("db_instance_id = ?", dbInstanceID).Order("created_at DESC").First(&cred).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) { return nil, nil }
		return nil, err
	}
	return &cred, nil
}

// ProjectRepository
type ProjectRepository struct { 
	db *gorm.DB 
}

func NewProjectsRepository(db *gorm.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) FindByIDAndUserID(id uuid.UUID, userID uuid.UUID) (*models.Project, error) {
	var proj models.Project
	err := r.db.Where("id = ? AND user_id = ?", id, userID).First(&proj).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &proj, nil
}
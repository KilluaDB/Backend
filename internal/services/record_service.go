package services

import (
	"backend/internal/database"
	"backend/internal/models"
	"backend/internal/repositories"
	"context"
	"fmt"

	"github.com/google/uuid"
)

// RecordService provides DB-agnostic operations on containers (tables/collections),
// records (rows/documents), and fields (columns/fields) using a DatabaseDriver
// resolved from the project's DBType.
type RecordService struct {
	projectRepo repositories.ProjectRepository
	drivers     database.DriverRegistry
}

func NewRecordService(
	projectRepo repositories.ProjectRepository,
	drivers database.DriverRegistry,
) *RecordService {
	return &RecordService{
		projectRepo: projectRepo,
		drivers:     drivers,
	}
}

// getProjectAndDriver resolves the project (with ownership check) and the
// appropriate DatabaseDriver based on project.DBType.
func (s *RecordService) getProjectAndDriver(
	ctx context.Context,
	projectID, userID uuid.UUID,
) (*models.Project, database.DatabaseDriver, error) {
	project, err := s.projectRepo.GetByIDAndUserID(ctx, projectID, userID)
	if err != nil {
		return nil, nil, err
	}
	if project == nil {
		return nil, nil, fmt.Errorf("project not found or access denied")
	}

	driver, err := s.drivers.GetDriver(project.DBType)
	if err != nil {
		return nil, nil, err
	}

	return project, driver, nil
}

// Container operations

func (s *RecordService) CreateContainer(ctx context.Context, projectID, userID uuid.UUID, name string) error {
	_, driver, err := s.getProjectAndDriver(ctx, projectID, userID)
	if err != nil {
		return err
	}
	return driver.CreateContainer(ctx, projectID.String(), name)
}

func (s *RecordService) DeleteContainer(ctx context.Context, projectID, userID uuid.UUID, name string) error {
	_, driver, err := s.getProjectAndDriver(ctx, projectID, userID)
	if err != nil {
		return err
	}
	return driver.DeleteContainer(ctx, projectID.String(), name)
}

func (s *RecordService) ListContainers(ctx context.Context, projectID, userID uuid.UUID) ([]string, error) {
	_, driver, err := s.getProjectAndDriver(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	return driver.ListContainers(ctx, projectID.String())
}

// Record operations

func (s *RecordService) InsertRecord(
	ctx context.Context,
	projectID, userID uuid.UUID,
	container string,
	data map[string]interface{},
) error {
	_, driver, err := s.getProjectAndDriver(ctx, projectID, userID)
	if err != nil {
		return err
	}
	return driver.InsertRecord(ctx, projectID.String(), container, data)
}

func (s *RecordService) GetRecords(
	ctx context.Context,
	projectID, userID uuid.UUID,
	container string,
	filter map[string]interface{},
) ([]map[string]interface{}, error) {
	_, driver, err := s.getProjectAndDriver(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	return driver.GetRecords(ctx, projectID.String(), container, filter)
}

func (s *RecordService) UpdateRecords(
	ctx context.Context,
	projectID, userID uuid.UUID,
	container string,
	filter map[string]interface{},
	update map[string]interface{},
) error {
	_, driver, err := s.getProjectAndDriver(ctx, projectID, userID)
	if err != nil {
		return err
	}
	return driver.UpdateRecords(ctx, projectID.String(), container, filter, update)
}

func (s *RecordService) DeleteRecords(
	ctx context.Context,
	projectID, userID uuid.UUID,
	container string,
	filter map[string]interface{},
) error {
	_, driver, err := s.getProjectAndDriver(ctx, projectID, userID)
	if err != nil {
		return err
	}
	return driver.DeleteRecords(ctx, projectID.String(), container, filter)
}

// Field operations

func (s *RecordService) AddField(
	ctx context.Context,
	projectID, userID uuid.UUID,
	container string,
	field string,
	fieldType string,
) error {
	_, driver, err := s.getProjectAndDriver(ctx, projectID, userID)
	if err != nil {
		return err
	}
	return driver.AddField(ctx, projectID.String(), container, field, fieldType)
}

func (s *RecordService) RemoveField(
	ctx context.Context,
	projectID, userID uuid.UUID,
	container string,
	field string,
) error {
	_, driver, err := s.getProjectAndDriver(ctx, projectID, userID)
	if err != nil {
		return err
	}
	return driver.RemoveField(ctx, projectID.String(), container, field)
}


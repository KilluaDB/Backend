package services

import (
	"backend/internal/models"
	"backend/internal/postgres/service"
	"backend/internal/repositories"
	"backend/internal/utils"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidProjectID     = errors.New("invalid project ID")
	ErrInvalidUserID        = errors.New("invalid user ID")
	ErrInvalidDBType        = errors.New("invalid db_type: must be 'postgresql', 'sql','postgres', 'mongodb', or 'nosql'")
	ErrInvalidResourceTier  = errors.New("invalid resource_tier: must be 'free', 'basic', or 'premium'")
	ErrProjectNotFound      = errors.New("project not found or access denied")
	ErrProjectCreateDB      = errors.New("failed to create project or database instance")
	ErrUnsupportedDBForRows = errors.New("row and column operations are only supported for PostgreSQL projects")
)

type ProjectService struct {
	projectRepo          repositories.ProjectRepository
	provisioner          *OperatorProvisioner
	dbInstanceRepo       *repositories.DatabaseInstanceRepository
	instanceConn         *InstanceConnectionService
	postgresTableService *service.TableService
}

func NewProjectService(
	projectRepo repositories.ProjectRepository,
	provisioner *OperatorProvisioner,
	dbInstanceRepo *repositories.DatabaseInstanceRepository,
	instanceConn *InstanceConnectionService,
	postgresTableService *service.TableService,
) *ProjectService {
	return &ProjectService{
		projectRepo:          projectRepo,
		provisioner:          provisioner,
		dbInstanceRepo:       dbInstanceRepo,
		instanceConn:         instanceConn,
		postgresTableService: postgresTableService,
	}
}

type CreateProjectRequest struct {
	Name         string  `json:"name" binding:"required"`
	Description  *string `json:"description,omitempty"`
	DBType       string  `json:"db_type" binding:"required"`
	ResourceTier string  `json:"resource_tier,omitempty"`
}

func normalizeDBType(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "postgresql", "sql", "postgres":
		return "postgres", nil
	case "mongodb", "nosql":
		return "mongodb", nil
	default:
		return "", ErrInvalidDBType
	}
}

func (s *ProjectService) CreateProject(userID string, req CreateProjectRequest) (*models.Project, *models.DatabaseInstance, error) {
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidUserID, err)
	}

	if req.ResourceTier == "" {
		req.ResourceTier = "free"
	}
	if req.Description == nil {
		empty := ""
		req.Description = &empty
	}

	internalDBType, err := normalizeDBType(req.DBType)
	if err != nil {
		return nil, nil, err
	}

	// tierResources validates the tier early so we fail before writing anything to the DB.
	if _, _, _, err := s.provisioner.tierResources(req.ResourceTier); err != nil {
		return nil, nil, ErrInvalidResourceTier
	}

	project := &models.Project{
		UserID:       userUUID,
		Name:         req.Name,
		Description:  req.Description,
		DBType:       internalDBType,
		ResourceTier: req.ResourceTier,
	}
	project.Prepare()

	dbInstance := &models.DatabaseInstance{
		ProjectID: project.ID,
		Status:    "creating",
	}
	dbInstance.Prepare()

	if err := s.projectRepo.Create(context.Background(), project); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrProjectCreateDB, err)
	}
	if err := s.dbInstanceRepo.Create(dbInstance); err != nil {
		_ = s.projectRepo.Delete(context.Background(), project.ID)
		return nil, nil, fmt.Errorf("%w: %v", ErrProjectCreateDB, err)
	}

	// Provision asynchronously; status transitions to "running" or "failed" once done.
	go s.provisionInstanceAsync(project.ID, dbInstance.ID, internalDBType, req.ResourceTier)

	// Reload to get DB-managed fields (timestamps etc.) for the response.
	if p, err := s.projectRepo.GetByID(context.Background(), project.ID); err == nil && p != nil {
		project = p
	}
	if inst, err := s.dbInstanceRepo.GetByID(dbInstance.ID); err == nil && inst != nil {
		dbInstance = inst
	}

	return project, dbInstance, nil
}

// provisionInstanceAsync provisions the database instance and updates its status.
// Credentials are never stored — GetConnection derives them from K8s on demand.
func (s *ProjectService) provisionInstanceAsync(projectID, instanceID uuid.UUID, dbType, resourceTier string) {
	log.Printf("Provisioning DB instance for project %s (type=%s tier=%s)", projectID, dbType, resourceTier)

	// Timeout must exceed the operator wait loops (10 min each) with margin.
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	_, err := s.provisioner.CreateInstance(ctx, projectID, dbType, resourceTier)
	if err != nil {
		log.Printf("ERROR: provision failed for project %s: %v", projectID, err)
		if statusErr := s.dbInstanceRepo.UpdateStatus(instanceID, "failed"); statusErr != nil {
			log.Printf("ERROR: failed to mark instance %s as failed: %v", instanceID, statusErr)
		}
		return
	}

	// Only store status — not the DSN. Credentials are read from K8s at connection time.
	if err := s.dbInstanceRepo.UpdateStatus(instanceID, "running"); err != nil {
		log.Printf("ERROR: failed to mark instance %s as running: %v", instanceID, err)
	}

	log.Printf("DB instance provisioned for project %s", projectID)
}

func (s *ProjectService) GetProjectByID(projectID string) (*models.Project, error) {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProjectID, err)
	}
	return s.projectRepo.GetByID(context.Background(), projectUUID)
}

func (s *ProjectService) GetProjectByIDAndUserID(projectID, userID string) (*models.Project, error) {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProjectID, err)
	}
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidUserID, err)
	}

	project, err := s.projectRepo.GetByIDAndUserID(context.Background(), projectUUID, userUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	if project == nil {
		return nil, ErrProjectNotFound
	}

	if inst, _ := s.dbInstanceRepo.GetByProjectID(project.ID); inst != nil {
		project.Status = inst.Status
	}

	return project, nil
}

func (s *ProjectService) GetProjectsByUserID(userID string) ([]models.Project, error) {
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidUserID, err)
	}

	projects, err := s.projectRepo.GetByUserID(context.Background(), userUUID)
	if err != nil {
		return nil, err
	}

	for i := range projects {
		if inst, _ := s.dbInstanceRepo.GetByProjectID(projects[i].ID); inst != nil {
			projects[i].Status = inst.Status
		}
	}

	return projects, nil
}

func (s *ProjectService) DeleteProjectByIDAndUserID(projectID, userID string) error {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidProjectID, err)
	}
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidUserID, err)
	}

	project, err := s.projectRepo.GetByIDAndUserID(context.Background(), projectUUID, userUUID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}
	if project == nil {
		return ErrProjectNotFound
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.provisioner.DeleteInstance(ctx, projectUUID, project.DBType); err != nil {
		log.Printf("Warning: failed to delete K8s resource for project %s: %v", projectID, err)
	}

	if err := s.projectRepo.DeleteByIDAndUserID(context.Background(), projectUUID, userUUID); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	return nil
}

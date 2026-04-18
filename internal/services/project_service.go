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
	ErrInvalidProjectID    = errors.New("invalid project ID")
	ErrInvalidUserID       = errors.New("invalid user ID")
	ErrInvalidDBType       = errors.New("invalid db_type: must be 'postgresql', 'sql','postgres', 'mongodb', or 'nosql'")
	ErrInvalidResourceTier = errors.New("invalid resource_tier: must be 'free', 'basic', or 'premium'")
	ErrProjectNotFound     = errors.New("project not found or access denied")
	ErrProjectCreateDB     = errors.New("failed to create project or database instance")
)

type ProjectService struct {
	projectRepo          repositories.ProjectRepository
	provisioner          *OperatorProvisioner
	postgresTableService *service.TableService
	poolEvicter          ProjectPoolEvicter
}

type ProjectPoolEvicter interface {
	EvictProject(projectID uuid.UUID)
}

func NewProjectService(
	projectRepo repositories.ProjectRepository,
	provisioner *OperatorProvisioner,
	postgresTableService *service.TableService,
	poolEvicter ProjectPoolEvicter,
) *ProjectService {
	return &ProjectService{
		projectRepo:          projectRepo,
		provisioner:          provisioner,
		postgresTableService: postgresTableService,
		poolEvicter:          poolEvicter,
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
		return "postgresql", nil
	case "mongodb", "nosql":
		return "mongodb", nil
	default:
		return "", ErrInvalidDBType
	}
}

func (s *ProjectService) CreateProject(ctx context.Context, userID string, req CreateProjectRequest) (*models.Project, error) {
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidUserID, err)
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
		return nil, err
	}

	// tierResources validates the tier early so we fail before writing anything to the DB.
	if _, _, _, err = s.provisioner.tierResources(req.ResourceTier); err != nil {
		return nil, ErrInvalidResourceTier
	}

	project := &models.Project{
		UserID:           userUUID,
		Name:             req.Name,
		Description:      req.Description,
		DBType:           internalDBType,
		ResourceTier:     req.ResourceTier,
		CreatedAt:        time.Time{},
		Status:           "",
		RuntimeCreatedAt: nil,
		RuntimeUpdatedAt: nil,
	}
	project.Prepare()

	if err := s.projectRepo.Create(ctx, project); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectCreateDB, err)
	}

	// Provision asynchronously; project status transitions to "running" or "failed" once done.
	go s.provisionInstanceAsync(ctx, project.ID, internalDBType, req.ResourceTier)

	// Reload to get DB-managed fields (timestamps etc.) for the response.
	if p, err := s.projectRepo.GetByID(ctx, project.ID); err == nil && p != nil {
		project = p
	}

	if project.Status == "" {
		project.Status = "creating"
	}

	return project, nil
}

// provisionInstanceAsync provisions the database instance and updates its status.
// Credentials are never stored — GetConnection derives them from K8s on demand.
func (s *ProjectService) provisionInstanceAsync(ctx context.Context, projectID uuid.UUID, dbType, resourceTier string) {
	log.Printf("Provisioning DB instance for project %s (type=%s tier=%s)", projectID, dbType, resourceTier)

	// Timeout must exceed the operator wait loops (10 min each) with margin.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	_, err := s.provisioner.CreateInstance(ctx, projectID, dbType, resourceTier)
	if err != nil {
		log.Printf("ERROR: provision failed for project %s: %v", projectID, err)
		if statusErr := s.projectRepo.UpdateRuntimeStatus(ctx, projectID, "failed"); statusErr != nil {
			log.Printf("ERROR: failed to mark project %s as failed: %v", projectID, statusErr)
		}

		return
	}

	// Only store status — not the DSN. Credentials are read from K8s at connection time.
	if err := s.projectRepo.UpdateRuntimeStatus(ctx, projectID, "running"); err != nil {
		log.Printf("ERROR: failed to mark project %s as running: %v", projectID, err)
	}

	log.Printf("DB instance provisioned for project %s", projectID)
}

func (s *ProjectService) GetProjectByID(ctx context.Context, projectID string) (*models.Project, error) {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProjectID, err)
	}
	return s.projectRepo.GetByID(ctx, projectUUID)
}

func (s *ProjectService) GetProjectByIDAndUserID(ctx context.Context, projectID, userID string) (*models.Project, error) {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProjectID, err)
	}
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidUserID, err)
	}

	project, err := s.projectRepo.GetByIDAndUserID(ctx, projectUUID, userUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	if project == nil {
		return nil, ErrProjectNotFound
	}

	if project.Status == "" {
		project.Status = "creating"
	}

	return project, nil
}

func (s *ProjectService) GetProjectsByUserID(ctx context.Context, userID string) ([]models.Project, error) {
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidUserID, err)
	}

	projects, err := s.projectRepo.GetByUserID(ctx, userUUID)
	if err != nil {
		return nil, err
	}

	for i := range projects {
		if projects[i].Status == "" {
			projects[i].Status = "creating"
		}
	}

	return projects, nil
}

func (s *ProjectService) DeleteProjectByIDAndUserID(ctx context.Context, projectID, userID string) error {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidProjectID, err)
	}
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidUserID, err)
	}

	project, err := s.projectRepo.GetByIDAndUserID(ctx, projectUUID, userUUID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}
	if project == nil {
		return ErrProjectNotFound
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.provisioner.DeleteInstance(ctx, projectUUID, project.DBType); err != nil {
		log.Printf("Warning: failed to delete K8s resource for project %s: %v", projectID, err)
	}
	if s.poolEvicter != nil {
		s.poolEvicter.EvictProject(projectUUID)
	}

	if err := s.projectRepo.DeleteByIDAndUserID(ctx, projectUUID, userUUID); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	return nil
}

package services

import (
	"backend/internal/models"
	"backend/internal/postgres/service"
	"backend/internal/repositories"
	"backend/internal/utils"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for project operations so handlers can return proper HTTP status and messages.
var (
	ErrInvalidProjectID    = errors.New("invalid project ID")
	ErrInvalidUserID       = errors.New("invalid user ID")
	ErrInvalidDBType       = errors.New("invalid db_type: must be 'postgresql', 'sql', 'mongodb', or 'nosql'")
	ErrInvalidResourceTier = errors.New("invalid resource_tier: must be 'free', 'basic', or 'premium'")
	ErrProjectNotFound      = errors.New("project not found or access denied")
	ErrProjectCreateDB      = errors.New("failed to create project or database instance")
	ErrUnsupportedDBForRows = errors.New("row and column operations are only supported for PostgreSQL projects")
)

type ProjectService struct {
	projectRepo       repositories.ProjectRepository
	provisioner       *OperatorProvisioner
	dbInstanceRepo    *repositories.DatabaseInstanceRepository
	dbCredentialRepo  *repositories.DatabaseCredentialRepository
	instanceConn      *InstanceConnectionService
	postgresTableService *service.TableService
}

func NewProjectService(
	projectRepo repositories.ProjectRepository,
	provisioner *OperatorProvisioner,
	dbInstanceRepo *repositories.DatabaseInstanceRepository,
	dbCredentialRepo *repositories.DatabaseCredentialRepository,
	instanceConn *InstanceConnectionService,
	postgresTableService *service.TableService,
) *ProjectService {
	return &ProjectService{
		projectRepo:         projectRepo,
		provisioner:         provisioner,
		dbInstanceRepo:     dbInstanceRepo,
		dbCredentialRepo:   dbCredentialRepo,
		instanceConn:       instanceConn,
		postgresTableService: postgresTableService,
	}
}

type CreateProjectRequest struct {
	Name         string  `json:"name" binding:"required"`
	Description  *string `json:"description,omitempty"`
	DBType       string  `json:"db_type" binding:"required"`   // 'postgresql'|'sql' (→ postgresql) or 'mongodb'|'nosql' (→ mongodb)
	ResourceTier string  `json:"resource_tier,omitempty"`      // 'free', 'basic', or 'premium'; defaults to 'free'
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
		emptyDesc := ""
		req.Description = &emptyDesc
	}

	dbTypeNorm := strings.ToLower(strings.TrimSpace(req.DBType))
	var internalDBType string
	switch dbTypeNorm {
	case "postgresql", "sql":
		internalDBType = "postgresql"
	case "mongodb", "nosql":
		internalDBType = "mongodb"
	default:
		return nil, nil, ErrInvalidDBType
	}

	if req.ResourceTier != "free" && req.ResourceTier != "basic" && req.ResourceTier != "premium" {
		return nil, nil, ErrInvalidResourceTier
	}

	// Build project and instance; project and instance are persisted before
	// provisioning so that the API can return immediately with status "creating".
	project := &models.Project{
		UserID:       userUUID,
		Name:         req.Name,
		Description:  req.Description,
		DBType:       internalDBType,
		ResourceTier: req.ResourceTier,
	}
	project.Prepare()

	cpu, memoryMB, storageGB := s.provisioner.GetTierResources(req.ResourceTier)
	cpuCores := int(cpu)
	ramMB := int(memoryMB)
	var port int
	if internalDBType == "postgresql" {
		port = 5432
	} else {
		port = 27017
	}

	dbInstance := &models.DatabaseInstance{
		ProjectID: project.ID,
		Status:    "creating",
		CPUCores:  &cpuCores,
		RAMMB:     &ramMB,
		StorageGB: &storageGB,
		Port:      &port,
		Host:      nil, // set after provision
	}
	dbInstance.Prepare()

	if err := s.projectRepo.Create(context.Background(), project); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrProjectCreateDB, err)
	}
	if err := s.dbInstanceRepo.Create(dbInstance); err != nil {
		_ = s.projectRepo.Delete(context.Background(), project.ID)
		return nil, nil, fmt.Errorf("%w: %v", ErrProjectCreateDB, err)
	}

	// Start provisioning asynchronously; status will be updated to "running"
	// or "failed" once provisioning completes.
	go s.provisionInstanceAsync(project.ID, dbInstance.ID, internalDBType, req.ResourceTier)

	// Reload project and instance to ensure timestamps and other DB-managed
	// fields are populated in the API response.
	persistedProject, err := s.projectRepo.GetByID(context.Background(), project.ID)
	if err == nil && persistedProject != nil {
		project = persistedProject
	}
	persistedInstance, err := s.dbInstanceRepo.GetByID(dbInstance.ID)
	if err == nil && persistedInstance != nil {
		dbInstance = persistedInstance
	}

	return project, dbInstance, nil
}

// provisionInstanceAsync provisions the database instance via the operator and
// updates instance status and credentials when ready.
func (s *ProjectService) provisionInstanceAsync(projectID, instanceID uuid.UUID, dbType, resourceTier string) {
	fmt.Printf("Provisioning DB instance for project %s (type %s, tier %s)\n", projectID.String(), dbType, resourceTier)

	provCtx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	result, err := s.provisioner.CreateInstance(provCtx, projectID, dbType, resourceTier)
	if err != nil {
		fmt.Printf("ERROR: Failed to provision DB instance for project %s: %v\n", projectID.String(), err)
		if statusErr := s.dbInstanceRepo.UpdateStatus(instanceID, "failed"); statusErr != nil {
			fmt.Printf("ERROR: Failed to mark instance %s as failed: %v\n", instanceID.String(), statusErr)
		}
		return
	}

	if err := s.dbInstanceRepo.UpdateAfterProvision(instanceID, result.Host, result.Port); err != nil {
		fmt.Printf("ERROR: Failed to update instance %s after provision: %v\n", instanceID.String(), err)
		return
	}

	encryptedPassword, err := utils.EncryptString(result.Password)
	if err != nil {
		fmt.Printf("Warning: failed to encrypt database password for project %s: %v\n", projectID.String(), err)
		return
	}

	credential := &models.DatabaseCredential{
		DBInstanceID:      instanceID,
		Username:          "admin",
		PasswordEncrypted: encryptedPassword,
	}
	if err := s.dbCredentialRepo.Upsert(credential); err != nil {
		fmt.Printf("Warning: failed to save database credentials for project %s: %v\n", projectID.String(), err)
		return
	}

	fmt.Printf("DB instance provisioned for project %s\n", projectID.String())
}

func (s *ProjectService) GetProjectByID(projectID string) (*models.Project, error) {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project ID: %w", err)
	}

	return s.projectRepo.GetByID(context.Background(), projectUUID)
}

func (s *ProjectService) GetProjectByIDAndUserID(projectID string, userID string) (*models.Project, error) {
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

	// Populate status from the project's database instance
	if inst, _ := s.dbInstanceRepo.GetByProjectID(project.ID); inst != nil {
		project.Status = inst.Status
	}

	return project, nil
}

func (s *ProjectService) GetProjectsByUserID(userID string) ([]models.Project, error) {
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	projects, err := s.projectRepo.GetByUserID(context.Background(), userUUID)
	if err != nil {
		return nil, err
	}

	// Populate status from each project's database instance
	for i := range projects {
		if inst, _ := s.dbInstanceRepo.GetByProjectID(projects[i].ID); inst != nil {
			projects[i].Status = inst.Status
		}
	}

	return projects, nil
}

func (s *ProjectService) DeleteProject(projectID string) error {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	// Get project to verify it exists
	project, err := s.projectRepo.GetByID(context.Background(), projectUUID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}
	if project == nil {
		return fmt.Errorf("project not found")
	}

	// Note: Container deletion should be handled via database_instances table
	// For now, just delete the project (CASCADE will handle related records)

	// Delete project from database
	return s.projectRepo.Delete(context.Background(), projectUUID)
}

func (s *ProjectService) DeleteProjectByIDAndUserID(projectID string, userID string) error {
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

	// Delete K8s DB resource by project ID (discover resource ref from cluster name convention)
	resourceRef := s.provisioner.ResourceRefForProject(projectUUID, project.DBType)
	delCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.provisioner.DeleteInstance(delCtx, resourceRef); err != nil {
		fmt.Printf("Warning: Failed to delete K8s DB resource %s for project %s: %v\n", resourceRef, projectID, err)
	} else {
		fmt.Printf("Successfully deleted K8s DB resource %s for project %s\n", resourceRef, projectID)
	}

	err = s.projectRepo.DeleteByIDAndUserID(context.Background(), projectUUID, userUUID)
	if err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	return nil
}

// InsertRow delegates to PostgreSQL row service when project is PostgreSQL.
func (s *ProjectService) InsertRow(userID, projectID uuid.UUID, req service.InsertRowRequest) (*service.InsertRowResponse, error) {
	proj, err := s.projectRepo.GetByIDAndUserID(context.Background(), projectID, userID)
	if err != nil || proj == nil {
		return nil, ErrProjectNotFound
	}
	if proj.DBType != "postgresql" {
		return nil, ErrUnsupportedDBForRows
	}
	return s.postgresTableService.InsertRow(context.Background(), userID, projectID, req)
}

// DeleteRow delegates to PostgreSQL row service when project is PostgreSQL.
func (s *ProjectService) DeleteRow(userID, projectID uuid.UUID, req service.DeleteRowRequest, rowID string) error {
	proj, err := s.projectRepo.GetByIDAndUserID(context.Background(), projectID, userID)
	if err != nil || proj == nil {
		return ErrProjectNotFound
	}
	if proj.DBType != "postgresql" {
		return ErrUnsupportedDBForRows
	}
	return s.postgresTableService.DeleteRow(context.Background(), userID, projectID, req, rowID)
}

// AddColumn delegates to PostgreSQL row service when project is PostgreSQL.
func (s *ProjectService) AddColumn(userID, projectID uuid.UUID, req service.AddColumnRequest) (*service.AddColumnResponse, error) {
	proj, err := s.projectRepo.GetByIDAndUserID(context.Background(), projectID, userID)
	if err != nil || proj == nil {
		return nil, ErrProjectNotFound
	}
	if proj.DBType != "postgresql" {
		return nil, ErrUnsupportedDBForRows
	}
	return s.postgresTableService.AddColumn(context.Background(), userID, projectID, req)
}

// DeleteColumn delegates to PostgreSQL row service when project is PostgreSQL.
func (s *ProjectService) DeleteColumn(userID, projectID uuid.UUID, req service.DeleteColumnRequest, columnName string) error {
	proj, err := s.projectRepo.GetByIDAndUserID(context.Background(), projectID, userID)
	if err != nil || proj == nil {
		return ErrProjectNotFound
	}
	if proj.DBType != "postgresql" {
		return ErrUnsupportedDBForRows
	}
	return s.postgresTableService.DeleteColumn(context.Background(), userID, projectID, req, columnName)
}

package services

import (
	"fmt"
	"my_project/internal/models"
	"my_project/internal/repositories"
	"my_project/internal/utils"
)

type ProjectService struct {
	projectRepo      *repositories.ProjectRepository
	orchestrator     *OrchestratorService
	dbInstanceRepo   *repositories.DatabaseInstanceRepository
	dbCredentialRepo *repositories.DatabaseCredentialRepository
}

func NewProjectService(
	projectRepo *repositories.ProjectRepository,
	orchestrator *OrchestratorService,
	dbInstanceRepo *repositories.DatabaseInstanceRepository,
	dbCredentialRepo *repositories.DatabaseCredentialRepository,
) *ProjectService {
	return &ProjectService{
		projectRepo:      projectRepo,
		orchestrator:     orchestrator,
		dbInstanceRepo:   dbInstanceRepo,
		dbCredentialRepo: dbCredentialRepo,
	}
}

type CreateProjectRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description,omitempty"`
	DBType      string  `json:"db_type" binding:"required"` // 'postgres' or 'mongodb'
}

func (s *ProjectService) CreateProject(userID string, req CreateProjectRequest) (*models.Project, error) {
	// Parse user ID
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	// Validate DB type
	if req.DBType != "postgres" && req.DBType != "mongodb" {
		return nil, fmt.Errorf("invalid db_type: must be 'postgres' or 'mongodb'")
	}

	// Create project record
	project := &models.Project{
		UserID:      userUUID,
		Name:        req.Name,
		Description: req.Description,
		DBType:      req.DBType,
	}

	if err := s.projectRepo.Create(project); err != nil {
		return nil, fmt.Errorf("failed to save project to database: %w", err)
	}

	// Map DB type for orchestrator (postgres -> postgresql)
	dbTypeForOrchestrator := req.DBType
	if req.DBType == "postgres" {
		dbTypeForOrchestrator = "postgresql"
	}

	// Create database instance record (status: creating)
	dbInstance := &models.DatabaseInstance{
		ProjectID: project.ID,
		Status:    "creating",
	}

	if err := s.dbInstanceRepo.Create(dbInstance); err != nil {
		// If instance creation fails, delete the project (rollback)
		s.projectRepo.Delete(project.ID)
		return nil, fmt.Errorf("failed to create database instance: %w", err)
	}

	// Create container via orchestrator
	orchestratorReq := CreateContainerRequest{
		SessionName:   project.ID.String(), // Use project ID as session name
		DatabaseType:  dbTypeForOrchestrator,
		Configuration: nil, // Can be extended later
	}

	fmt.Printf("Creating container for project %s with database type %s\n", project.ID.String(), dbTypeForOrchestrator)
	orchestratorResp, err := s.orchestrator.CreateContainer(orchestratorReq)
	if err != nil {
		// Update instance status to failed
		s.dbInstanceRepo.UpdateStatus(dbInstance.ID, "failed")
		fmt.Printf("ERROR: Failed to create container: %v\n", err)
		return nil, fmt.Errorf("failed to create container: %w", err)
	}
	fmt.Printf("Container created successfully: %s\n", orchestratorResp.ContainerID)

	// Update database instance with container details
	endpoint := orchestratorResp.ConnectionInfo.Host
	port := orchestratorResp.ConnectionInfo.Port
	containerID := orchestratorResp.ContainerID

	// Store container ID
	if err := s.dbInstanceRepo.UpdateContainerID(dbInstance.ID, containerID); err != nil {
		return nil, fmt.Errorf("failed to update database instance container ID: %w", err)
	}

	// Update endpoint and port
	if err := s.dbInstanceRepo.UpdateEndpoint(dbInstance.ID, endpoint, port); err != nil {
		return nil, fmt.Errorf("failed to update database instance endpoint: %w", err)
	}

	// Update status to running
	if err := s.dbInstanceRepo.UpdateStatus(dbInstance.ID, "running"); err != nil {
		return nil, fmt.Errorf("failed to update database instance status: %w", err)
	}

	// Store database credentials (encrypted)
	hashedPassword, err := utils.Hash(orchestratorResp.ConnectionInfo.Password)
	if err != nil {
		// Log error but don't fail - credentials can be retrieved later
		fmt.Printf("Warning: failed to hash database password: %v\n", err)
	} else {
		credential := &models.DatabaseCredential{
			DBInstanceID:      dbInstance.ID,
			Username:          orchestratorResp.ConnectionInfo.User,
			PasswordEncrypted: string(hashedPassword),
		}

		if err := s.dbCredentialRepo.Create(credential); err != nil {
			// Log error but don't fail - credentials can be retrieved later
			fmt.Printf("Warning: failed to save database credentials: %v\n", err)
		}
	}

	return project, nil
}

func (s *ProjectService) GetProjectByID(projectID string) (*models.Project, error) {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project ID: %w", err)
	}

	return s.projectRepo.GetByID(projectUUID)
}

func (s *ProjectService) GetProjectByIDAndUserID(projectID string, userID string) (*models.Project, error) {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project ID: %w", err)
	}

	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	project, err := s.projectRepo.GetByIDAndUserID(projectUUID, userUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	if project == nil {
		return nil, fmt.Errorf("project not found or access denied")
	}

	return project, nil
}

func (s *ProjectService) GetProjectsByUserID(userID string) ([]models.Project, error) {
	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	return s.projectRepo.GetByUserID(userUUID)
}

func (s *ProjectService) DeleteProject(projectID string) error {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	// Get project to verify it exists
	project, err := s.projectRepo.GetByID(projectUUID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}
	if project == nil {
		return fmt.Errorf("project not found")
	}

	// Note: Container deletion should be handled via database_instances table
	// For now, just delete the project (CASCADE will handle related records)

	// Delete project from database
	return s.projectRepo.Delete(projectUUID)
}

func (s *ProjectService) DeleteProjectByIDAndUserID(projectID string, userID string) error {
	projectUUID, err := utils.ParseUUID(projectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	userUUID, err := utils.ParseUUID(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	// Verify project belongs to user
	project, err := s.projectRepo.GetByIDAndUserID(projectUUID, userUUID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}
	if project == nil {
		return fmt.Errorf("project not found or access denied")
	}

	// Get database instance for this project
	dbInstance, err := s.dbInstanceRepo.GetByProjectID(projectUUID)
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}

	// If database instance exists and has a container ID, stop the container
	if dbInstance != nil && dbInstance.ContainerID != nil && *dbInstance.ContainerID != "" {
		// Try to stop container via orchestrator (best effort, don't fail if it fails)
		if err := s.orchestrator.DeleteContainer(*dbInstance.ContainerID); err != nil {
			// Log error but don't fail - container might already be stopped or deleted
			fmt.Printf("Warning: Failed to stop container %s for project %s: %v\n", *dbInstance.ContainerID, projectID, err)
		} else {
			fmt.Printf("Successfully stopped container %s for project %s\n", *dbInstance.ContainerID, projectID)
		}
	}

	// Delete project from database (CASCADE will handle database_instances and credentials)
	err = s.projectRepo.DeleteByIDAndUserID(projectUUID, userUUID)
	if err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}

	return nil
}

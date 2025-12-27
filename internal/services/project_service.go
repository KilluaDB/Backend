package services

import (
	"backend/internal/models"
	"backend/internal/repositories"
	"backend/internal/utils"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
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
	Name         string  `json:"name" binding:"required"`
	Description  *string `json:"description,omitempty"`
	DBType       string  `json:"db_type" binding:"required"`       // 'postgres' or 'mongodb'
	ResourceTier string  `json:"resource_tier" binding:"required"` // 'free', 'basic', or 'premium'
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

	// Validate resource tier
	if req.ResourceTier != "free" && req.ResourceTier != "basic" && req.ResourceTier != "premium" {
		return nil, fmt.Errorf("invalid resource_tier: must be 'free', 'basic', or 'premium'")
	}

	// Create project record
	project := &models.Project{
		UserID:       userUUID,
		Name:         req.Name,
		Description:  req.Description,
		DBType:       req.DBType,
		ResourceTier: req.ResourceTier,
	}

	if err := s.projectRepo.Create(project); err != nil {
		return nil, fmt.Errorf("failed to save project to database: %w", err)
	}

	// Map DB type for orchestrator (postgres -> postgresql)
	dbTypeForOrchestrator := req.DBType
	if req.DBType == "postgres" {
		dbTypeForOrchestrator = "postgresql"
	}

	// Map resource tier to resource limits
	resourceConfig := s.getResourceConfigForTier(req.ResourceTier)

	// Get CPU and RAM values for database instance
	cpuCores := int(resourceConfig["cpu"].(float64))
	ramMB := int(resourceConfig["memory_mb"].(float64))
	// Storage can be set based on tier as well, defaulting to 10GB for all tiers
	storageGB := 10

	// Get default port for database type
	var port int
	if req.DBType == "postgres" {
		port = 5432
	} else if req.DBType == "mongodb" {
		port = 27017
	} else {
		port = 5432 // Default to postgres port
	}

	// Create database instance record (status: creating) with resource information
	dbInstance := &models.DatabaseInstance{
		ProjectID: project.ID,
		Status:    "creating",
		CPUCores:  &cpuCores,
		RAMMB:     &ramMB,
		StorageGB: &storageGB,
		Port:      &port,
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
		Configuration: resourceConfig,
	}

	fmt.Printf("Creating container for project %s with database type %s and tier %s (CPU: %d, RAM: %dMB)\n",
		project.ID.String(), dbTypeForOrchestrator, req.ResourceTier, cpuCores, ramMB)
	orchestratorResp, err := s.orchestrator.CreateContainer(orchestratorReq)
	if err != nil {
		// Update instance status to failed
		s.dbInstanceRepo.UpdateStatus(dbInstance.ID, "failed")
		fmt.Printf("ERROR: Failed to create container: %v\n", err)
		return nil, fmt.Errorf("failed to create container: %w", err)
	}
	fmt.Printf("Container created successfully: %s\n", orchestratorResp.ContainerID)

	// Update database instance with container details
	containerID := orchestratorResp.ContainerID

	// Store container ID (IP will be retrieved from orchestrator when needed)
	if err := s.dbInstanceRepo.UpdateContainerID(dbInstance.ID, containerID); err != nil {
		return nil, fmt.Errorf("failed to update database instance container ID: %w", err)
	}

	// Update status to running
	if err := s.dbInstanceRepo.UpdateStatus(dbInstance.ID, "running"); err != nil {
		return nil, fmt.Errorf("failed to update database instance status: %w", err)
	}

	// Store database credentials: encrypt the password returned by the orchestrator
	encryptedPassword, err := utils.EncryptString(orchestratorResp.ConnectionInfo.Password)
	if err != nil {
		// Log error but don't fail - queries will fail until credentials are fixed
		fmt.Printf("Warning: failed to encrypt database password: %v\n", err)
	} else {
		credential := &models.DatabaseCredential{
			DBInstanceID:      dbInstance.ID,
			Username:          orchestratorResp.ConnectionInfo.User,
			PasswordEncrypted: encryptedPassword,
		}

		if err := s.dbCredentialRepo.Create(credential); err != nil {
			// Log error but don't fail - credentials can be recreated by recreating the instance
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

// getResourceConfigForTier maps resource tiers to resource configurations
// Returns a map with cpu (in cores) and memory_mb (in MB) for the orchestrator
func (s *ProjectService) getResourceConfigForTier(tier string) map[string]interface{} {
	config := make(map[string]interface{})

	switch tier {
	case "free":
		// Free tier: 0.5 CPU, 512 MB RAM
		config["cpu"] = 0.5
		config["memory_mb"] = 512.0
	case "basic":
		// Basic tier: 1 CPU, 1024 MB (1 GB) RAM
		config["cpu"] = 1.0
		config["memory_mb"] = 1024.0
	case "premium":
		// Premium tier: 2 CPU, 2048 MB (2 GB) RAM
		config["cpu"] = 2.0
		config["memory_mb"] = 2048.0
	default:
		// Default to free tier if invalid
		config["cpu"] = 0.5
		config["memory_mb"] = 512.0
	}

	return config
}

// getDBConnection gets a database connection for a project's database instance
func (s *ProjectService) getDBConnection(userID uuid.UUID, projectID uuid.UUID) (*sql.DB, error) {
	// Validate project ownership
	project, err := s.projectRepo.GetByIDAndUserID(projectID, userID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found or not accessible")
	}

	// Find running DB instance for this project
	inst, err := s.dbInstanceRepo.GetRunningByProjectID(projectID)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, errors.New("no running database instance for this project")
	}

	// Fetch credentials for the instance
	cred, err := s.dbCredentialRepo.GetLatestByInstanceID(inst.ID)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, errors.New("no credentials configured for this database instance")
	}

	// Build connection string
	if inst.ContainerID == nil || *inst.ContainerID == "" {
		return nil, errors.New("database instance container ID not configured")
	}
	if inst.Port == nil {
		return nil, errors.New("database instance port not configured")
	}

	// Get container IP from orchestrator
	containerIP, ok := s.orchestrator.GetContainerIP(*inst.ContainerID)
	if !ok {
		// Try to get from Redis as fallback
		var err error
		containerIP, err = s.orchestrator.GetContainerIPFromRedis(context.Background(), *inst.ContainerID)
		if err != nil {
			return nil, fmt.Errorf("failed to get container IP: %w", err)
		}
	}

	// Decrypt password before building DSN
	dbPassword, err := utils.DecryptString(cred.PasswordEncrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt database credentials: %w", err)
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		containerIP, *inst.Port, cred.Username, dbPassword, "postgres")

	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	return sqlDB, nil
}

// validateIdentifier validates SQL identifiers (table names, column names) to prevent SQL injection
func validateIdentifier(identifier string) error {
	// Check for empty string
	if identifier == "" {
		return errors.New("identifier cannot be empty")
	}

	// Allow alphanumeric characters, underscores, and hyphens
	// Must start with a letter or underscore
	validPattern := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_\-]*$`)
	if !validPattern.MatchString(identifier) {
		return errors.New("invalid identifier: must start with letter or underscore and contain only alphanumeric characters, underscores, and hyphens")
	}

	return nil
}

// InsertRowRequest represents the request body for inserting a row
type InsertRowRequest struct {
	Table  string                 `json:"table" binding:"required"`
	Values map[string]interface{} `json:"values" binding:"required"`
}

// InsertRowResponse represents the response for inserting a row
type InsertRowResponse struct {
	RowID int64 `json:"row_id"`
}

// InsertRow inserts a row into a table
func (s *ProjectService) InsertRow(userID uuid.UUID, projectID uuid.UUID, req InsertRowRequest) (*InsertRowResponse, error) {
	// Validate table name
	if err := validateIdentifier(req.Table); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}
	// Validate that values map is not empty
	if len(req.Values) == 0 {
		return nil, errors.New("values cannot be empty")
	}

	// Validate column names
	for colName := range req.Values {
		if err := validateIdentifier(colName); err != nil {
			return nil, fmt.Errorf("invalid column name '%s': %w", colName, err)
		}
	}

	// Get database connection
	db, err := s.getDBConnection(userID, projectID)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Check if the table has an 'id' column before attempting RETURNING id
	// PostgreSQL stores identifiers in lowercase in information_schema unless quoted
	// So we compare using LOWER() to handle case-insensitive matching
	// Also check the 'public' schema (default schema)
	var hasIDColumn bool
	err = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 
			FROM information_schema.columns 
			WHERE table_schema = 'public' 
			AND LOWER(table_name) = LOWER($1) 
			AND column_name = 'id'
		)
	`, req.Table).Scan(&hasIDColumn)
	if err != nil {
		// If we can't check, assume no id column and proceed without RETURNING
		hasIDColumn = false
	}

	// Build INSERT query with parameterized values
	columns := make([]string, 0, len(req.Values))
	placeholders := make([]string, 0, len(req.Values))
	values := make([]interface{}, 0, len(req.Values))
	paramIndex := 1

	// Preserve column order by iterating in a deterministic way
	colOrder := make([]string, 0, len(req.Values))
	for col := range req.Values {
		colOrder = append(colOrder, col)
	}

	// Build columns and values arrays
	for _, col := range colOrder {
		val := req.Values[col]
		columns = append(columns, pq.QuoteIdentifier(col))
		placeholders = append(placeholders, fmt.Sprintf("$%d", paramIndex))
		values = append(values, val)
		paramIndex++
	}

	// Build columns and placeholders strings
	columnsStr := ""
	placeholdersStr := ""
	for i, col := range columns {
		if i > 0 {
			columnsStr += ", "
			placeholdersStr += ", "
		}
		columnsStr += col
		placeholdersStr += placeholders[i]
	}

	// Use pq.QuoteIdentifier for table name
	tableName := pq.QuoteIdentifier(req.Table)

	// Try to use RETURNING id if the table has an id column
	if hasIDColumn {
		queryWithReturning := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING id",
			tableName, columnsStr, placeholdersStr)

		var rowID int64
		err = db.QueryRow(queryWithReturning, values...).Scan(&rowID)
		if err == nil {
			// Successfully got the id
			return &InsertRowResponse{RowID: rowID}, nil
		}

		// If QueryRow failed, check if it's a column not found error
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "42703" {
			// Column doesn't actually exist (maybe the check was wrong), fall through to Exec
			// This handles edge cases where information_schema check was incorrect
		} else {
			// Some other error occurred (constraint violation, data type mismatch, etc.)
			// Return the error as it's likely a real problem
			return nil, fmt.Errorf("failed to insert row into table %s: %w", req.Table, err)
		}
	}

	// Either table doesn't have id column, or RETURNING id failed/not available
	// Execute INSERT without RETURNING
	queryWithoutReturning := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName, columnsStr, placeholdersStr)

	result, execErr := db.Exec(queryWithoutReturning, values...)
	if execErr != nil {
		return nil, fmt.Errorf("failed to insert row into table %s: %w", req.Table, execErr)
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return nil, errors.New("no rows were inserted")
	}

	// If successful but no id returned, return 0 as row_id
	// The client will need to query the table to find the inserted row
	return &InsertRowResponse{RowID: 0}, nil
}

type DeleteRowRequest struct {
	TableName string `json:"table_name" binding:"required"`
}

// DeleteRow deletes a row from a table by ID
func (s *ProjectService) DeleteRow(
	userID uuid.UUID,
	projectID uuid.UUID,
	req DeleteRowRequest,
	rowID string,
) error {

	if err := validateIdentifier(req.TableName); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}

	db, err := s.getDBConnection(userID, projectID)
	if err != nil {
		return err
	}
	defer db.Close()

	rowIDInt, err := strconv.ParseInt(rowID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid row id: %w", err)
	}

	query := fmt.Sprintf(
		`DELETE FROM %s WHERE customer_id = $1`,
		pq.QuoteIdentifier(req.TableName),
	)

	result, err := db.Exec(query, rowIDInt)
	if err != nil {
		return fmt.Errorf("failed to delete row: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return errors.New("row not found")
	}

	return nil
}

// AddColumnRequest represents the request body for adding a column
type AddColumnRequest struct {
	TableName string      `json:"table_name" binding:"required"`
	Name      string      `json:"name" binding:"required"`
	Type      string      `json:"type" binding:"required"`
	Default   interface{} `json:"default,omitempty"`
}

// AddColumnResponse represents the response for adding a column
type AddColumnResponse struct {
	ColumnID int64 `json:"column_id"`
}

// AddColumn adds a column to a table
func (s *ProjectService) AddColumn(userID uuid.UUID, projectID uuid.UUID, req AddColumnRequest) (*AddColumnResponse, error) {
	// Validate table name
	if err := validateIdentifier(req.TableName); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}

	// Validate column name
	if err := validateIdentifier(req.Name); err != nil {
		return nil, fmt.Errorf("invalid column name: %w", err)
	}

	// Validate type is not empty
	if req.Type == "" {
		return nil, errors.New("column type cannot be empty")
	}

	// Get database connection
	db, err := s.getDBConnection(userID, projectID)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Build ALTER TABLE query
	tableNameQuoted := pq.QuoteIdentifier(req.TableName)
	columnNameQuoted := pq.QuoteIdentifier(req.Name)

	// Build the ALTER TABLE statement
	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableNameQuoted, columnNameQuoted, req.Type)

	// Add DEFAULT clause if provided
	// Since default is omitempty, if it's nil, the field might not be in the JSON
	// We'll only add DEFAULT if it's explicitly provided (handled by binding:"omitempty")
	// For now, we'll use the value as-is in the SQL, but this is not ideal for security
	// A better approach would be to validate and quote properly based on type
	if req.Default != nil {
		// Format default value based on type
		switch v := req.Default.(type) {
		case string:
			// Escape single quotes in strings
			escaped := strings.ReplaceAll(v, "'", "''")
			query += fmt.Sprintf(" DEFAULT '%s'", escaped)
		case bool:
			if v {
				query += " DEFAULT TRUE"
			} else {
				query += " DEFAULT FALSE"
			}
		default:
			// For numbers and other types, use as-is (they should be safe)
			query += fmt.Sprintf(" DEFAULT %v", v)
		}
	}

	// Execute query
	_, err = db.Exec(query)
	if err != nil {
		return nil, fmt.Errorf("failed to add column: %w", err)
	}

	// Get the column's ordinal position as column_id
	// PostgreSQL stores column information in information_schema.columns
	var columnID int64
	err = db.QueryRow(`
		SELECT ordinal_position 
		FROM information_schema.columns 
		WHERE table_name = $1 AND column_name = $2
	`, req.TableName, req.Name).Scan(&columnID)
	if err != nil {
		// If we can't get the column_id, return 0
		columnID = 0
	}

	return &AddColumnResponse{ColumnID: columnID}, nil
}

// DeleteColumnRequest represents the request body for deleting a column
type DeleteColumnRequest struct {
	TableName string `json:"table_name" binding:"required"`
}

// DeleteColumn deletes a column from a table
func (s *ProjectService) DeleteColumn(userID uuid.UUID, projectID uuid.UUID, req DeleteColumnRequest, columnName string) error {
	// Validate table name
	if err := validateIdentifier(req.TableName); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}

	// Validate column name
	if err := validateIdentifier(columnName); err != nil {
		return fmt.Errorf("invalid column name: %w", err)
	}

	// Get database connection
	db, err := s.getDBConnection(userID, projectID)
	if err != nil {
		return err
	}
	defer db.Close()

	// Build ALTER TABLE DROP COLUMN query
	tableNameQuoted := pq.QuoteIdentifier(req.TableName)
	columnNameQuoted := pq.QuoteIdentifier(columnName)
	query := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableNameQuoted, columnNameQuoted)

	// Execute query
	_, err = db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to delete column: %w", err)
	}

	return nil
}

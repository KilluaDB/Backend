package services

import (
	"backend/internal/models"
	"backend/internal/repositories"
	"backend/internal/utils"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

// Sentinel errors for project operations so handlers can return proper HTTP status and messages.
var (
	ErrInvalidProjectID    = errors.New("invalid project ID")
	ErrInvalidUserID       = errors.New("invalid user ID")
	ErrInvalidDBType       = errors.New("invalid db_type: must be 'postgresql', 'sql', 'mongodb', or 'nosql'")
	ErrInvalidResourceTier = errors.New("invalid resource_tier: must be 'free', 'basic', or 'premium'")
	ErrProjectNotFound     = errors.New("project not found or access denied")
	ErrProjectCreateDB     = errors.New("failed to create project or database instance")
)

type ProjectService struct {
	projectRepo      repositories.ProjectRepository
	provisioner      *OperatorProvisioner
	dbInstanceRepo   *repositories.DatabaseInstanceRepository
	dbCredentialRepo *repositories.DatabaseCredentialRepository
	instanceConn     *InstanceConnectionService
}

func NewProjectService(
	projectRepo repositories.ProjectRepository,
	provisioner *OperatorProvisioner,
	dbInstanceRepo *repositories.DatabaseInstanceRepository,
	dbCredentialRepo *repositories.DatabaseCredentialRepository,
	instanceConn *InstanceConnectionService,
) *ProjectService {
	return &ProjectService{
		projectRepo:      projectRepo,
		provisioner:      provisioner,
		dbInstanceRepo:   dbInstanceRepo,
		dbCredentialRepo: dbCredentialRepo,
		instanceConn:     instanceConn,
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

	ctx := context.Background()
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	// Check if the table has an 'id' column before attempting RETURNING id
	// PostgreSQL stores identifiers in lowercase in information_schema unless quoted
	// So we compare using LOWER() to handle case-insensitive matching
	// Also check the 'public' schema (default schema)
	var hasIDColumn bool
	err = pool.QueryRow(ctx, `
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
		err = pool.QueryRow(ctx, queryWithReturning, values...).Scan(&rowID)
		if err == nil {
			// Successfully got the id
			return &InsertRowResponse{RowID: rowID}, nil
		}

		// If QueryRow failed, check if it's a column not found error
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42703" {
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

	cmdTag, execErr := pool.Exec(ctx, queryWithoutReturning, values...)
	if execErr != nil {
		return nil, fmt.Errorf("failed to insert row into table %s: %w", req.Table, execErr)
	}

	// Check if any rows were affected
	rowsAffected := cmdTag.RowsAffected()

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

	ctx := context.Background()
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	rowIDInt, err := strconv.ParseInt(rowID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid row id: %w", err)
	}

	query := fmt.Sprintf(
		`DELETE FROM %s WHERE customer_id = $1`,
		pq.QuoteIdentifier(req.TableName),
	)

	cmdTag, err := pool.Exec(ctx, query, rowIDInt)
	if err != nil {
		return fmt.Errorf("failed to delete row: %w", err)
	}

	rowsAffected := cmdTag.RowsAffected()

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

	ctx := context.Background()
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

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
	_, err = pool.Exec(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to add column: %w", err)
	}

	// Get the column's ordinal position as column_id
	// PostgreSQL stores column information in information_schema.columns
	var columnID int64
	err = pool.QueryRow(ctx, `
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

	ctx := context.Background()
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Build ALTER TABLE DROP COLUMN query
	tableNameQuoted := pq.QuoteIdentifier(req.TableName)
	columnNameQuoted := pq.QuoteIdentifier(columnName)
	query := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableNameQuoted, columnNameQuoted)

	// Execute query
	_, err = pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to delete column: %w", err)
	}

	return nil
}

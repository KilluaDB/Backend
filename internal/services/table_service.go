package services

import (
	"backend/internal/repositories"
	"backend/internal/utils"
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "log"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type TableService struct {
	projectRepo     *repositories.ProjectRepository
	instanceRepo    *repositories.DatabaseInstanceRepository
	credentialsRepo *repositories.DatabaseCredentialRepository
	executeRepo     *repositories.QueryHistoryRepository
	tableRepo       *repositories.TableRepository
	orchestrator    *OrchestratorService
}

func NewTableService(
	projectRepo *repositories.ProjectRepository,
	instanceRepo *repositories.DatabaseInstanceRepository,
	credentialsRepo *repositories.DatabaseCredentialRepository,
	executeRepo *repositories.QueryHistoryRepository,
	tableRepo *repositories.TableRepository,
	orchestrator *OrchestratorService,
) *TableService {
	return &TableService{
		projectRepo:     projectRepo,
		instanceRepo:    instanceRepo,
		credentialsRepo: credentialsRepo,
		executeRepo:     executeRepo,
		tableRepo:       tableRepo,
		orchestrator:    orchestrator,
	}
}

type Column struct {
	Name       string  `json:"name" binding:"required"`
	Type       string  `json:"type" binding:"required"`
	Default    *string `json:"default"`
	Primary    bool    `json:"primary"`
	IsUnique   bool    `json:"is_unique"`
	IsIdentity bool    `json:"is_identity"`
	Nullable   bool    `json:"nullable"`
}

type ForeignKeyRef struct {
	LocalColumn   string `json:"local_column" binding:"required"`
	ForeignColumn string `json:"foreign_column" binding:"required"`
	OnUpdate      string `json:"on_update" binding:"omitempty, oneof=CASCADE RESTRICT NO ACTION"`
	OnDelete      string `json:"on_delete" binding:"omitempty, oneof=CASCADE RESTRICT NO ACTION SET NULL SET DEFAULT"`
}

type ForeignKey struct {
	Schema     string          `json:"schema" binding:"required"`
	Table      string          `json:"table" binding:"required"`
	References []ForeignKeyRef `json:"references" binding:"required, min=1"`
}

type CreateTableRequest struct {
	Schema      string      `json:"schema" binding:"required"`
	Table       string      `json:"table" binding:"required"`
	Columns     []Column    `json:"columns" binding:"required"`
	ForeignKeys *ForeignKey `json:"foreign_keys"`
}

type UpdateTableRequest struct {
	Schema      string      `json:"schema"`
	Table       string      `json:"table"`
	Columns     []Column    `json:"columns"`
	ForeignKeys *ForeignKey `json:"foreign_keys"`
}

type DeleteTableRequest struct {
	Schema string `json:"schema" binding:"required"`
	Table  string `json:"table" binding:"required"`
}

func (s *TableService) CreateTable(req *CreateTableRequest, userId uuid.UUID, projectId uuid.UUID) (*sql.Result, error) {
	// Validate request
	if err := s.validateCreateTableRequest(req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	sqlDb, err := s.openDbConnection(userId, projectId)
	if err != nil {
		return nil, err
	}
	defer sqlDb.Close()

	// Start transaction
	tx, err := sqlDb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	query, err := s.parseCreateQuery(req)
	if err != nil {
		return nil, err
	}

	result, err := tx.Exec(query)
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &result, nil
}

func (s *TableService) DeleteTable(req *DeleteTableRequest, userId uuid.UUID, projectId uuid.UUID) (*sql.Result, error) {
	// Validate identifiers
	if !isValidIdentifier(req.Schema) {
		return nil, errors.New("invalid schema name")
	}
	if !isValidIdentifier(req.Table) {
		return nil, errors.New("invalid table name")
	}

	sqlDb, err := s.openDbConnection(userId, projectId)
	if err != nil {
		return nil, err
	}
	defer sqlDb.Close()

	// Start transaction
	tx, err := sqlDb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := s.tableRepo.Delete(tx, req.Schema, req.Table)
	if err != nil {
		return nil, fmt.Errorf("failed to delete table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &result, nil
}

// func (s *TableService) UpdateTable(req *UpdateTableRequest, userId uuid.UUID, projectId uuid.UUID) (*sql.Result, error) {
// 	sqlDb, err := s.openDbConnection(userId, projectId)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer sqlDb.Close()

// 	return nil, nil
// }

func (s *TableService) parseCreateQuery(req *CreateTableRequest) (string, error) {
	if req.Schema == "" {
		req.Schema = "public"
	}

	// Use quoted identifiers to prevent SQL injection
	query := fmt.Sprintf("CREATE TABLE \"%s\".\"%s\" (\n", req.Schema, req.Table)
	for i, col := range req.Columns {
		columnDef := fmt.Sprintf("  \"%s\" %s", col.Name, col.Type)

		if col.IsIdentity {
			columnDef += " GENERATED ALWAYS AS IDENTITY"
		}

		if col.Primary {
			columnDef += " PRIMARY KEY"
		}

		if col.IsUnique {
			columnDef += " UNIQUE"
		}

		if !col.Nullable {
			columnDef += " NOT NULL"
		}

		if col.Default != nil && *col.Default != "" {
			columnDef += fmt.Sprintf(" DEFAULT %s", *col.Default)
		}

		// Add comma for all but last column, or if FK exists
		if i < len(req.Columns)-1 || (req.ForeignKeys != nil && len(req.ForeignKeys.References) > 0) {
			columnDef += ","
		}

		query += columnDef + "\n"
	}

	if req.ForeignKeys != nil && len(req.ForeignKeys.References) > 0 {
		for i, fk := range req.ForeignKeys.References {
			fkDef := fmt.Sprintf("  FOREIGN KEY (\"%s\") REFERENCES \"%s\".\"%s\"(\"%s\")",
				fk.LocalColumn,
				req.ForeignKeys.Schema,
				req.ForeignKeys.Table,
				fk.ForeignColumn,
			)

			if fk.OnDelete != "" {
				fkDef += " ON DELETE " + fk.OnDelete
			}

			if fk.OnUpdate != "" {
				fkDef += " ON UPDATE " + fk.OnUpdate
			}

			// No comma on last FK
			if i < len(req.ForeignKeys.References)-1 {
				fkDef += ","
			}

			query += fkDef + "\n"
		}
	}
	query += ");\n"

	return query, nil

	/*
		{
			"schema": 	"public",
			"table": 	"users",
			"columns":	[
				{
					"name": 			"id",
					"type": 			"INT",
					"primary": 		true,
					"is_unique": 	true,
					"is_identity": true,
					"nullable": 	false
				},
				{
					"name": 			"first_name",
					"type": 			"VARCHAR(50)",
					"nullable": 	false
				},
				{
					"name": 			"last_name",
					"type": 			"VARCHAR(50)",
					"nullable": 	false
				},
				{
					"name": 			"department_id",
					"type": 			"INT",
					"nullable": 	false
				}
			],
			"foreign_keys": [
				{
					"schema":	"public",
					"table":		"users",
					"references": [
						{
							"local_column": 	"department_id",
							"foreign_column": "id",
							"on_update": 		"CASCADE",
							"on_delete": 		"SET NULL"
						}
					]
				}
			]
		}
	*/
}

// isValidIdentifier checks if a string is a valid PostgreSQL identifier
func isValidIdentifier(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	// PostgreSQL identifiers: start with letter or underscore, followed by letters, digits, underscores, or dollar signs
	matched, _ := regexp.MatchString(`^[a-zA-Z_][a-zA-Z0-9_$]*$`, name)
	return matched
}

// validateCreateTableRequest validates the create table request
func (s *TableService) validateCreateTableRequest(req *CreateTableRequest) error {
	if req.Schema == "" {
		req.Schema = "public"
	}

	if !isValidIdentifier(req.Schema) {
		return errors.New("invalid schema name")
	}
	if !isValidIdentifier(req.Table) {
		return errors.New("invalid table name")
	}

	if len(req.Columns) == 0 {
		return errors.New("at least one column is required")
	}

	// Validate column names and types
	for i, col := range req.Columns {
		if !isValidIdentifier(col.Name) {
			return fmt.Errorf("invalid column name at index %d: %s", i, col.Name)
		}
		if col.Type == "" {
			return fmt.Errorf("column type is required for column: %s", col.Name)
		}
		// Validate column type (basic check)
		if !isValidColumnType(col.Type) {
			return fmt.Errorf("invalid column type for %s: %s", col.Name, col.Type)
		}
	}

	// Validate foreign keys if present
	if req.ForeignKeys != nil {
		if !isValidIdentifier(req.ForeignKeys.Schema) {
			return errors.New("invalid foreign key schema name")
		}
		if !isValidIdentifier(req.ForeignKeys.Table) {
			return errors.New("invalid foreign key table name")
		}
		for _, ref := range req.ForeignKeys.References {
			if !isValidIdentifier(ref.LocalColumn) || !isValidIdentifier(ref.ForeignColumn) {
				return errors.New("invalid foreign key column name")
			}
		}
	}

	return nil
}

// isValidColumnType validates PostgreSQL column types
func isValidColumnType(colType string) bool {
	// Convert to uppercase for comparison
	upper := strings.ToUpper(colType)
	validTypes := []string{
		"INT", "INTEGER", "BIGINT", "SMALLINT", "SERIAL", "BIGSERIAL",
		"DECIMAL", "NUMERIC", "REAL", "DOUBLE PRECISION",
		"BOOLEAN", "BOOL",
		"CHAR", "VARCHAR", "TEXT",
		"DATE", "TIME", "TIMESTAMP", "TIMESTAMPTZ", "INTERVAL",
		"UUID", "JSON", "JSONB", "BYTEA",
	}

	// Check exact match or parameterized types like VARCHAR(50)
	for _, valid := range validTypes {
		if strings.HasPrefix(upper, valid) {
			return true
		}
	}
	return false
}

func (s *TableService) openDbConnection(userId uuid.UUID, projectId uuid.UUID) (*sql.DB, error) {
	project, err := s.projectRepo.GetByIDAndUserID(projectId, userId)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found or not accessible")
	}

	dbInstance, err := s.instanceRepo.GetRunningByProjectID(projectId)
	if err != nil {
		return nil, err
	}
	if dbInstance == nil {
		return nil, errors.New("no running database instance for this project")
	}

	dbCred, err := s.credentialsRepo.GetLatestByInstanceID(dbInstance.ID)
	if err != nil {
		return nil, err
	}
	if dbCred == nil {
		return nil, errors.New("no credentials configured for this database instance")
	}

	if dbInstance.ContainerID == nil || *dbInstance.ContainerID == "" {
		return nil, errors.New("database instance container ID not configured")
	}
	if dbInstance.Port == nil {
		return nil, errors.New("database instance port not configured")
	}

	// Get container IP from orchestrator
	containerIP, ok := s.orchestrator.GetContainerIP(*dbInstance.ContainerID)
	if !ok {
		// Try to get from Redis as fallback
		var err error
		containerIP, err = s.orchestrator.GetContainerIPFromRedis(context.Background(), *dbInstance.ContainerID)
		if err != nil {
			return nil, fmt.Errorf("failed to get container IP: %w", err)
		}
	}

	dbPassword, err := utils.DecryptString(dbCred.PasswordEncrypted)
	if err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		containerIP,
		*dbInstance.Port,
		dbCred.Username,
		dbPassword,
		"postgres",
	)

	sqlDb, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	return sqlDb, nil
}

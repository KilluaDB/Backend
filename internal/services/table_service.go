package services

import (
	"database/sql"
	"errors"
	"fmt"
	_ "log"
	"my_project/internal/repositories"
	"my_project/internal/utils"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type TableService struct {
	projectRepo *repositories.ProjectRepository
	instanceRepo *repositories.DatabaseInstanceRepository
	credentialsRepo *repositories.DatabaseCredentialRepository
	executeRepo *repositories.QueryHistoryRepository
	tableRepo *repositories.TableRepository
}

func NewTableService(
	projectRepo *repositories.ProjectRepository,
	instanceRepo *repositories.DatabaseInstanceRepository,
	credentialsRepo *repositories.DatabaseCredentialRepository,
	executeRepo *repositories.QueryHistoryRepository,
	tableRepo *repositories.TableRepository,
) *TableService {
	return &TableService {
		projectRepo: projectRepo,
		instanceRepo: instanceRepo,
		credentialsRepo: credentialsRepo,
		executeRepo: executeRepo,
		tableRepo: tableRepo,
	}
}

type Column struct {
	Name			string	`json:"name" binding:"required"`
	Type 			string	`json:"type" binding:"required"`
	Default		*string	`json:"default"`
	Primary		bool		`json:"primary"`
	IsUnique 	bool		`json:"is_unique"`
	IsIdentity	bool		`json:"is_identity"`
	Nullable		bool		`json:"nullable"`
}

type ForeignKeyRef struct {
	LocalColumn		string	`json:"local_column" binding:"required"`
	ForeignColumn	string	`json:"foreign_column" binding:"required"`
	OnUpdate			string	`json:"on_update" binding:"omitempty, oneof=CASCADE RESTRICT NO ACTION"`
	OnDelete			string	`json:"on_delete" binding:"omitempty, oneof=CASCADE RESTRICT NO ACTION SET NULL SET DEFAULT"`
}

type ForeignKey struct {
	Schema 		string				`json:"schema" binding:"required"`
	Table 		string				`json:"table" binding:"required"`
	References	[]ForeignKeyRef	`json:"references" binding:"required, min=1"`
}

type CreateTableRequest struct {
	Schema 		string			`json:"schema" binding:"required"`
	Table 		string			`json:"table" binding:"required"`
	Columns 		[]Column			`json:"columns" binding:"required"`
	ForeignKeys *ForeignKey		`json:"foreign_keys"`
}

type UpdateTableRequest struct {
	Schema 		string			`json:"schema"`
	Table 		string			`json:"table"`
	Columns 		[]Column			`json:"columns"`
	ForeignKeys *ForeignKey		`json:"foreign_keys"`
}

type DeleteTableRequest struct {
	Schema 		string			`json:"schema" binding:"required"`
	Table 		string			`json:"table" binding:"required"`
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

	if dbInstance.Endpoint == nil || dbInstance.Port == nil {
		return nil, errors.New("database instance endpoint or port not configured")
	}

	dbPassword, err := utils.DecryptString(dbCred.PasswordEncrypted)
	if err != nil {
		return nil, err
	}	

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", 
		*dbInstance.Endpoint,
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
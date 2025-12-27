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
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type QueryService struct {
	projectRepo  *repositories.ProjectRepository
	instanceRepo *repositories.DatabaseInstanceRepository
	credRepo     *repositories.DatabaseCredentialRepository
	execRepo     *repositories.QueryHistoryRepository
	orchestrator *OrchestratorService
}

func NewQueryService(projectRepo *repositories.ProjectRepository, instanceRepo *repositories.DatabaseInstanceRepository, credRepo *repositories.DatabaseCredentialRepository, execRepo *repositories.QueryHistoryRepository, orchestrator *OrchestratorService) *QueryService {
	return &QueryService{
		projectRepo:  projectRepo,
		instanceRepo: instanceRepo,
		credRepo:     credRepo,
		execRepo:     execRepo,
		orchestrator: orchestrator,
	}
}

type QueryResult struct {
	Columns       []string                 `json:"columns"`
	Rows          []map[string]interface{} `json:"rows"`
	RowCount      int                      `json:"row_count"`
	RowsAffected  int64                    `json:"rows_affected,omitempty"`
	ExecutionTime int64                    `json:"execution_time_ms"`
	Error         string                   `json:"error,omitempty"`
}

type ExecuteQueryRequest struct {
	Query string `json:"query" binding:"required"`
}

// ValidateSQLQuery validates SQL queries to prevent dangerous operations
func (s *QueryService) ValidateSQLQuery(query string) error {
	// Trim + uppercase
	normalized := strings.ToUpper(strings.TrimSpace(query))

	// Remove comments
	commentPattern := regexp.MustCompile(`--.*|/\*[\s\S]*?\*/`)
	normalized = commentPattern.ReplaceAllString(normalized, "")
	normalized = strings.TrimSpace(normalized)

	if normalized == "" {
		return errors.New("query cannot be empty")
	}

	// Block dangerous operations
	dangerousKeywords := []string{
		"DROP DATABASE",
		"DROP SCHEMA",
		"TRUNCATE",
		"DELETE FROM", // Allow DELETE but require WHERE clause
		"ALTER DATABASE",
		"CREATE DATABASE",
		"CREATE SCHEMA",
	}

	for _, keyword := range dangerousKeywords {
		if strings.Contains(normalized, keyword) {
			// Special handling for DELETE - allow if it has WHERE clause
			if keyword == "DELETE FROM" {
				if !strings.Contains(normalized, "WHERE") {
					return errors.New("DELETE statements must include a WHERE clause for safety")
				}
				continue
			}
			return fmt.Errorf("operation '%s' is not allowed for security reasons", keyword)
		}
	}

	// Check for multiple statements (prevent SQL injection via multiple statements)
	// TODO: Single statements with multiple semicolons are allowed
	if strings.Contains(normalized, ";") && len(strings.Split(normalized, ";")) > 2 {
		// Allow single semicolon at the end
		parts := strings.Split(normalized, ";")
		nonEmptyParts := 0
		for _, part := range parts {
			if strings.TrimSpace(part) != "" {
				nonEmptyParts++
			}
		}
		if nonEmptyParts > 1 {
			return errors.New("multiple statements are not allowed for security reasons")
		}
	}

	return nil
}

// ExecuteQuery executes a SQL query on the specified database connection
func (s *QueryService) ExecuteQuery(userID uuid.UUID, req *ExecuteQueryRequest, projectId uuid.UUID) (*QueryResult, *models.QueryHistory, error) {
	startTime := time.Now()

	// Validate project ownership
	project, err := s.projectRepo.GetByIDAndUserID(projectId, userID)
	if err != nil {
		return nil, nil, err
	}
	if project == nil {
		return nil, nil, errors.New("project not found or not accessible")
	}

	// Find running DB instance for this project
	inst, err := s.instanceRepo.GetRunningByProjectID(projectId)
	if err != nil {
		return nil, nil, err
	}
	if inst == nil {
		return nil, nil, errors.New("no running database instance for this project")
	}

	// Fetch credentials for the instance
	cred, err := s.credRepo.GetLatestByInstanceID(inst.ID)
	if err != nil {
		return nil, nil, err
	}
	if cred == nil {
		return nil, nil, errors.New("no credentials configured for this database instance")
	}

	// Validate query
	if err := s.ValidateSQLQuery(req.Query); err != nil {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		exec := &models.QueryHistory{
			DBInstanceID:    inst.ID,
			UserID:          userID,
			QueryText:       req.Query,
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &[]int{int(execTime)}[0],
		}
		_ = s.execRepo.Create(exec)
		return &QueryResult{Error: err.Error(), ExecutionTime: execTime}, exec, nil
	}

	// Validate container_id exists
	if inst.ContainerID == nil || *inst.ContainerID == "" {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		exec := &models.QueryHistory{
			DBInstanceID:    inst.ID,
			UserID:          userID,
			QueryText:       req.Query,
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &[]int{int(execTime)}[0],
		}
		_ = s.execRepo.Create(exec)
		return &QueryResult{Error: "database instance container ID not configured", ExecutionTime: execTime}, exec, nil
	}

	// Get current IP from orchestrator
	ip, ok := s.orchestrator.GetContainerIP(*inst.ContainerID)
	if !ok {
		// Try Redis as fallback
		var err error
		ip, err = s.orchestrator.GetContainerIPFromRedis(context.Background(), *inst.ContainerID)
		if err != nil {
			execTime := time.Since(startTime).Milliseconds()
			success := false
			exec := &models.QueryHistory{
				DBInstanceID:    inst.ID,
				UserID:          userID,
				QueryText:       req.Query,
				ExecutedAt:      time.Now(),
				Success:         &success,
				ExecutionTimeMs: &[]int{int(execTime)}[0],
			}
			_ = s.execRepo.Create(exec)
			return &QueryResult{Error: "failed to get container IP from orchestrator", ExecutionTime: execTime}, exec, nil
		}
	}

	// Validate port
	if inst.Port == nil {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		exec := &models.QueryHistory{
			DBInstanceID:    inst.ID,
			UserID:          userID,
			QueryText:       req.Query,
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &[]int{int(execTime)}[0],
		}
		_ = s.execRepo.Create(exec)
		return &QueryResult{Error: "database instance port not configured", ExecutionTime: execTime}, exec, nil
	}

	// Decrypt password before building DSN
	dbPassword, err := utils.DecryptString(cred.PasswordEncrypted)
	if err != nil {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		exec := &models.QueryHistory{
			DBInstanceID:    inst.ID,
			UserID:          userID,
			QueryText:       req.Query,
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &[]int{int(execTime)}[0],
		}
		_ = s.execRepo.Create(exec)
		return &QueryResult{Error: "failed to decrypt database credentials", ExecutionTime: execTime}, exec, nil
	}

	// Build connection string using IP from orchestrator
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		ip, *inst.Port, cred.Username, dbPassword, "postgres")
	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		exec := &models.QueryHistory{
			DBInstanceID:    inst.ID,
			UserID:          userID,
			QueryText:       req.Query,
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &[]int{int(execTime)}[0],
		}
		_ = s.execRepo.Create(exec)
		return &QueryResult{Error: err.Error(), ExecutionTime: execTime}, exec, nil
	}
	defer sqlDB.Close()

	result, err := s.executeSQLQuery(sqlDB, req.Query)
	execTime := time.Since(startTime).Milliseconds()
	result.ExecutionTime = execTime

	success := err == nil && result.Error == ""
	execTimeInt := int(execTime)
	exec := &models.QueryHistory{
		DBInstanceID:    inst.ID,
		UserID:          userID,
		QueryText:       req.Query,
		ExecutedAt:      time.Now(),
		Success:         &success,
		ExecutionTimeMs: &execTimeInt,
	}

	if err != nil || result.Error != "" {
		if err != nil {
			result.Error = err.Error()
		}
	}
	_ = s.execRepo.Create(exec)
	return result, exec, nil
}

// executeSQLQuery executes a SQL query and returns results
func (s *QueryService) executeSQLQuery(db *sql.DB, query string) (*QueryResult, error) {
	// Check if it's a SELECT query or other query type

	normalized := strings.ToUpper(strings.TrimSpace(query))
	isSelect := strings.HasPrefix(normalized, "SELECT") || strings.HasPrefix(normalized, "EXPLAIN SELECT")

	if isSelect {
		return s.executeSelectQuery(db, query)
	}

	// For non-SELECT queries (INSERT, UPDATE, DELETE, etc.)
	return s.executeNonSelectQuery(db, query)
}

// executeSelectQuery executes a SELECT query
func (s *QueryService) executeSelectQuery(db *sql.DB, query string) (*QueryResult, error) {
	rows, err := db.Query(query)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}

	var resultRows []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return &QueryResult{Error: err.Error()}, nil
		}

		rowMap := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if val != nil {
				switch v := val.(type) {
				case []byte:
					rowMap[col] = string(v)
				case time.Time:
					rowMap[col] = v.Format(time.RFC3339)
				default:
					rowMap[col] = v
				}
			} else {
				rowMap[col] = nil
			}
		}
		resultRows = append(resultRows, rowMap)
	}

	if err := rows.Err(); err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}

	return &QueryResult{
		Columns:      columns,
		Rows:         resultRows,
		RowCount:     len(resultRows),
		RowsAffected: int64(len(resultRows)),
	}, nil
}

// executeNonSelectQuery executes non-SELECT queries (INSERT, UPDATE, DELETE, etc.)
func (s *QueryService) executeNonSelectQuery(db *sql.DB, query string) (*QueryResult, error) {
	result, err := db.Exec(query)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}

	return &QueryResult{
		RowsAffected: rowsAffected,
		RowCount:     0,
	}, nil
}

// GetQueryHistory returns query execution history for a user
func (s *QueryService) GetQueryHistory(userID uuid.UUID, limit int) ([]models.QueryHistory, error) {
	return s.execRepo.GetByUserID(userID, limit)
}

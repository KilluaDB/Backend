package services

import (
	"backend/internal/models"
	"backend/internal/repositories"
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type QueryService struct {
	instanceConn *InstanceConnectionService
	execRepo     *repositories.QueryHistoryRepository
}

func NewQueryService(
	instanceConn *InstanceConnectionService,
	execRepo *repositories.QueryHistoryRepository,
) *QueryService {
	return &QueryService{
		instanceConn: instanceConn,
		execRepo:     execRepo,
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
	normalized := strings.ToUpper(strings.TrimSpace(query))

	commentPattern := regexp.MustCompile(`--.*|/\*[\s\S]*?\*/`)
	normalized = commentPattern.ReplaceAllString(normalized, "")
	normalized = strings.TrimSpace(normalized)

	if normalized == "" {
		return errors.New("query cannot be empty")
	}

	dangerousKeywords := []string{
		"DROP DATABASE",
		"DROP SCHEMA",
		"TRUNCATE",
		"DELETE FROM",
		"ALTER DATABASE",
		"CREATE DATABASE",
		"CREATE SCHEMA",
	}

	for _, keyword := range dangerousKeywords {
		if strings.Contains(normalized, keyword) {
			if keyword == "DELETE FROM" {
				if !strings.Contains(normalized, "WHERE") {
					return errors.New("DELETE statements must include a WHERE clause for safety")
				}
				continue
			}
			return errors.New("operation '" + keyword + "' is not allowed for security reasons")
		}
	}

	if strings.Contains(normalized, ";") && len(strings.Split(normalized, ";")) > 2 {
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

// ExecuteQuery executes a SQL query on the project's database instance.
func (s *QueryService) ExecuteQuery(userID uuid.UUID, req *ExecuteQueryRequest, projectId uuid.UUID) (*QueryResult, *models.QueryHistory, error) {
	startTime := time.Now()
	ctx := context.Background()

	if err := s.ValidateSQLQuery(req.Query); err != nil {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		exec := &models.QueryHistory{
			DBInstanceID:    uuid.Nil,
			UserID:          userID,
			QueryText:       req.Query,
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &[]int{int(execTime)}[0],
		}
		_ = s.execRepo.Create(exec)
		return &QueryResult{Error: err.Error(), ExecutionTime: execTime}, exec, nil
	}

	pool, instanceID, err := s.instanceConn.GetPoolWithMeta(ctx, userID, projectId)
	if err != nil {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		exec := &models.QueryHistory{
			DBInstanceID:    instanceID,
			UserID:          userID,
			QueryText:       req.Query,
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &[]int{int(execTime)}[0],
		}
		_ = s.execRepo.Create(exec)
		return &QueryResult{Error: err.Error(), ExecutionTime: execTime}, exec, nil
	}
	defer pool.Close()

	result, err := s.executeSQLQuery(ctx, pool, req.Query)
	execTime := time.Since(startTime).Milliseconds()
	result.ExecutionTime = execTime

	success := err == nil && result.Error == ""
	execTimeInt := int(execTime)
	exec := &models.QueryHistory{
		DBInstanceID:    instanceID,
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

func (s *QueryService) executeSQLQuery(ctx context.Context, pool *pgxpool.Pool, query string) (*QueryResult, error) {
	normalized := strings.ToUpper(strings.TrimSpace(query))
	isSelect := strings.HasPrefix(normalized, "SELECT") || strings.HasPrefix(normalized, "EXPLAIN SELECT")

	if isSelect {
		return s.executeSelectQuery(ctx, pool, query)
	}
	return s.executeNonSelectQuery(ctx, pool, query)
}

func (s *QueryService) executeSelectQuery(ctx context.Context, pool *pgxpool.Pool, query string) (*QueryResult, error) {
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		columns[i] = string(fd.Name)
	}

	var resultRows []map[string]interface{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return &QueryResult{Error: err.Error()}, nil
		}
		rowMap := make(map[string]interface{})
		for i, col := range columns {
			val := vals[i]
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

func (s *QueryService) executeNonSelectQuery(ctx context.Context, pool *pgxpool.Pool, query string) (*QueryResult, error) {
	cmdTag, err := pool.Exec(ctx, query)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}

	return &QueryResult{
		RowsAffected: cmdTag.RowsAffected(),
		RowCount:     0,
	}, nil
}

// GetQueryHistory returns query execution history for a user
func (s *QueryService) GetQueryHistory(userID uuid.UUID, limit int) ([]models.QueryHistory, error) {
	return s.execRepo.GetByUserID(userID, limit)
}

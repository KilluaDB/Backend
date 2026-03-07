package services

import (
	"backend/internal/database"
	"backend/internal/models"
	"backend/internal/repositories"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type QueryService struct {
	instanceConn *InstanceConnectionService
	execRepo     *repositories.QueryHistoryRepository
	projectRepo  repositories.ProjectRepository
	drivers      database.DriverRegistry
}

func NewQueryService(
	instanceConn *InstanceConnectionService,
	execRepo *repositories.QueryHistoryRepository,
	projectRepo repositories.ProjectRepository,
	drivers database.DriverRegistry,
) *QueryService {
	return &QueryService{
		instanceConn: instanceConn,
		execRepo:     execRepo,
		projectRepo:  projectRepo,
		drivers:      drivers,
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

// MongoQueryRequest describes a MongoDB query operation for unified /query/execute endpoint.
type MongoQueryRequest struct {
	Collection string                 `json:"collection" binding:"required"`
	Operation  string                 `json:"operation" binding:"required"` // find | insertOne | updateMany | deleteMany
	Filter     map[string]interface{} `json:"filter,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"` // for insertOne/updateMany
	Limit      *int                   `json:"limit,omitempty"` // used for find
}

// GetProjectByIDAndUserID is used by handlers to detect DB type with ownership check.
func (s *QueryService) GetProjectByIDAndUserID(ctx context.Context, projectID, userID uuid.UUID) (*models.Project, error) {
	return s.projectRepo.GetByIDAndUserID(ctx, projectID, userID)
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

// ExecuteSQLQuery executes a SQL query on the project's PostgreSQL database instance.
func (s *QueryService) ExecuteSQLQuery(userID uuid.UUID, req *ExecuteQueryRequest, projectId uuid.UUID) (*QueryResult, *models.QueryHistory, error) {
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

// ExecuteMongoQuery executes a MongoDB query operation on the project's MongoDB instance.
// It records the execution in query_history (QueryText stored as JSON of the request).
func (s *QueryService) ExecuteMongoQuery(userID uuid.UUID, req *MongoQueryRequest, projectId uuid.UUID) (interface{}, *models.QueryHistory, error) {
	startTime := time.Now()
	ctx := context.Background()

	// Ownership check + determine DB type
	project, err := s.projectRepo.GetByIDAndUserID(ctx, projectId, userID)
	if err != nil {
		return nil, nil, err
	}
	if project == nil {
		return nil, nil, fmt.Errorf("project not found or access denied")
	}
	if project.DBType != "mongodb" {
		return nil, nil, fmt.Errorf("project db_type is %q (expected mongodb)", project.DBType)
	}

	operation := strings.ToLower(strings.TrimSpace(req.Operation))
	switch operation {
	case "find", "insertone", "updatemany", "deletemany":
	default:
		execTime := time.Since(startTime).Milliseconds()
		success := false
		exec := &models.QueryHistory{
			DBInstanceID:    uuid.Nil,
			UserID:          userID,
			QueryText:       mustJSON(req),
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &[]int{int(execTime)}[0],
		}
		_ = s.execRepo.Create(exec)
		return map[string]any{"error": "unsupported mongo operation"}, exec, nil
	}

	if req.Collection == "" {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		exec := &models.QueryHistory{
			DBInstanceID:    uuid.Nil,
			UserID:          userID,
			QueryText:       mustJSON(req),
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &[]int{int(execTime)}[0],
		}
		_ = s.execRepo.Create(exec)
		return map[string]any{"error": "collection is required"}, exec, nil
	}

	if (operation == "insertone" || operation == "updatemany") && len(req.Data) == 0 {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		exec := &models.QueryHistory{
			DBInstanceID:    uuid.Nil,
			UserID:          userID,
			QueryText:       mustJSON(req),
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &[]int{int(execTime)}[0],
		}
		_ = s.execRepo.Create(exec)
		return map[string]any{"error": "data is required for this operation"}, exec, nil
	}

	driver, err := s.drivers.GetDriver(project.DBType)
	if err != nil {
		return nil, nil, err
	}

	payload := map[string]any{
		"collection": req.Collection,
		"operation":  operation,
		"filter":     req.Filter,
		"data":       req.Data,
	}
	if req.Limit != nil {
		payload["limit"] = *req.Limit
	}

	result, execErr := driver.ExecuteQuery(ctx, projectId.String(), payload)
	execTime := time.Since(startTime).Milliseconds()

	// Best-effort instance id for query history.
	instanceID, instErr := s.instanceConn.GetInstanceID(ctx, userID, projectId)
	if instErr != nil {
		instanceID = uuid.Nil
	}

	success := execErr == nil
	execTimeInt := int(execTime)
	exec := &models.QueryHistory{
		DBInstanceID:    instanceID,
		UserID:          userID,
		QueryText:       mustJSON(req),
		ExecutedAt:      time.Now(),
		Success:         &success,
		ExecutionTimeMs: &execTimeInt,
	}
	_ = s.execRepo.Create(exec)

	if execErr != nil {
		return map[string]any{"error": execErr.Error()}, exec, nil
	}
	return result, exec, nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
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

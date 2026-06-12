package service

import (
	"backend/internal/metrics"
	"backend/internal/model"
	"backend/internal/postgres/infra"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

type QueryHistoryItem struct {
	Query           string  `json:"query"`
	Calls           int64   `json:"calls"`
	TotalTimeMs     float64 `json:"total_time_ms"`
	MeanTimeMs      float64 `json:"mean_time_ms"`
	Rows            int64   `json:"rows"`
	SharedBlksHit   int64   `json:"shared_blks_hit"`
	SharedBlksRead  int64   `json:"shared_blks_read"`
	TempBlksWritten int64   `json:"temp_blks_written"`
}

// Sentinel errors for query operations so handlers can return proper HTTP status.
var (
	ErrInvalidQuery = errors.New("invalid or disallowed query")
)

type QueryService struct {
	instanceConn infra.InstanceConnectionService
	maxLimit     int
}

func NewQueryService(instanceConn infra.InstanceConnectionService, maxLimit int) *QueryService {
	if maxLimit <= 0 {
		maxLimit = 50
	}
	return &QueryService{
		instanceConn: instanceConn,
		maxLimit:     maxLimit,
	}
}

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

	// Enforce LIMIT for SELECT queries to avoid unbounded result sets.
	// isSelect := strings.HasPrefix(normalized, "SELECT") || strings.HasPrefix(normalized, "EXPLAIN SELECT")
	// if isSelect {
	// 	// Simple detection of LIMIT <number> ignoring comments (already stripped above).
	// 	limitRegex := regexp.MustCompile(`\bLIMIT\s+(\d+)\b`)
	// 	matches := limitRegex.FindStringSubmatch(normalized)
	// 	if len(matches) < 2 {
	// 		return fmt.Errorf("SELECT queries must include a LIMIT clause with value <= %d", s.maxLimit)
	// 	}
	// 	limitValueStr := matches[1]
	// 	limitValue, err := strconv.Atoi(limitValueStr)
	// 	if err != nil {
	// 		return fmt.Errorf("unable to parse LIMIT value: %v", err)
	// 	}
	// 	if limitValue > s.maxLimit {
	// 		return fmt.Errorf("LIMIT value %d exceeds the maximum allowed (%d)", limitValue, s.maxLimit)
	// 	}
	// }

	return nil
}

// ExecuteSQLQuery executes a SQL query on the project's PostgreSQL database instance.
func (s *QueryService) ExecuteSQLQuery(ctx context.Context, userID uuid.UUID, req *ExecuteQueryRequest, projectId uuid.UUID) (*QueryResult, *model.QueryHistory, error) {
	startTime := time.Now()

	if err := s.ValidateSQLQuery(req.Query); err != nil {
		instanceID, _ := s.instanceConn.GetInstanceID(ctx, userID, projectId)
		execTime := time.Since(startTime).Milliseconds()
		success := false
		execTimeInt := int(execTime)
		exec := &model.QueryHistory{
			DBInstanceID:    instanceID,
			UserID:          userID,
			QueryText:       req.Query,
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &execTimeInt,
		}
		return &QueryResult{Error: err.Error(), ExecutionTime: execTime}, exec, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
	}

	pool, instanceID2, err := s.instanceConn.GetPoolWithMeta(ctx, userID, projectId)
	if err != nil {
		return nil, nil, err
	}

	result, err := s.executeSQLQuery(ctx, pool, req.Query)
	execTime := time.Since(startTime).Milliseconds()
	if result != nil {
		result.ExecutionTime = execTime
	}

	success := err == nil && (result == nil || result.Error == "")
	execTimeInt := int(execTime)
	exec := &model.QueryHistory{
		DBInstanceID:    instanceID2,
		UserID:          userID,
		QueryText:       req.Query,
		ExecutedAt:      time.Now(),
		Success:         &success,
		ExecutionTimeMs: &execTimeInt,
	}

	if err != nil {
		if result == nil {
			result = &QueryResult{ExecutionTime: execTime}
		}
		result.Error = err.Error()
		return result, exec, err
	}
	if result != nil && result.Error != "" {
		return result, exec, errors.New(result.Error)
	}
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
	start := time.Now()
	rows, err := pool.Query(ctx, query)
	duration := time.Since(start).Seconds()
	metrics.PgQueryDuration.WithLabelValues("select").Observe(duration)
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("postgres", "select").Inc()
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
					if len(v) == 16 {
						if u, err := uuid.FromBytes(v); err == nil {
							rowMap[col] = u.String()
						} else {
							rowMap[col] = string(v)
						}
					} else {
						rowMap[col] = string(v)
					}
				case [16]byte:
					rowMap[col] = uuid.UUID(v).String()
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
	start := time.Now()
	cmdTag, err := pool.Exec(ctx, query)
	duration := time.Since(start).Seconds()
	metrics.PgQueryDuration.WithLabelValues("exec").Observe(duration)
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("postgres", "exec").Inc()
		return &QueryResult{Error: err.Error()}, nil
	}

	return &QueryResult{
		RowsAffected: cmdTag.RowsAffected(),
		RowCount:     0,
	}, nil
}

// GetQueryHistory returns recent user-generated query statistics from pg_stat_statements.
func (s *QueryService) GetQueryHistory(ctx context.Context, userID, projectID uuid.UUID, limit int) ([]QueryHistoryItem, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 30 {
		limit = 30
	}

	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	var hasStatStatements bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&hasStatStatements); err != nil {
		return nil, err
	}
	if !hasStatStatements {
		return []QueryHistoryItem{}, nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	rows, err := pool.Query(queryCtx, `
		SELECT query, calls::bigint, total_exec_time, mean_exec_time, rows::bigint,
		       shared_blks_hit::bigint, shared_blks_read::bigint, temp_blks_written::bigint
		FROM pg_stat_statements
		WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
		  AND userid = (SELECT oid FROM pg_roles WHERE rolname = current_user)
		ORDER BY total_exec_time DESC, calls DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]QueryHistoryItem, 0, limit)
	for rows.Next() {
		var item QueryHistoryItem
		if err := rows.Scan(&item.Query, &item.Calls, &item.TotalTimeMs, &item.MeanTimeMs, &item.Rows, &item.SharedBlksHit, &item.SharedBlksRead, &item.TempBlksWritten); err != nil {
			return nil, err
		}
		// Skip system-generated queries (autovacuum, CHECKPOINT, VACUUM, etc.)
		if isSystemQueryText(item.Query) {
			continue
		}
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}

	return items, rows.Err()
}

// isSystemQueryText filters out non-user queries (autovacuum, background maintenance, etc.)
func isSystemQueryText(query string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(query))

	// Transaction control and empty queries
	if normalized == "BEGIN" || normalized == "COMMIT" || normalized == "ROLLBACK" || normalized == "" {
		return true
	}

	systemPatterns := []string{
		"AUTOVACUUM",
		"CHECKPOINT",
		"VACUUM",
		"ANALYZE",
		"REINDEX",
		"CLUSTER",
		"REFRESH MATERIALIZED VIEW",
		"CALL pg_",                 // system functions
		"SELECT pg_",               // system introspection
		"UPDATE pg_",               // system catalog updates
		"DELETE FROM pg_",          // system catalog deletes
		"INFORMATION_SCHEMA",       // system schema queries
		"PG_CATALOG",               // Postgres catalog
		"PG_STAT_STATEMENTS",       // stats collection itself
		"PG_EXTENSION",             // extension checking
		"PG_STAT_ACTIVITY",         // activity monitoring
		"PG_STAT_DATABASE",         // database statistics
		"PG_STAT_USER_TABLES",      // table statistics
		"FROM pg_",                 // any FROM pg_*
		"JOIN pg_",                 // joins to system catalogs
		"PG_DATABASE_SIZE",         // size functions
		"PG_POSTMASTER_START_TIME", // server uptime
		"PG_IS_IN_RECOVERY",        // recovery status
		"VERSION()",                // server version
		"UNNEST",                   // utility functions
	}

	for _, pattern := range systemPatterns {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}

package services

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"my_project/internal/models"
	"my_project/internal/repositories"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"gorm.io/gorm"
)

type QueryService struct {
	projectRepo  *repositories.ProjectRepository
	instanceRepo *repositories.DatabaseInstanceRepository
	credRepo     *repositories.DatabaseCredentialRepository
	execRepo     *repositories.QueryHistoryRepository
	db           *gorm.DB
}

func NewQueryService(projectRepo *repositories.ProjectRepository, instanceRepo *repositories.DatabaseInstanceRepository, credRepo *repositories.DatabaseCredentialRepository, execRepo *repositories.QueryHistoryRepository, db *gorm.DB) *QueryService {
	return &QueryService{
		projectRepo:  projectRepo,
		instanceRepo: instanceRepo,
		credRepo:     credRepo,
		execRepo:     execRepo,
		db:           db,
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
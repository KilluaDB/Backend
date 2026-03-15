package service

import (
	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

// Sentinel errors for table operations so handlers can return proper HTTP status.
var (
	ErrInvalidTableRequest = errors.New("invalid table request")
	ErrTableAlreadyExists  = errors.New("table already exists")
	ErrTableNotFound       = errors.New("table does not exist")
)

type TableService struct {
	instanceConn InstanceConnectionService
	tableRepo    *repository.TableRepository
}

func NewTableService(
	instanceConn InstanceConnectionService,
	tableRepo *repository.TableRepository,
) *TableService {
	return &TableService{
		instanceConn: instanceConn,
		tableRepo:    tableRepo,
	}
}

func (s *TableService) CreateTable(req *model.CreateTableRequest, userId uuid.UUID, projectId uuid.UUID) (*model.TableOpResult, error) {
	if err := s.validateCreateTableRequest(req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTableRequest, err)
	}

	ctx := context.Background()
	pool, err := s.instanceConn.GetPool(ctx, userId, projectId)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query, err := s.parseCreateQuery(req)
	if err != nil {
		return nil, err
	}

	cmdTag, err := tx.Exec(ctx, query)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P07" { // duplicate_table
			return nil, ErrTableAlreadyExists
		}
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &model.TableOpResult{RowsAffected: cmdTag.RowsAffected()}, nil
}

func (s *TableService) DeleteTable(req *model.DeleteTableRequest, userId uuid.UUID, projectId uuid.UUID) (*model.TableOpResult, error) {
	if !isValidIdentifier(req.Schema) {
		return nil, fmt.Errorf("%w: invalid schema name", ErrInvalidTableRequest)
	}
	if !isValidIdentifier(req.Table) {
		return nil, fmt.Errorf("%w: invalid table name", ErrInvalidTableRequest)
	}

	ctx := context.Background()
	pool, err := s.instanceConn.GetPool(ctx, userId, projectId)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	cmdTag, err := s.tableRepo.Delete(ctx, tx, req.Schema, req.Table)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" { // undefined_table
			return nil, ErrTableNotFound
		}
		return nil, fmt.Errorf("failed to delete table: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &model.TableOpResult{RowsAffected: cmdTag.RowsAffected()}, nil
}

func (s *TableService) parseCreateQuery(req *model.CreateTableRequest) (string, error) {
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
func (s *TableService) validateCreateTableRequest(req *model.CreateTableRequest) error {
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

// validateRowColumnIdentifier validates SQL identifiers (table/column names) for row/column ops.
func validateRowColumnIdentifier(identifier string) error {
	if identifier == "" {
		return errors.New("identifier cannot be empty")
	}
	validPattern := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_\-]*$`)
	if !validPattern.MatchString(identifier) {
		return errors.New("invalid identifier: must start with letter or underscore and contain only alphanumeric characters, underscores, and hyphens")
	}
	return nil
}

// InsertRowRequest represents the request body for inserting a row.
type InsertRowRequest struct {
	Table  string                 `json:"table" binding:"required"`
	Values map[string]interface{} `json:"values" binding:"required"`
}

// InsertRowResponse represents the response for inserting a row.
type InsertRowResponse struct {
	RowID int64 `json:"row_id"`
}

// InsertRow inserts a row into a table.
func (s *TableService) InsertRow(ctx context.Context, userID, projectID uuid.UUID, req InsertRowRequest) (*InsertRowResponse, error) {
	if err := validateRowColumnIdentifier(req.Table); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}
	if len(req.Values) == 0 {
		return nil, errors.New("values cannot be empty")
	}
	for colName := range req.Values {
		if err := validateRowColumnIdentifier(colName); err != nil {
			return nil, fmt.Errorf("invalid column name '%s': %w", colName, err)
		}
	}

	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	var hasIDColumn bool
	_ = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 
			FROM information_schema.columns 
			WHERE table_schema = 'public' 
			AND LOWER(table_name) = LOWER($1) 
			AND column_name = 'id'
		)
	`, req.Table).Scan(&hasIDColumn)

	columns := make([]string, 0, len(req.Values))
	placeholders := make([]string, 0, len(req.Values))
	values := make([]interface{}, 0, len(req.Values))
	paramIndex := 1
	colOrder := make([]string, 0, len(req.Values))
	for col := range req.Values {
		colOrder = append(colOrder, col)
	}
	for _, col := range colOrder {
		val := req.Values[col]
		columns = append(columns, pq.QuoteIdentifier(col))
		placeholders = append(placeholders, fmt.Sprintf("$%d", paramIndex))
		values = append(values, val)
		paramIndex++
	}

	var columnsStr, placeholdersStr string
	for i, col := range columns {
		if i > 0 {
			columnsStr += ", "
			placeholdersStr += ", "
		}
		columnsStr += col
		placeholdersStr += placeholders[i]
	}

	tableName := pq.QuoteIdentifier(req.Table)

	if hasIDColumn {
		queryWithReturning := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING id",
			tableName, columnsStr, placeholdersStr)
		var rowID int64
		err = pool.QueryRow(ctx, queryWithReturning, values...).Scan(&rowID)
		if err == nil {
			return &InsertRowResponse{RowID: rowID}, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42703" {
			// fall through
		} else {
			return nil, fmt.Errorf("failed to insert row into table %s: %w", req.Table, err)
		}
	}

	queryWithoutReturning := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName, columnsStr, placeholdersStr)
	cmdTag, execErr := pool.Exec(ctx, queryWithoutReturning, values...)
	if execErr != nil {
		return nil, fmt.Errorf("failed to insert row into table %s: %w", req.Table, execErr)
	}
	if cmdTag.RowsAffected() == 0 {
		return nil, errors.New("no rows were inserted")
	}
	return &InsertRowResponse{RowID: 0}, nil
}

// DeleteRowRequest represents the request body for deleting a row.
type DeleteRowRequest struct {
	TableName string `json:"table_name" binding:"required"`
}

// DeleteRow deletes a row from a table by ID.
func (s *TableService) DeleteRow(ctx context.Context, userID, projectID uuid.UUID, req DeleteRowRequest, rowID string) error {
	if err := validateRowColumnIdentifier(req.TableName); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	rowIDInt, err := strconv.ParseInt(rowID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid row id: %w", err)
	}
	query := fmt.Sprintf(`DELETE FROM %s WHERE customer_id = $1`, pq.QuoteIdentifier(req.TableName))
	cmdTag, err := pool.Exec(ctx, query, rowIDInt)
	if err != nil {
		return fmt.Errorf("failed to delete row: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return errors.New("row not found")
	}
	return nil
}

// AddColumnRequest represents the request body for adding a column.
type AddColumnRequest struct {
	TableName string      `json:"table_name" binding:"required"`
	Name      string      `json:"name" binding:"required"`
	Type      string      `json:"type" binding:"required"`
	Default   interface{} `json:"default,omitempty"`
}

// AddColumnResponse represents the response for adding a column.
type AddColumnResponse struct {
	ColumnID int64 `json:"column_id"`
}

// AddColumn adds a column to a table.
func (s *TableService) AddColumn(ctx context.Context, userID, projectID uuid.UUID, req AddColumnRequest) (*AddColumnResponse, error) {
	if err := validateRowColumnIdentifier(req.TableName); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}
	if err := validateRowColumnIdentifier(req.Name); err != nil {
		return nil, fmt.Errorf("invalid column name: %w", err)
	}
	if req.Type == "" {
		return nil, errors.New("column type cannot be empty")
	}

	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	tableNameQuoted := pq.QuoteIdentifier(req.TableName)
	columnNameQuoted := pq.QuoteIdentifier(req.Name)
	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableNameQuoted, columnNameQuoted, req.Type)
	if req.Default != nil {
		switch v := req.Default.(type) {
		case string:
			escaped := strings.ReplaceAll(v, "'", "''")
			query += fmt.Sprintf(" DEFAULT '%s'", escaped)
		case bool:
			if v {
				query += " DEFAULT TRUE"
			} else {
				query += " DEFAULT FALSE"
			}
		default:
			query += fmt.Sprintf(" DEFAULT %v", v)
		}
	}
	if _, err = pool.Exec(ctx, query); err != nil {
		return nil, fmt.Errorf("failed to add column: %w", err)
	}

	var columnID int64
	_ = pool.QueryRow(ctx, `
		SELECT ordinal_position 
		FROM information_schema.columns 
		WHERE table_name = $1 AND column_name = $2
	`, req.TableName, req.Name).Scan(&columnID)
	return &AddColumnResponse{ColumnID: columnID}, nil
}

// DeleteColumnRequest represents the request body for deleting a column.
type DeleteColumnRequest struct {
	TableName string `json:"table_name" binding:"required"`
}

// DeleteColumn deletes a column from a table.
func (s *TableService) DeleteColumn(ctx context.Context, userID, projectID uuid.UUID, req DeleteColumnRequest, columnName string) error {
	if err := validateRowColumnIdentifier(req.TableName); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}
	if err := validateRowColumnIdentifier(columnName); err != nil {
		return fmt.Errorf("invalid column name: %w", err)
	}
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	tableNameQuoted := pq.QuoteIdentifier(req.TableName)
	columnNameQuoted := pq.QuoteIdentifier(columnName)
	query := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableNameQuoted, columnNameQuoted)
	if _, err = pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("failed to delete column: %w", err)
	}
	return nil
}

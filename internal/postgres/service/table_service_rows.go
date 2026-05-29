package service

import (
	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GetTables lists base tables in the given schema.
func (s *TableService) GetTables(ctx context.Context, projectID, userID uuid.UUID, schema string) ([]string, error) {
	if err := ValidatePostgresSchemaName(schema); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTableRequest, err)
	}
	schema = PostgresSchema(schema)
	return withProjectPool(s, ctx, userID, projectID, func(pool *pgxpool.Pool) ([]string, error) {
		schemaRepo := repository.NewSchemaRepository(pool)
		return schemaRepo.GetTables(ctx, schema)
	})
}

// GetTableMetadata returns column definitions and key metadata for a table (no row data).
func (s *TableService) GetTableMetadata(ctx context.Context, projectID, userID uuid.UUID, schema, table string) (*model.TableMetadata, error) {
	if err := ValidatePostgresSchemaName(schema); err != nil {
		return nil, err
	}
	schema = PostgresSchema(schema)
	if err := validateRowColumnIdentifier(table); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}

	return withProjectPool(s, ctx, userID, projectID, func(pool *pgxpool.Pool) (*model.TableMetadata, error) {
		schemaRepo := repository.NewSchemaRepository(pool)
		exists, err := schemaRepo.TableExists(ctx, schema, table)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrTableNotFound
		}

		cols, err := schemaRepo.GetColumnDetails(ctx, schema, table)
		if err != nil {
			return nil, err
		}
		pks, err := schemaRepo.GetPrimaryKeys(ctx, schema, table)
		if err != nil {
			return nil, err
		}
		fks, err := schemaRepo.GetForeignKeys(ctx, schema, table)
		if err != nil {
			return nil, err
		}

		return &model.TableMetadata{
			Schema:      schema,
			Table:       table,
			Columns:     cols,
			PrimaryKeys: pks,
			ForeignKeys: fks,
		}, nil
	})
}

// GetRows selects rows with optional equality filter; limit is capped at repository.MaxGetRowsLimit.
// When includeTotal is true, runs an additional COUNT(*) with the same filter (omit for large tables if not needed).
func (s *TableService) GetRows(ctx context.Context, projectID, userID uuid.UUID, schema, table string, filter map[string]interface{}, limit, offset int, includeTotal bool) (*model.GetRowsResult, error) {
	if err := ValidatePostgresSchemaName(schema); err != nil {
		return nil, err
	}
	schema = PostgresSchema(schema)
	if err := validateRowColumnIdentifier(table); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}
	for col := range filter {
		if err := validateRowColumnIdentifier(col); err != nil {
			return nil, fmt.Errorf("invalid column name '%s': %w", col, err)
		}
	}

	return withProjectPool(s, ctx, userID, projectID, func(pool *pgxpool.Pool) (*model.GetRowsResult, error) {
		rows, hasMore, err := s.tableRepo.SelectRows(ctx, pool, schema, table, filter, limit, offset)
		if err != nil {
			return nil, err
		}
		out := &model.GetRowsResult{
			Rows:    rows,
			Limit:   limit,
			Offset:  offset,
			HasMore: hasMore,
		}
		if includeTotal {
			n, err := s.tableRepo.CountRows(ctx, pool, schema, table, filter)
			if err != nil {
				return nil, err
			}
			out.Total = &n
		}
		return out, nil
	})
}

// UpdateRows runs UPDATE with equality filter; nil/empty filter updates all rows.
func (s *TableService) UpdateRows(ctx context.Context, projectID, userID uuid.UUID, schema, table string, filter, update map[string]interface{}) error {
	if err := ValidatePostgresSchemaName(schema); err != nil {
		return err
	}
	schema = PostgresSchema(schema)
	if err := validateRowColumnIdentifier(table); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}
	if len(update) == 0 {
		return errors.New("update cannot be empty")
	}
	for col := range update {
		if err := validateRowColumnIdentifier(col); err != nil {
			return fmt.Errorf("invalid column name '%s': %w", col, err)
		}
	}
	for col := range filter {
		if err := validateRowColumnIdentifier(col); err != nil {
			return fmt.Errorf("invalid column name '%s': %w", col, err)
		}
	}

	return withProjectPoolErr(s, ctx, userID, projectID, func(pool *pgxpool.Pool) error {
		return s.tableRepo.UpdateRows(ctx, pool, schema, table, filter, update)
	})
}

// DeleteRowsByFilter deletes rows matching an optional equality filter; empty filter deletes all rows.
func (s *TableService) DeleteRowsByFilter(ctx context.Context, userID, projectID uuid.UUID, schema, table string, filter map[string]interface{}) error {
	if err := ValidatePostgresSchemaName(schema); err != nil {
		return err
	}
	schema = PostgresSchema(schema)
	if err := validateRowColumnIdentifier(table); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}
	for col := range filter {
		if err := validateRowColumnIdentifier(col); err != nil {
			return fmt.Errorf("invalid column name '%s': %w", col, err)
		}
	}

	return withProjectPoolErr(s, ctx, userID, projectID, func(pool *pgxpool.Pool) error {
		return s.tableRepo.DeleteRowsByFilter(ctx, pool, schema, table, filter)
	})
}

// InsertRowRequest represents the request body for inserting a row (schema from query string).
type InsertRowRequest struct {
	Schema string
	Table  string
	Values map[string]interface{}
}

// InsertRowResponse represents the response for inserting a row.
// RowID is int64 for integer/bigserial primary keys, or string (UUID) for UUID primary keys.
type InsertRowResponse struct {
	RowID interface{} `json:"row_id"`
}

// InsertRow inserts a row into a table.
func (s *TableService) InsertRow(ctx context.Context, userID, projectID uuid.UUID, req InsertRowRequest) (*InsertRowResponse, error) {
	if err := ValidatePostgresSchemaName(req.Schema); err != nil {
		return nil, err
	}
	schema := PostgresSchema(req.Schema)
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

	return withProjectPool(s, ctx, userID, projectID, func(pool *pgxpool.Pool) (*InsertRowResponse, error) {
		rowID, err := s.tableRepo.InsertRow(ctx, pool, schema, req.Table, req.Values)
		if err != nil {
			return nil, err
		}
		return &InsertRowResponse{RowID: rowID}, nil
	})
}

// AddColumnRequest represents the request body for adding a column (schema from query string).
// Default is kept as interface{} for backward compatibility with existing clients that send
// JSON literals like booleans or numbers; it is converted to a SQL literal string internally.
type AddColumnRequest struct {
	Schema      string
	TableName   string
	Name        string
	Type        string
	Default     interface{}
	Primary     bool
	IsUnique    bool
	IsIdentity  bool
	Nullable    bool
	ForeignKeys []model.AddColumnForeignKey
}

// AddColumnResponse represents the response for adding a column.
type AddColumnResponse struct {
	ColumnID int64 `json:"column_id"`
}

// sqlLiteralFromDefault converts a JSON default value into a SQL literal expression suitable for
// inlining into a CREATE/ALTER statement. Strings are treated as raw SQL expressions when they look
// like function calls or numeric/boolean literals, otherwise they are single-quote escaped.
// Returns nil when the input represents an absent default.
func sqlLiteralFromDefault(v interface{}) *string {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
		escaped := "'" + strings.ReplaceAll(x, "'", "''") + "'"
		return &escaped
	case bool:
		s := "FALSE"
		if x {
			s = "TRUE"
		}
		return &s
	default:
		s := fmt.Sprintf("%v", x)
		return &s
	}
}

// AddColumn adds a column (with optional constraints and single-column foreign keys) to a table.
// The whole operation runs in a single transaction so any FK failure rolls back the column too.
func (s *TableService) AddColumn(ctx context.Context, userID, projectID uuid.UUID, req AddColumnRequest) (*AddColumnResponse, error) {
	if err := ValidatePostgresSchemaName(req.Schema); err != nil {
		return nil, err
	}
	schema := PostgresSchema(req.Schema)
	if err := validateRowColumnIdentifier(req.TableName); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}

	def := model.TableColumnDef{
		Name:       req.Name,
		Type:       req.Type,
		Default:    sqlLiteralFromDefault(req.Default),
		Primary:    req.Primary,
		IsUnique:   req.IsUnique,
		IsIdentity: req.IsIdentity,
		Nullable:   req.Nullable,
	}
	if err := validateTableColumnDefs([]model.TableColumnDef{def}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTableRequest, err)
	}

	for i := range req.ForeignKeys {
		fk := &req.ForeignKeys[i]
		fk.Schema = PostgresSchema(fk.Schema)
		fk.Table = strings.TrimSpace(fk.Table)
		if !isValidIdentifier(fk.Schema) {
			return nil, fmt.Errorf("%w: foreign_keys[%d]: invalid schema name", ErrInvalidTableRequest, i)
		}
		if !isValidIdentifier(fk.Table) {
			return nil, fmt.Errorf("%w: foreign_keys[%d]: invalid table name", ErrInvalidTableRequest, i)
		}
		if !isValidIdentifier(fk.ForeignColumn) {
			return nil, fmt.Errorf("%w: foreign_keys[%d]: invalid foreign_column name", ErrInvalidTableRequest, i)
		}
	}

	return withProjectPool(s, ctx, userID, projectID, func(pool *pgxpool.Pool) (*AddColumnResponse, error) {
		schemaRepo := repository.NewSchemaRepository(pool)
		exists, err := schemaRepo.TableExists(ctx, schema, req.TableName)
		if err != nil {
			return nil, fmt.Errorf("failed to check table: %w", err)
		}
		if !exists {
			return nil, ErrTableNotFound
		}

		pkList, err := schemaRepo.GetPrimaryKeys(ctx, schema, req.TableName)
		if err != nil {
			return nil, fmt.Errorf("failed to read primary keys: %w", err)
		}
		tableHasPK := len(pkList) > 0

		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to start transaction: %w", err)
		}
		defer tx.Rollback(ctx)

		if err := s.tableRepo.AddColumnFromDefTx(ctx, tx, schema, req.TableName, def, tableHasPK); err != nil {
			return nil, fmt.Errorf("failed to add column: %w", err)
		}

		for i, fk := range req.ForeignKeys {
			name := generateFKConstraintName(req.TableName, def.Name, fk.Table, i+1)
			if err := s.tableRepo.AddForeignKeyTx(ctx, tx, schema, req.TableName, name, def.Name, fk.Schema, fk.Table, fk.ForeignColumn, normalizeFKRule(fk.OnUpdate), normalizeFKRule(fk.OnDelete)); err != nil {
				return nil, fmt.Errorf("failed to add foreign key to %q.%q: %w", fk.Schema, fk.Table, err)
			}
		}

		var columnID int64
		if err := tx.QueryRow(ctx, `
			SELECT ordinal_position
			FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
		`, schema, req.TableName, def.Name).Scan(&columnID); err != nil {
			return nil, fmt.Errorf("failed to read new column position: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}

		return &AddColumnResponse{ColumnID: columnID}, nil
	})
}

// DeleteColumnRequest identifies the table (schema from query string).
type DeleteColumnRequest struct {
	Schema    string
	TableName string
}

// DeleteColumn deletes a column from a table.
func (s *TableService) DeleteColumn(ctx context.Context, userID, projectID uuid.UUID, req DeleteColumnRequest, columnName string) error {
	if err := ValidatePostgresSchemaName(req.Schema); err != nil {
		return err
	}
	schema := PostgresSchema(req.Schema)
	if err := validateRowColumnIdentifier(req.TableName); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}
	if err := validateRowColumnIdentifier(columnName); err != nil {
		return fmt.Errorf("invalid column name: %w", err)
	}
	return withProjectPoolErr(s, ctx, userID, projectID, func(pool *pgxpool.Pool) error {
		if err := s.tableRepo.DropColumn(ctx, pool, schema, req.TableName, columnName); err != nil {
			return fmt.Errorf("failed to delete column: %w", err)
		}
		return nil
	})
}

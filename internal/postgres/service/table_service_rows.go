package service

import (
	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"
	"context"
	"errors"
	"fmt"

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
type AddColumnRequest struct {
	Schema    string
	TableName string
	Name      string
	Type      string
	Default   interface{}
}

// AddColumnResponse represents the response for adding a column.
type AddColumnResponse struct {
	ColumnID int64 `json:"column_id"`
}

// AddColumn adds a column to a table.
func (s *TableService) AddColumn(ctx context.Context, userID, projectID uuid.UUID, req AddColumnRequest) (*AddColumnResponse, error) {
	if err := ValidatePostgresSchemaName(req.Schema); err != nil {
		return nil, err
	}
	schema := PostgresSchema(req.Schema)
	if err := validateRowColumnIdentifier(req.TableName); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}
	if err := validateRowColumnIdentifier(req.Name); err != nil {
		return nil, fmt.Errorf("invalid column name: %w", err)
	}
	if req.Type == "" {
		return nil, errors.New("column type cannot be empty")
	}
	if !isValidColumnType(req.Type) {
		return nil, fmt.Errorf("%w: invalid column type: %s", ErrInvalidTableRequest, req.Type)
	}

	return withProjectPool(s, ctx, userID, projectID, func(pool *pgxpool.Pool) (*AddColumnResponse, error) {
		var defaultVal interface{}
		if req.Default != nil {
			defaultVal = req.Default
		}

		columnID, err := s.tableRepo.AddColumn(ctx, pool, schema, req.TableName, req.Name, req.Type, defaultVal)
		if err != nil {
			return nil, fmt.Errorf("failed to add column: %w", err)
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

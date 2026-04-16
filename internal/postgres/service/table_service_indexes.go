package service

import (
	"backend/internal/postgres/model"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var allowedIndexMethods = map[string]struct{}{
	"btree": {}, "hash": {}, "gin": {}, "gist": {}, "spgist": {}, "brin": {},
}

func normalizeIndexMethod(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	if m == "" {
		return "btree"
	}
	return m
}

// ListTableIndexes returns index metadata for a table.
func (s *TableService) ListTableIndexes(ctx context.Context, projectID, userID uuid.UUID, schema, table string) ([]model.TableIndexInfo, error) {
	if err := ValidatePostgresSchemaName(schema); err != nil {
		return nil, err
	}
	schema = PostgresSchema(schema)
	if err := validateRowColumnIdentifier(table); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}

	return withProjectPool(s, ctx, userID, projectID, func(pool *pgxpool.Pool) ([]model.TableIndexInfo, error) {
		return s.tableRepo.ListIndexes(ctx, pool, schema, table)
	})
}

// CreateTableIndex creates a B-tree-family or other allowlisted-method index on named columns.
func (s *TableService) CreateTableIndex(ctx context.Context, projectID, userID uuid.UUID, schema, table string, req *model.CreateIndexRequest) error {
	if req == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalidTableRequest)
	}
	if err := ValidatePostgresSchemaName(schema); err != nil {
		return err
	}
	schema = PostgresSchema(schema)
	if err := validateRowColumnIdentifier(table); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}
	if err := validateRowColumnIdentifier(req.Name); err != nil {
		return fmt.Errorf("invalid index name: %w", err)
	}
	if len(req.Columns) == 0 {
		return fmt.Errorf("%w: at least one column is required", ErrInvalidTableRequest)
	}
	for _, col := range req.Columns {
		if err := validateRowColumnIdentifier(col); err != nil {
			return fmt.Errorf("invalid column name %q: %w", col, err)
		}
	}
	method := normalizeIndexMethod(req.Method)
	if _, ok := allowedIndexMethods[method]; !ok {
		return fmt.Errorf("%w: unsupported index method %q", ErrInvalidTableRequest, req.Method)
	}

	return withProjectPoolErr(s, ctx, userID, projectID, func(pool *pgxpool.Pool) error {
		err := s.tableRepo.CreateIndex(ctx, pool, schema, table, req.Name, req.Columns, req.Unique, method)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				switch pgErr.Code {
				case "42710", "42P07":
					return fmt.Errorf("%w: %v", ErrIndexAlreadyExists, err)
				}
			}
			return err
		}
		return nil
	})
}

// DropTableIndex drops a non-primary index that belongs to the table.
func (s *TableService) DropTableIndex(ctx context.Context, projectID, userID uuid.UUID, schema, table, indexName string) error {
	if err := ValidatePostgresSchemaName(schema); err != nil {
		return err
	}
	schema = PostgresSchema(schema)
	if err := validateRowColumnIdentifier(table); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}
	if err := validateRowColumnIdentifier(indexName); err != nil {
		return fmt.Errorf("invalid index name: %w", err)
	}

	return withProjectPoolErr(s, ctx, userID, projectID, func(pool *pgxpool.Pool) error {
		return s.tableRepo.DropIndex(ctx, pool, schema, table, indexName)
	})
}

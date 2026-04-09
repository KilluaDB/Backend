package repository

import (
	"backend/internal/postgres/model"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lib/pq"
)

var (
	// ErrIndexNotFound is returned when no index matches the table and name.
	ErrIndexNotFound = errors.New("index not found")
	// ErrCannotDropPrimaryIndex is returned when attempting to DROP a primary-key index.
	ErrCannotDropPrimaryIndex = errors.New("cannot drop primary key index")
)

// ListIndexes returns indexes defined on the given table (excluding internal-only rels).
func (r *TableRepository) ListIndexes(ctx context.Context, pool *pgxpool.Pool, schema, table string) ([]model.TableIndexInfo, error) {
	rows, err := pool.Query(ctx, `
		SELECT i.relname,
		       ix.indisunique,
		       ix.indisprimary,
		       am.amname,
		       pg_get_indexdef(i.oid, true, false),
		       ix.indisvalid
		FROM pg_class t
		JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = $1
		JOIN pg_index ix ON t.oid = ix.indrelid
		JOIN pg_class i ON i.oid = ix.indexrelid AND i.relkind = 'i'
		JOIN pg_am am ON i.relam = am.oid
		WHERE t.relname = $2 AND t.relkind IN ('r', 'p')
		ORDER BY i.relname
	`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("list indexes: %w", err)
	}
	defer rows.Close()

	var out []model.TableIndexInfo
	for rows.Next() {
		var info model.TableIndexInfo
		if err := rows.Scan(&info.Name, &info.Unique, &info.Primary, &info.Method, &info.Definition, &info.Valid); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

// CreateIndex runs CREATE [UNIQUE] INDEX with quoted identifiers only (method must be allowlisted by caller).
func (r *TableRepository) CreateIndex(ctx context.Context, pool *pgxpool.Pool, schema, table, indexName string, columns []string, unique bool, method string) error {
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		method = "btree"
	}

	schemaQ := pq.QuoteIdentifier(schema)
	tableQ := pq.QuoteIdentifier(table)
	idxQ := pq.QuoteIdentifier(indexName)
	colParts := make([]string, len(columns))
	for i, c := range columns {
		colParts[i] = pq.QuoteIdentifier(c)
	}

	var b strings.Builder
	if unique {
		b.WriteString("CREATE UNIQUE INDEX ")
	} else {
		b.WriteString("CREATE INDEX ")
	}
	b.WriteString(idxQ)
	b.WriteString(" ON ")
	b.WriteString(schemaQ)
	b.WriteString(".")
	b.WriteString(tableQ)
	if method != "btree" {
		b.WriteString(" USING ")
		b.WriteString(method)
	}
	b.WriteString(" (")
	b.WriteString(strings.Join(colParts, ", "))
	b.WriteString(")")

	_, err := pool.Exec(ctx, b.String())
	return err
}

// DropIndex drops an index by name if it belongs to the given table and is not the primary key index.
func (r *TableRepository) DropIndex(ctx context.Context, pool *pgxpool.Pool, schema, table, indexName string) error {
	var isPrimary bool
	err := pool.QueryRow(ctx, `
		SELECT ix.indisprimary
		FROM pg_class i
		JOIN pg_index ix ON i.oid = ix.indexrelid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = $1 AND t.relname = $2 AND i.relname = $3 AND i.relkind = 'i'
	`, schema, table, indexName).Scan(&isPrimary)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrIndexNotFound
		}
		return err
	}
	if isPrimary {
		return ErrCannotDropPrimaryIndex
	}

	q := fmt.Sprintf("DROP INDEX %s.%s",
		pq.QuoteIdentifier(schema), pq.QuoteIdentifier(indexName))
	_, err = pool.Exec(ctx, q)
	return err
}

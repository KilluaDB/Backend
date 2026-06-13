package repository

import (
	"backend/internal/postgres/model"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type SchemaRepository struct {
	pool poolQuerier
}

func NewSchemaRepository(pool poolQuerier) *SchemaRepository {
	return &SchemaRepository{pool: pool}
}

// ListSchemas returns user-visible schema names (excludes PostgreSQL system schemas).
func (r *SchemaRepository) ListSchemas(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema')
		  AND schema_name !~ '^pg_'
		ORDER BY schema_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// GetTables returns all table names in the specified schema
func (r *SchemaRepository) GetTables(ctx context.Context, schema string) ([]string, error) {
	query := `
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = $1 
		AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`

	rows, err := r.pool.Query(ctx, query, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tables, nil
}

// GetColumns returns all columns for a specific table in a schema
func (r *SchemaRepository) GetColumns(ctx context.Context, schema, table string) ([]model.Column, error) {
	query := `
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`

	rows, err := r.pool.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []model.Column
	for rows.Next() {
		var col model.Column
		var nullable string
		if err := rows.Scan(&col.Name, &col.DataType, &nullable); err != nil {
			return nil, err
		}
		col.Nullable = nullable == "YES"
		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return columns, nil
}

// TableExists returns true if a base table exists in the schema.
func (r *SchemaRepository) TableExists(ctx context.Context, schema, table string) (bool, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT 1
		FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = $2 AND table_type = 'BASE TABLE'
		LIMIT 1
	`, schema, table).Scan(&n)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// GetColumnDetails returns column metadata used for ALTER TABLE sync.
func (r *SchemaRepository) GetColumnDetails(ctx context.Context, schema, table string) ([]model.ColumnDetail, error) {
	query := `
		SELECT column_name, data_type, udt_name, character_maximum_length, is_nullable, column_default,
		       COALESCE(is_identity, 'NO') = 'YES'
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`
	rows, err := r.pool.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.ColumnDetail
	for rows.Next() {
		var d model.ColumnDetail
		var nullable string
		var def sql.NullString
		var charMax sql.NullInt32
		if err := rows.Scan(&d.Name, &d.DataType, &d.UdtName, &charMax, &nullable, &def, &d.IsIdentity); err != nil {
			return nil, err
		}
		d.IsNullable = nullable == "YES"
		if charMax.Valid {
			n := int(charMax.Int32)
			d.CharMaxLength = &n
		}
		if def.Valid {
			s := def.String
			d.ColumnDefault = &s
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetPrimaryKeys returns all primary key column names for a specific table
func (r *SchemaRepository) GetPrimaryKeys(ctx context.Context, schema, table string) ([]string, error) {
	query := `
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu 
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'PRIMARY KEY'
			AND tc.table_schema = $1
			AND tc.table_name = $2
		ORDER BY kcu.ordinal_position
	`

	rows, err := r.pool.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pks []string
	for rows.Next() {
		var pk string
		if err := rows.Scan(&pk); err != nil {
			return nil, err
		}
		pks = append(pks, pk)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return pks, nil
}

// GetForeignKeys returns all foreign keys for a specific table (one row per FK column; composite keys yield multiple rows).
func (r *SchemaRepository) GetForeignKeys(ctx context.Context, schema, table string) ([]model.ForeignKey, error) {
	query := `
		SELECT 
			tc.constraint_name,
			kcu.column_name,
			ccu.table_schema AS foreign_table_schema,
			ccu.table_name AS foreign_table_name,
			ccu.column_name AS foreign_column_name,
			rc.update_rule,
			rc.delete_rule
		FROM information_schema.table_constraints AS tc
		JOIN information_schema.key_column_usage AS kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage AS ccu
			ON ccu.constraint_name = tc.constraint_name
			AND ccu.table_schema = tc.table_schema
		JOIN information_schema.referential_constraints AS rc
			ON rc.constraint_schema = tc.table_schema
			AND rc.constraint_name = tc.constraint_name
		WHERE tc.constraint_type = 'FOREIGN KEY'
			AND tc.table_schema = $1
			AND tc.table_name = $2
	`

	rows, err := r.pool.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fks []model.ForeignKey
	for rows.Next() {
		var fk model.ForeignKey
		if err := rows.Scan(&fk.ConstraintName, &fk.FromColumn, &fk.ToSchema, &fk.ToTable, &fk.ToColumn, &fk.UpdateRule, &fk.DeleteRule); err != nil {
			return nil, err
		}
		fks = append(fks, fk)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return fks, nil
}

// TableColumn represents a table and column pair
type TableColumn struct {
	Table  string
	Column string
}

// GetUniqueConstraintsBatch returns a map of table:column pairs that have unique constraints
func (r *SchemaRepository) GetUniqueConstraintsBatch(ctx context.Context, schema string, tableColumns []TableColumn) (map[string]bool, error) {
	if len(tableColumns) == 0 {
		return make(map[string]bool), nil
	}

	// Build query with multiple conditions
	var conditions []string
	var args []interface{}
	argNum := 1

	for _, tc := range tableColumns {
		conditions = append(conditions, fmt.Sprintf("(tc.table_schema = $%d AND tc.table_name = $%d AND kcu.column_name = $%d)",
			argNum, argNum+1, argNum+2))
		args = append(args, schema, tc.Table, tc.Column)
		argNum += 3
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT tc.table_name, kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu 
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'UNIQUE' 
			AND tc.table_schema = $%d
			AND (%s)
	`, argNum, strings.Join(conditions, " OR "))
	args = append(args, schema)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query unique constraints: %w", err)
	}
	defer rows.Close()

	uniqueMap := make(map[string]bool)
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return nil, fmt.Errorf("failed to scan unique constraint: %w", err)
		}
		// Use table:column as key
		uniqueMap[fmt.Sprintf("%s:%s", table, column)] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating unique constraints: %w", err)
	}

	return uniqueMap, nil
}

package repositories

import (
	"context"
	"fmt"
	"my_project/internal/models"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SchemaRepository struct {
	pool *pgxpool.Pool
}

func NewSchemaRepository(pool *pgxpool.Pool) *SchemaRepository {
	return &SchemaRepository{pool: pool}
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
func (r *SchemaRepository) GetColumns(ctx context.Context, schema, table string) ([]models.Column, error) {
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

	var columns []models.Column
	for rows.Next() {
		var col models.Column
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

// GetForeignKeys returns all foreign keys for a specific table
func (r *SchemaRepository) GetForeignKeys(ctx context.Context, schema, table string) ([]models.ForeignKey, error) {
	query := `
		SELECT 
			tc.constraint_name,
			kcu.column_name,
			ccu.table_name AS foreign_table_name,
			ccu.column_name AS foreign_column_name
		FROM information_schema.table_constraints AS tc
		JOIN information_schema.key_column_usage AS kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage AS ccu
			ON ccu.constraint_name = tc.constraint_name
			AND ccu.table_schema = tc.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
			AND tc.table_schema = $1
			AND tc.table_name = $2
	`

	rows, err := r.pool.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fks []models.ForeignKey
	for rows.Next() {
		var fk models.ForeignKey
		if err := rows.Scan(&fk.ConstraintName, &fk.FromColumn, &fk.ToTable, &fk.ToColumn); err != nil {
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


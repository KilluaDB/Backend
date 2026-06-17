package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

// MaxGetRowsLimit caps SELECT from the table rows API (unbounded scans would risk OOM).
const MaxGetRowsLimit = 1000

func sortedMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// buildEqWhere builds `col = $n AND ...` (no WHERE keyword) with stable column order.
func buildEqWhere(filter map[string]interface{}, startParam int) (clause string, args []interface{}, next int) {
	if len(filter) == 0 {
		return "", nil, startParam
	}
	keys := sortedMapKeys(filter)
	parts := make([]string, 0, len(keys))
	args = make([]interface{}, 0, len(keys))
	p := startParam
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s = $%d", pq.QuoteIdentifier(k), p))
		args = append(args, filter[k])
		p++
	}
	return strings.Join(parts, " AND "), args, p
}

func scanRowsToMaps(rows pgx.Rows) ([]map[string]interface{}, error) {
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		columns[i] = string(fd.Name)
	}

	var out []map[string]interface{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
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
		out = append(out, rowMap)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func qualifiedTableIdent(schema, table string) string {
	return fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(table))
}

// SelectRows selects up to limit rows with optional equality filter.
// It requests one extra row to compute hasMore; the returned slice is trimmed to limit.
func (r *TableRepository) SelectRows(ctx context.Context, pool poolQuerier, schema, table string, filter map[string]interface{}, limit, offset int) ([]map[string]interface{}, bool, error) {
	if limit < 1 || limit > MaxGetRowsLimit {
		return nil, false, fmt.Errorf("limit must be between 1 and %d", MaxGetRowsLimit)
	}
	if offset < 0 {
		return nil, false, errors.New("offset must be non-negative")
	}

	fetchLimit := limit + 1
	q := fmt.Sprintf("SELECT * FROM %s", qualifiedTableIdent(schema, table))
	whereClause, whereArgs, next := buildEqWhere(filter, 1)
	args := whereArgs
	if whereClause != "" {
		q += " WHERE " + whereClause
	}
	q += fmt.Sprintf(" LIMIT $%d OFFSET $%d", next, next+1)
	args = append(args, fetchLimit, offset)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, false, fmt.Errorf("failed to query rows: %w", err)
	}
	maps, err := scanRowsToMaps(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(maps) > limit
	if hasMore {
		maps = maps[:limit]
	}
	return maps, hasMore, nil
}

// CountRows returns the number of rows matching the optional equality filter.
func (r *TableRepository) CountRows(ctx context.Context, pool poolQuerier, schema, table string, filter map[string]interface{}) (int64, error) {
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s", qualifiedTableIdent(schema, table))
	whereClause, whereArgs, _ := buildEqWhere(filter, 1)
	if whereClause != "" {
		q += " WHERE " + whereClause
	}
	var n int64
	err := pool.QueryRow(ctx, q, whereArgs...).Scan(&n)
	return n, err
}

// UpdateRows runs UPDATE with equality filter; nil/empty filter updates all rows.
func (r *TableRepository) UpdateRows(ctx context.Context, pool poolQuerier, schema, table string, filter, update map[string]interface{}) error {
	setKeys := sortedMapKeys(update)
	setParts := make([]string, 0, len(setKeys))
	args := make([]interface{}, 0, len(update)+len(filter))
	p := 1
	for _, k := range setKeys {
		setParts = append(setParts, fmt.Sprintf("%s = $%d", pq.QuoteIdentifier(k), p))
		args = append(args, update[k])
		p++
	}
	query := fmt.Sprintf("UPDATE %s SET %s", qualifiedTableIdent(schema, table), strings.Join(setParts, ", "))
	if len(filter) > 0 {
		whereClause, whereArgs, _ := buildEqWhere(filter, p)
		args = append(args, whereArgs...)
		query += " WHERE " + whereClause
	}
	if _, err := pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to update rows: %w", err)
	}
	return nil
}

// DeleteRowsByFilter deletes rows matching an optional equality filter; empty filter deletes all rows.
func (r *TableRepository) DeleteRowsByFilter(ctx context.Context, pool poolQuerier, schema, table string, filter map[string]interface{}) error {
	query := fmt.Sprintf("DELETE FROM %s", qualifiedTableIdent(schema, table))
	var args []interface{}
	whereClause, whereArgs, _ := buildEqWhere(filter, 1)
	if whereClause != "" {
		query += " WHERE " + whereClause
		args = whereArgs
	}
	if _, err := pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to delete rows: %w", err)
	}
	return nil
}

// InsertRow inserts a row; returned rowID is string (UUID), int64 (serial), or int64(0) when no RETURNING id.
func (r *TableRepository) InsertRow(ctx context.Context, pool poolQuerier, schema, table string, values map[string]interface{}) (rowID interface{}, err error) {
	var pkColumn string
	var pkDataType string
	errPK := pool.QueryRow(ctx, `
		SELECT a.attname, format_type(a.atttypid, a.atttypmod)
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE i.indisprimary
		AND n.nspname = $1
		AND LOWER(c.relname) = LOWER($2)
		LIMIT 1
	`, schema, table).Scan(&pkColumn, &pkDataType)
	hasPK := errPK == nil && pkColumn != ""

	colOrder := sortedMapKeys(values)
	columns := make([]string, 0, len(values))
	placeholders := make([]string, 0, len(values))
	vals := make([]interface{}, 0, len(values))
	paramIndex := 1
	for _, col := range colOrder {
		v := values[col]
		columns = append(columns, pq.QuoteIdentifier(col))
		placeholders = append(placeholders, fmt.Sprintf("$%d", paramIndex))
		vals = append(vals, v)
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

	tableName := qualifiedTableIdent(schema, table)

	if hasPK {
		queryWithReturning := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING %s",
			tableName, columnsStr, placeholdersStr, pq.QuoteIdentifier(pkColumn))
		switch pkDataType {
		case "uuid":
			queryUUIDReturning := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING %s::text",
				tableName, columnsStr, placeholdersStr, pq.QuoteIdentifier(pkColumn))
			var rowIDStr string
			err = pool.QueryRow(ctx, queryUUIDReturning, vals...).Scan(&rowIDStr)
			if err == nil {
				return rowIDStr, nil
			}
		default:
			var rid int64
			err = pool.QueryRow(ctx, queryWithReturning, vals...).Scan(&rid)
			if err == nil {
				return rid, nil
			}
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42703" {
			// fall through to insert without RETURNING
		} else {
			return nil, fmt.Errorf("failed to insert row into table %s: %w", table, err)
		}
	}

	queryWithoutReturning := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName, columnsStr, placeholdersStr)
	cmdTag, execErr := pool.Exec(ctx, queryWithoutReturning, vals...)
	if execErr != nil {
		return nil, fmt.Errorf("failed to insert row into table %s: %w", table, execErr)
	}
	if cmdTag.RowsAffected() == 0 {
		return nil, errors.New("no rows were inserted")
	}
	return int64(0), nil
}

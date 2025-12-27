package repositories

import (
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TableRepository struct {
	pool *pgxpool.Pool
}

func NewTableRepository(pool *pgxpool.Pool) *TableRepository {
	return &TableRepository {
		pool: pool,
	}
}

func (r *TableRepository) Delete(tx *sql.Tx, schema string, table string) (sql.Result, error) {
	// Use quoted identifiers to prevent SQL injection
	query := fmt.Sprintf("DROP TABLE \"%s\".\"%s\" CASCADE", schema, table)

	result, err := tx.Exec(query)
	if err != nil {
		return nil, fmt.Errorf("failed to drop table: %w", err)
	}
	
	return result, nil
}

// func (r *TableRepository) UpdateTableName(userDb *sql.DB, schema string, oldTable string, newtable string) (sql.Result, error) {
// 	query := fmt.Sprintf("ALTER TABLE %s.%s RENAME TO %s", schema, oldTable, newtable)

// 	result, err := userDb.Exec(query)
	
// 	return result, err
// }
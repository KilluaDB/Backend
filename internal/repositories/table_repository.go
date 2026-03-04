package repositories

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type TableRepository struct{}

func NewTableRepository() *TableRepository {
	return &TableRepository{}
}

func (r *TableRepository) Delete(ctx context.Context, tx pgx.Tx, schema string, table string) (pgconn.CommandTag, error) {
	query := fmt.Sprintf("DROP TABLE \"%s\".\"%s\" CASCADE", schema, table)
	return tx.Exec(ctx, query)
}

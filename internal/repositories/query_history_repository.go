package repositories

import (
	"backend/internal/models"
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type QueryHistoryRepository struct {
	pool *pgxpool.Pool
}

func NewQueryHistoryRepository(pool *pgxpool.Pool) *QueryHistoryRepository {
	return &QueryHistoryRepository{pool: pool}
}

func (r *QueryHistoryRepository) Create(queryHistory *models.QueryHistory) error {
	ctx := context.Background()

	queryHistory.Prepare()

	query := `
		INSERT INTO query_history (id, db_instance_id, user_id, query_text, executed_at, success, execution_time_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.pool.Exec(ctx, query,
		queryHistory.ID,
		queryHistory.DBInstanceID,
		queryHistory.UserID,
		queryHistory.QueryText,
		queryHistory.ExecutedAt,
		queryHistory.Success,
		queryHistory.ExecutionTimeMs,
	)

	return err
}

func (r *QueryHistoryRepository) GetByUserID(userID uuid.UUID, limit int) ([]models.QueryHistory, error) {
	ctx := context.Background()

	if limit <= 0 {
		limit = 100 // Default limit
	}

	query := `
		SELECT id, db_instance_id, user_id, query_text, executed_at, success, execution_time_ms
		FROM query_history WHERE user_id = $1
		ORDER BY executed_at DESC
		LIMIT $2
	`

	rows, err := r.pool.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []models.QueryHistory
	for rows.Next() {
		var qh models.QueryHistory
		err := rows.Scan(
			&qh.ID,
			&qh.DBInstanceID,
			&qh.UserID,
			&qh.QueryText,
			&qh.ExecutedAt,
			&qh.Success,
			&qh.ExecutionTimeMs,
		)
		if err != nil {
			return nil, err
		}
		queries = append(queries, qh)
	}

	return queries, rows.Err()
}

package repository

import (
	"backend/internal/model"
	"context"
	"errors"
	"time"

	"backend/internal/metrics"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ProjectRepository stores and reads project metadata.
type ProjectRepository struct {
	pool pgxPool
}

func NewProjectRepository(pool pgxPool) *ProjectRepository {
	return &ProjectRepository{pool: pool}
}

func (r *ProjectRepository) Create(ctx context.Context, project *model.Project) error {
	defer func(start time.Time) {
		metrics.MetaDbQueryDuration.WithLabelValues("project_create").Observe(time.Since(start).Seconds())
	}(time.Now())

	if ctx == nil {
		ctx = context.Background()
	}

	project.Prepare()

	query := `
		INSERT INTO projects (id, user_id, name, description, db_type, resource_tier, created_at, status, runtime_created_at, runtime_updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	now := time.Now()
	_, err := r.pool.Exec(ctx, query,
		project.ID,
		project.UserID,
		project.Name,
		project.Description,
		project.DBType,
		project.ResourceTier,
		now,
		project.Status,
		now,
		now,
	)

	return err
}

func (r *ProjectRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Project, error) {
	defer func(start time.Time) {
		metrics.MetaDbQueryDuration.WithLabelValues("project_get_by_id").Observe(time.Since(start).Seconds())
	}(time.Now())

	if ctx == nil {
		ctx = context.Background()
	}

	query := `
		SELECT id, user_id, name, description, db_type, resource_tier, created_at, status, runtime_created_at, runtime_updated_at
		FROM projects WHERE id = $1
	`

	var project model.Project
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&project.ID,
		&project.UserID,
		&project.Name,
		&project.Description,
		&project.DBType,
		&project.ResourceTier,
		&project.CreatedAt,
		&project.Status,
		&project.RuntimeCreatedAt,
		&project.RuntimeUpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &project, nil
}

func (r *ProjectRepository) GetByIDAndUserID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*model.Project, error) {
	defer func(start time.Time) {
		metrics.MetaDbQueryDuration.WithLabelValues("project_get_by_id_and_user_id").Observe(time.Since(start).Seconds())
	}(time.Now())

	if ctx == nil {
		ctx = context.Background()
	}

	query := `
		SELECT id, user_id, name, description, db_type, resource_tier, created_at, status, runtime_created_at, runtime_updated_at
		FROM projects WHERE id = $1 AND user_id = $2
	`

	var project model.Project
	err := r.pool.QueryRow(ctx, query, id, userID).Scan(
		&project.ID,
		&project.UserID,
		&project.Name,
		&project.Description,
		&project.DBType,
		&project.ResourceTier,
		&project.CreatedAt,
		&project.Status,
		&project.RuntimeCreatedAt,
		&project.RuntimeUpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &project, nil
}

func (r *ProjectRepository) GetByUserID(ctx context.Context, userID uuid.UUID) ([]model.Project, error) {
	defer func(start time.Time) {
		metrics.MetaDbQueryDuration.WithLabelValues("project_get_by_user_id").Observe(time.Since(start).Seconds())
	}(time.Now())

	if ctx == nil {
		ctx = context.Background()
	}

	query := `
		SELECT id, user_id, name, description, db_type, resource_tier, created_at, status, runtime_created_at, runtime_updated_at
		FROM projects WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []model.Project
	for rows.Next() {
		var project model.Project
		err := rows.Scan(
			&project.ID,
			&project.UserID,
			&project.Name,
			&project.Description,
			&project.DBType,
			&project.ResourceTier,
			&project.CreatedAt,
			&project.Status,
			&project.RuntimeCreatedAt,
			&project.RuntimeUpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}

	return projects, rows.Err()
}

func (r *ProjectRepository) UpdateRuntimeStatus(ctx context.Context, id uuid.UUID, status string) error {
	defer func(start time.Time) {
		metrics.MetaDbQueryDuration.WithLabelValues("project_update_runtime_status").Observe(time.Since(start).Seconds())
	}(time.Now())

	if ctx == nil {
		ctx = context.Background()
	}

	query := `
		UPDATE projects
		SET status = $2, runtime_updated_at = $3
		WHERE id = $1
	`

	_, err := r.pool.Exec(ctx, query, id, status, time.Now())
	return err
}

func (r *ProjectRepository) Update(ctx context.Context, project *model.Project) error {
	defer func(start time.Time) {
		metrics.MetaDbQueryDuration.WithLabelValues("project_update").Observe(time.Since(start).Seconds())
	}(time.Now())

	if ctx == nil {
		ctx = context.Background()
	}

	query := `
		UPDATE projects SET
			name = $2, description = $3, db_type = $4, resource_tier = $5
		WHERE id = $1
	`

	_, err := r.pool.Exec(ctx, query,
		project.ID,
		project.Name,
		project.Description,
		project.DBType,
		project.ResourceTier,
	)

	return err
}

func (r *ProjectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	defer func(start time.Time) {
		metrics.MetaDbQueryDuration.WithLabelValues("project_delete").Observe(time.Since(start).Seconds())
	}(time.Now())

	if ctx == nil {
		ctx = context.Background()
	}

	query := `DELETE FROM projects WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

func (r *ProjectRepository) DeleteByIDAndUserID(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	defer func(start time.Time) {
		metrics.MetaDbQueryDuration.WithLabelValues("project_delete_by_id_and_user_id").Observe(time.Since(start).Seconds())
	}(time.Now())

	if ctx == nil {
		ctx = context.Background()
	}

	query := `DELETE FROM projects WHERE id = $1 AND user_id = $2`
	result, err := r.pool.Exec(ctx, query, id, userID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return errors.New("project not found or access denied")
	}

	return nil
}

func (r *ProjectRepository) DeleteByUserIDTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	defer func(start time.Time) {
		metrics.MetaDbQueryDuration.WithLabelValues("project_delete_by_user_id_tx").Observe(time.Since(start).Seconds())
	}(time.Now())

	if ctx == nil {
		ctx = context.Background()
	}
	if tx == nil {
		return errors.New("transaction is required")
	}

	query := `DELETE FROM projects WHERE user_id = $1`
	_, err := tx.Exec(ctx, query, userID)
	return err
}

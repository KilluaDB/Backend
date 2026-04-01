package repository

import (
	"backend/internal/models"
	"backend/internal/repositories"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresProjectRepository is the PostgreSQL implementation of ProjectRepository.
type PostgresProjectRepository struct {
	pool *pgxpool.Pool
}

var _ repositories.ProjectRepository = (*PostgresProjectRepository)(nil)

func NewProjectRepository(pool *pgxpool.Pool) *PostgresProjectRepository {
	return &PostgresProjectRepository{pool: pool}
}

func (r *PostgresProjectRepository) Create(ctx context.Context, project *models.Project) error {
	if ctx == nil {
		ctx = context.Background()
	}

	project.Prepare()

	query := `
		INSERT INTO projects (id, user_id, name, description, db_type, resource_tier, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
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
	)

	return err
}

func (r *PostgresProjectRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	query := `
		SELECT id, user_id, name, description, db_type, resource_tier, created_at
		FROM projects WHERE id = $1
	`

	var project models.Project
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&project.ID,
		&project.UserID,
		&project.Name,
		&project.Description,
		&project.DBType,
		&project.ResourceTier,
		&project.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &project, nil
}

func (r *PostgresProjectRepository) GetByIDAndUserID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	query := `
		SELECT id, user_id, name, description, db_type, resource_tier, created_at
		FROM projects WHERE id = $1 AND user_id = $2
	`

	var project models.Project
	err := r.pool.QueryRow(ctx, query, id, userID).Scan(
		&project.ID,
		&project.UserID,
		&project.Name,
		&project.Description,
		&project.DBType,
		&project.ResourceTier,
		&project.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &project, nil
}

func (r *PostgresProjectRepository) GetByUserID(ctx context.Context, userID uuid.UUID) ([]models.Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	query := `
		SELECT id, user_id, name, description, db_type, resource_tier, created_at
		FROM projects WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var project models.Project
		err := rows.Scan(
			&project.ID,
			&project.UserID,
			&project.Name,
			&project.Description,
			&project.DBType,
			&project.ResourceTier,
			&project.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}

	return projects, rows.Err()
}

func (r *PostgresProjectRepository) Update(ctx context.Context, project *models.Project) error {
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

func (r *PostgresProjectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	query := `DELETE FROM projects WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

func (r *PostgresProjectRepository) DeleteByIDAndUserID(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	query := `DELETE FROM projects WHERE id = $1 AND user_id = $2`
	result, err := r.pool.Exec(ctx, query, id, userID)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	if result.RowsAffected() == 0 {
		return errors.New("project not found or access denied")
	}

	return nil
}

package repositories

import (
	"backend/internal/models"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DatabaseInstanceRepository struct {
	pool *pgxpool.Pool
}

func NewDatabaseInstanceRepository(pool *pgxpool.Pool) *DatabaseInstanceRepository {
	return &DatabaseInstanceRepository{pool: pool}
}

func (r *DatabaseInstanceRepository) Create(instance *models.DatabaseInstance) error {
	ctx := context.Background()

	instance.Prepare()

	query := `
		INSERT INTO database_instances (id, project_id, cpu_cores, ram_mb, storage_gb, status, port, host, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	now := time.Now()
	_, err := r.pool.Exec(ctx, query,
		instance.ID,
		instance.ProjectID,
		instance.CPUCores,
		instance.RAMMB,
		instance.StorageGB,
		instance.Status,
		instance.Port,
		instance.Host,
		now,
		now,
	)

	return err
}

func (r *DatabaseInstanceRepository) GetByID(id uuid.UUID) (*models.DatabaseInstance, error) {
	ctx := context.Background()

	query := `
		SELECT id, project_id, cpu_cores, ram_mb, storage_gb, status, port, host, created_at, updated_at
		FROM database_instances WHERE id = $1
	`

	var instance models.DatabaseInstance
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&instance.ID,
		&instance.ProjectID,
		&instance.CPUCores,
		&instance.RAMMB,
		&instance.StorageGB,
		&instance.Status,
		&instance.Port,
		&instance.Host,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &instance, nil
}

func (r *DatabaseInstanceRepository) GetByProjectID(projectID uuid.UUID) (*models.DatabaseInstance, error) {
	ctx := context.Background()

	query := `
		SELECT id, project_id, cpu_cores, ram_mb, storage_gb, status, port, host, created_at, updated_at
		FROM database_instances WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`

	var instance models.DatabaseInstance
	err := r.pool.QueryRow(ctx, query, projectID).Scan(
		&instance.ID,
		&instance.ProjectID,
		&instance.CPUCores,
		&instance.RAMMB,
		&instance.StorageGB,
		&instance.Status,
		&instance.Port,
		&instance.Host,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &instance, nil
}

func (r *DatabaseInstanceRepository) UpdateStatus(id uuid.UUID, status string) error {
	ctx := context.Background()

	query := `
		UPDATE database_instances 
		SET status = $2, updated_at = $3
		WHERE id = $1
	`

	_, err := r.pool.Exec(ctx, query, id, status, time.Now())
	return err
}

// UpdateHost sets the host for the database instance (e.g. after K8s provisioning).
func (r *DatabaseInstanceRepository) UpdateHost(id uuid.UUID, host string) error {
	ctx := context.Background()

	query := `
		UPDATE database_instances 
		SET host = $2, updated_at = $3
		WHERE id = $1
	`

	_, err := r.pool.Exec(ctx, query, id, host, time.Now())
	return err
}

// UpdateAfterProvision sets host, port, and status to running after successful provisioning.
// K8s resource is discovered by project_id (ResourceRefForProject) when needed.
func (r *DatabaseInstanceRepository) UpdateAfterProvision(id uuid.UUID, host string, port int) error {
	ctx := context.Background()

	query := `
		UPDATE database_instances 
		SET host = $2, port = $3, status = 'running', updated_at = $4
		WHERE id = $1
	`

	_, err := r.pool.Exec(ctx, query, id, host, port, time.Now())
	return err
}

// UpdateConnectionInfo sets host, port, and status to running (e.g. after healing from operator).
func (r *DatabaseInstanceRepository) UpdateConnectionInfo(id uuid.UUID, host string, port int) error {
	ctx := context.Background()

	query := `
		UPDATE database_instances 
		SET host = $2, port = $3, status = 'running', updated_at = $4
		WHERE id = $1
	`

	_, err := r.pool.Exec(ctx, query, id, host, port, time.Now())
	return err
}

func (r *DatabaseInstanceRepository) UpdateResources(id uuid.UUID, cpuCores int, ramMB int, storageGB int) error {
	ctx := context.Background()

	query := `
		UPDATE database_instances 
		SET cpu_cores = $2, ram_mb = $3, storage_gb = $4, updated_at = $5
		WHERE id = $1
	`

	_, err := r.pool.Exec(ctx, query, id, cpuCores, ramMB, storageGB, time.Now())
	return err
}

func (r *DatabaseInstanceRepository) GetRunningByProjectID(projectID uuid.UUID) (*models.DatabaseInstance, error) {
	ctx := context.Background()

	query := `
		SELECT id, project_id, cpu_cores, ram_mb, storage_gb, status, port, host, created_at, updated_at
		FROM database_instances WHERE project_id = $1 AND status = 'running'
		ORDER BY created_at DESC
		LIMIT 1
	`

	var instance models.DatabaseInstance
	err := r.pool.QueryRow(ctx, query, projectID).Scan(
		&instance.ID,
		&instance.ProjectID,
		&instance.CPUCores,
		&instance.RAMMB,
		&instance.StorageGB,
		&instance.Status,
		&instance.Port,
		&instance.Host,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &instance, nil
}

// GetConnectableByProjectID returns an instance that can be used to connect.
// Requires host and port. K8s resource is discovered by project_id when needed.
func (r *DatabaseInstanceRepository) GetConnectableByProjectID(projectID uuid.UUID) (*models.DatabaseInstance, error) {
	ctx := context.Background()

	query := `
		SELECT id, project_id, cpu_cores, ram_mb, storage_gb, status, port, host, created_at, updated_at
		FROM database_instances
		WHERE project_id = $1
		  AND host IS NOT NULL AND host != ''
		  AND port IS NOT NULL
		ORDER BY CASE WHEN status = 'running' THEN 0 ELSE 1 END, created_at DESC
		LIMIT 1
	`

	var instance models.DatabaseInstance
	err := r.pool.QueryRow(ctx, query, projectID).Scan(
		&instance.ID,
		&instance.ProjectID,
		&instance.CPUCores,
		&instance.RAMMB,
		&instance.StorageGB,
		&instance.Status,
		&instance.Port,
		&instance.Host,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &instance, nil
}

func (r *DatabaseInstanceRepository) Delete(id uuid.UUID) error {
	ctx := context.Background()

	query := `DELETE FROM database_instances WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

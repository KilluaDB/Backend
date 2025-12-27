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

type DatabaseCredentialRepository struct {
	pool *pgxpool.Pool
}

func NewDatabaseCredentialRepository(pool *pgxpool.Pool) *DatabaseCredentialRepository {
	return &DatabaseCredentialRepository{pool: pool}
}

func (r *DatabaseCredentialRepository) Create(credential *models.DatabaseCredential) error {
	ctx := context.Background()

	credential.Prepare()

	query := `
		INSERT INTO database_credentials (id, db_instance_id, username, password_encrypted, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	now := time.Now()
	_, err := r.pool.Exec(ctx, query,
		credential.ID,
		credential.DBInstanceID,
		credential.Username,
		credential.PasswordEncrypted,
		now,
	)

	return err
}

func (r *DatabaseCredentialRepository) GetByInstanceID(instanceID uuid.UUID) ([]models.DatabaseCredential, error) {
	ctx := context.Background()

	query := `
		SELECT id, db_instance_id, username, password_encrypted, created_at
		FROM database_credentials WHERE db_instance_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var credentials []models.DatabaseCredential
	for rows.Next() {
		var cred models.DatabaseCredential
		err := rows.Scan(
			&cred.ID,
			&cred.DBInstanceID,
			&cred.Username,
			&cred.PasswordEncrypted,
			&cred.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, cred)
	}

	return credentials, rows.Err()
}

func (r *DatabaseCredentialRepository) GetLatestByInstanceID(instanceID uuid.UUID) (*models.DatabaseCredential, error) {
	ctx := context.Background()

	query := `
		SELECT id, db_instance_id, username, password_encrypted, created_at
		FROM database_credentials WHERE db_instance_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`

	var cred models.DatabaseCredential
	err := r.pool.QueryRow(ctx, query, instanceID).Scan(
		&cred.ID,
		&cred.DBInstanceID,
		&cred.Username,
		&cred.PasswordEncrypted,
		&cred.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &cred, nil
}

func (r *DatabaseCredentialRepository) GetByID(id uuid.UUID) (*models.DatabaseCredential, error) {
	ctx := context.Background()

	query := `
		SELECT id, db_instance_id, username, password_encrypted, created_at
		FROM database_credentials WHERE id = $1
	`

	var cred models.DatabaseCredential
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&cred.ID,
		&cred.DBInstanceID,
		&cred.Username,
		&cred.PasswordEncrypted,
		&cred.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &cred, nil
}

func (r *DatabaseCredentialRepository) Delete(id uuid.UUID) error {
	ctx := context.Background()

	query := `DELETE FROM database_credentials WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

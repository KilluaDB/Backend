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

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) Create(user *models.User) error {
	ctx := context.Background()

	user.Prepare()

	// Set default role if not provided
	if user.Role == "" {
		user.Role = "user"
	}

	// Set default status if not provided
	if user.Status == "" {
		user.Status = "active"
	}

	query := `
		INSERT INTO users (id, email, password_hash, role, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	now := time.Now()
	_, err := r.pool.Exec(ctx, query,
		user.ID,
		user.Email,
		user.PasswordHash,
		user.Role,
		user.Status,
		now,
	)

	return err
}

func (r *UserRepository) FindUserByID(id uuid.UUID) (*models.User, error) {
	ctx := context.Background()

	query := `SELECT id, email, password_hash, role, status, created_at, last_login_at, deleted_at
		FROM users WHERE id = $1 AND deleted_at IS NULL`

	var user models.User
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.Status,
		&user.CreatedAt,
		&user.LastLoginAt,
		&user.DeletedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}

func (r *UserRepository) FindUserByEmail(email string) (*models.User, error) {
	ctx := context.Background()

	query := `SELECT id, email, password_hash, role, status, created_at, last_login_at, deleted_at
		FROM users WHERE email = $1 AND deleted_at IS NULL`

	var user models.User
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.Status,
		&user.CreatedAt,
		&user.LastLoginAt,
		&user.DeletedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}

func (r *UserRepository) FindUserByName(username string) (*models.User, error) {
	// This method is not used but kept for compatibility
	// If you need it, you can implement it similar to FindUserByEmail
	return nil, errors.New("not implemented")
}

func (r *UserRepository) DeleteRefreshTokensByUserID(userID uuid.UUID) error {
	ctx := context.Background()

	query := `DELETE FROM sessions WHERE user_id = $1`
	_, err := r.pool.Exec(ctx, query, userID)
	return err
}

func (r *UserRepository) Update(user *models.User) error {
	ctx := context.Background()

	query := `
		UPDATE users 
		SET email = $2, role = $3, status = $4
		WHERE id = $1 AND deleted_at IS NULL
	`

	_, err := r.pool.Exec(ctx, query,
		user.ID,
		user.Email,
		user.Role,
		user.Status,
	)

	return err
}

func (r *UserRepository) Delete(id uuid.UUID) error {
	ctx := context.Background()

	// Soft delete: update deleted_at and status instead of hard delete
	query := `
		UPDATE users 
		SET deleted_at = NOW(), 
		    status = 'deleted'
		WHERE id = $1 AND deleted_at IS NULL
	`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

func (r *UserRepository) FindAll() ([]models.User, error) {
	ctx := context.Background()

	query := `SELECT id, email, password_hash, role, status, created_at, last_login_at, deleted_at
		FROM users
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.PasswordHash,
			&user.Role,
			&user.Status,
			&user.CreatedAt,
			&user.LastLoginAt,
			&user.DeletedAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// CountUsers returns the total number of active (non-deleted) users
func (r *UserRepository) CountUsers() (int, error) {
	ctx := context.Background()

	query := `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`

	var count int
	err := r.pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// CountAdmins returns the number of active users with admin role
func (r *UserRepository) CountAdmins() (int, error) {
	ctx := context.Background()

	query := `SELECT COUNT(*) FROM users WHERE role = 'admin' AND deleted_at IS NULL`

	var count int
	err := r.pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

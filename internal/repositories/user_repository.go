package repositories

import (
	"context"
	"errors"
	"my_project/internal/models"
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
	
	query := `
		INSERT INTO users (id, email, password_hash, created_at)
		VALUES ($1, $2, $3, $4)
	`
	
	now := time.Now()
	_, err := r.pool.Exec(ctx, query,
		user.ID,
		user.Email,
		user.PasswordHash,
		now,
	)
	
	return err
}

func (r *UserRepository) FindUserByID(id uuid.UUID) (*models.User, error) {
	ctx := context.Background()
	
	query := `SELECT id, email, password_hash, created_at, last_login_at
		FROM users WHERE id = $1`
	
	var user models.User
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.LastLoginAt,
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
	
	query := `SELECT id, email, password_hash, created_at, last_login_at
		FROM users WHERE email = $1`
	
	var user models.User
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.LastLoginAt,
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

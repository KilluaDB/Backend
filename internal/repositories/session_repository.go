package repositories

import (
	"backend/internal/models"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionRepository struct {
	pool *pgxpool.Pool
}

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

func (r *SessionRepository) Create(session *models.Session) error {
	ctx := context.Background()

	session.Prepare()

	query := `
		INSERT INTO sessions (id, user_id, refresh_token, is_revoked, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := r.pool.Exec(ctx, query,
		session.ID,
		session.UserID,
		session.RefreshToken,
		session.IsRevoked,
		time.Now(),
		session.ExpiresAt,
	)

	return err
}

func (r *SessionRepository) FindByToken(token string) (*models.Session, error) {
	ctx := context.Background()

	query := `SELECT id, user_id, refresh_token, is_revoked, created_at, expires_at
		FROM sessions WHERE refresh_token = $1`

	var session models.Session
	err := r.pool.QueryRow(ctx, query, token).Scan(
		&session.ID,
		&session.UserID,
		&session.RefreshToken,
		&session.IsRevoked,
		&session.CreatedAt,
		&session.ExpiresAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &session, nil
}

func (r *SessionRepository) Revoke(token string) error {
	ctx := context.Background()

	query := `UPDATE sessions SET is_revoked = true WHERE refresh_token = $1`
	_, err := r.pool.Exec(ctx, query, token)
	return err
}

func (r *SessionRepository) DeleteExpired() error {
	ctx := context.Background()

	query := `DELETE FROM sessions WHERE expires_at < $1`
	_, err := r.pool.Exec(ctx, query, time.Now())
	return err
}

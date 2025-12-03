package models

import (
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID           uuid.UUID `json:"id"`
	UserID       uuid.UUID `json:"user_id"`
	RefreshToken string    `json:"refresh_token"`
	IsRevoked    bool      `json:"is_revoked"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (s *Session) Prepare() {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
}

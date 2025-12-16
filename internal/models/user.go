package models

import (
	"html"
	"strings"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID  `json:"id"`
	Email        string     `json:"email"`
	Password     string     `json:"password,omitempty"` // For JSON input only, not stored in DB
	PasswordHash string     `json:"-"`                  // Don't expose password hash in JSON - stored in DB
	Role         string     `json:"role"`               // "user", "admin", "manager", etc.
	Status       string     `json:"status"`             // "active", "deleted"
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

func (u *User) Prepare() {
	u.Email = html.EscapeString(strings.TrimSpace(u.Email))
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
}

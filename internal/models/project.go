package models

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"user_id"`
	Name         string     `json:"name"`
	Description  *string    `json:"description,omitempty"`
	DBType       string     `json:"db_type"`        // 'postgresql' or 'mongodb'
	ResourceTier string     `json:"resource_tier"`  // 'free', 'basic', or 'premium'
	CreatedAt    time.Time  `json:"created_at"`
	Status       string     `json:"status,omitempty"` // instance status: creating, running, failed (set when loading for API)
}

func (p *Project) Prepare() {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.ResourceTier == "" {
		p.ResourceTier = "free"
	}
}
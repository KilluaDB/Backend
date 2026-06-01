package model

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID               uuid.UUID  `json:"id"`
	UserID           uuid.UUID  `json:"user_id"`
	Name             string     `json:"name"`
	Description      *string    `json:"description,omitempty"`
	DBType           string     `json:"db_type"`       // 'postgresql' or 'mongodb'
	ResourceTier     string     `json:"resource_tier"` // 'free', 'basic', or 'premium'
	CreatedAt        time.Time  `json:"created_at"`
	Status           string     `json:"status,omitempty"` // runtime status: creating, running, failed, paused, deleted
	RuntimeCreatedAt *time.Time `json:"runtime_created_at,omitempty"`
	RuntimeUpdatedAt *time.Time `json:"runtime_updated_at,omitempty"`
}

func (p *Project) Prepare() {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.ResourceTier == "" {
		p.ResourceTier = "free"
	}
	if p.Status == "" {
		p.Status = "creating"
	}
}

package models

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	DBType      string     `json:"db_type"` // 'postgres' or 'mongodb'
	CreatedAt   time.Time  `json:"created_at"`
}

func (p *Project) Prepare() {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
}
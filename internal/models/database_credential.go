package models

import (
	"time"

	"github.com/google/uuid"
)

type DatabaseCredential struct {
	ID              uuid.UUID `json:"id"`
	DBInstanceID    uuid.UUID `json:"db_instance_id"`
	Username        string    `json:"username"`
	PasswordEncrypted string  `json:"-"` // Don't expose encrypted password
	CreatedAt       time.Time `json:"created_at"`
}

func (d *DatabaseCredential) Prepare() {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
}


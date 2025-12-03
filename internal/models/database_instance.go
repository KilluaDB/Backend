package models

import (
	"time"

	"github.com/google/uuid"
)

type DatabaseInstance struct {
	ID          uuid.UUID `json:"id"`
	ProjectID   uuid.UUID `json:"project_id"`
	CPUCores    *int      `json:"cpu_cores,omitempty"`
	RAMMB       *int      `json:"ram_mb,omitempty"`
	StorageGB   *int      `json:"storage_gb,omitempty"`
	Status      string    `json:"status"` // 'creating', 'running', 'failed', 'paused', 'deleted'
	Endpoint    *string   `json:"endpoint,omitempty"`
	Port        *int      `json:"port,omitempty"`
	ContainerID *string   `json:"container_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (d *DatabaseInstance) Prepare() {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if d.Status == "" {
		d.Status = "creating"
	}
}


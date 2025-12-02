package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Project matches projects table from script.sql
type Project struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID      uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	Name        string    `gorm:"type:text;not null" json:"name"`
	Description string    `gorm:"type:text" json:"description,omitempty"`
	DBType      string    `gorm:"type:text;not null" json:"db_type"` // 'postgres' | 'mongodb'
	CreatedAt   time.Time `gorm:"type:timestamptz;autoCreateTime" json:"created_at"`
	User        User      `gorm:"foreignKey:UserID;references:ID" json:"user,omitempty"`
}

func (Project) TableName() string { 
	return "projects" 
}

func (p *Project) BeforeCreate(tx *gorm.DB) (err error) {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return
}

// DatabaseInstance matches database_instances table
type DatabaseInstance struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	ProjectID uuid.UUID `gorm:"type:uuid;not null;index" json:"project_id"`
	CPUCores  int       `json:"cpu_cores"`
	RAMMB     int       `json:"ram_mb"`
	StorageGB int       `json:"storage_gb"`
	Status    string    `gorm:"type:text;not null" json:"status"` // creating, running, failed, paused, deleted
	Endpoint  string    `gorm:"type:text" json:"endpoint"`
	Port      int       `json:"port"`
	CreatedAt time.Time `gorm:"type:timestamptz;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"type:timestamptz;autoUpdateTime" json:"updated_at"`
}

func (DatabaseInstance) TableName() string { 
	return "database_instances" 
}

func (d *DatabaseInstance) BeforeCreate(tx *gorm.DB) (err error) {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	return
}

// DatabaseCredential matches database_credentials table
type DatabaseCredential struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	DBInstanceID     uuid.UUID `gorm:"type:uuid;not null;index" json:"db_instance_id"`
	Username         string    `gorm:"type:text;not null" json:"username"`
	PasswordEncrypted string   `gorm:"type:text;not null" json:"password_encrypted"`
	CreatedAt        time.Time `gorm:"type:timestamptz;autoCreateTime" json:"created_at"`
}

func (DatabaseCredential) TableName() string { 
	return "database_credentials" 
}

func (c *DatabaseCredential) BeforeCreate(tx *gorm.DB) (err error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return
}

// QueryHistory maps to query_history table from script.sql
type QueryHistory struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	DBInstanceID    uuid.UUID `gorm:"type:uuid;not null;index" json:"db_instance_id"`
	UserID          uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	QueryText       string    `gorm:"type:text;not null" json:"query_text"`
	ExecutedAt      time.Time `gorm:"type:timestamptz;autoCreateTime" json:"executed_at"`
	Success         bool      `json:"success"`
	ExecutionTimeMs int       `json:"execution_time_ms"`
}

func (QueryHistory) TableName() string { return "query_history" }

func (q *QueryHistory) BeforeCreate(tx *gorm.DB) (err error) {
	if q.ID == uuid.Nil {
		q.ID = uuid.New()
	}
	return
}

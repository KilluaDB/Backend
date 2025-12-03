package models

import (
	"time"

	"github.com/google/uuid"
)

type QueryHistory struct {
	ID              uuid.UUID `json:"id"`
	DBInstanceID    uuid.UUID `json:"db_instance_id"`
	UserID          uuid.UUID `json:"user_id"`
	QueryText       string    `json:"query_text"`
	ExecutedAt      time.Time `json:"executed_at"`
	Success         *bool     `json:"success,omitempty"`
	ExecutionTimeMs *int      `json:"execution_time_ms,omitempty"`
}

func (q *QueryHistory) Prepare() {
	if q.ID == uuid.Nil {
		q.ID = uuid.New()
	}
	if q.ExecutedAt.IsZero() {
		q.ExecutedAt = time.Now()
	}
}


package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestQueryHistory_Prepare(t *testing.T) {
	// Empty record: ID is generated and ExecutedAt is populated.
	q := &QueryHistory{}
	q.Prepare()
	assert.NotEqual(t, uuid.Nil, q.ID)
	assert.False(t, q.ExecutedAt.IsZero())

	// Existing ID and ExecutedAt are preserved.
	id := uuid.New()
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	q2 := &QueryHistory{ID: id, ExecutedAt: ts}
	q2.Prepare()
	assert.Equal(t, id, q2.ID)
	assert.Equal(t, ts, q2.ExecutedAt)
}

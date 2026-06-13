package model

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestProject_Prepare(t *testing.T) {
	// Empty project: ID is generated and string defaults are applied.
	p := &Project{}
	p.Prepare()
	assert.NotEqual(t, uuid.Nil, p.ID)
	assert.Equal(t, "free", p.ResourceTier)
	assert.Equal(t, "creating", p.Status)

	// Existing ID and explicit fields are preserved.
	id := uuid.New()
	p2 := &Project{ID: id, ResourceTier: "premium", Status: "running"}
	p2.Prepare()
	assert.Equal(t, id, p2.ID)
	assert.Equal(t, "premium", p2.ResourceTier)
	assert.Equal(t, "running", p2.Status)
}

package handler

import (
	"testing"
	"time"

	"backend/internal/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDbTypeForAPI(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"postgresql", "sql"},
		{"postgres", "sql"},
		{"mongodb", "nosql"},
		{"other", "other"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, dbTypeForAPI(tt.in))
	}
}

func TestProjectToAPI(t *testing.T) {
	id := uuid.New()
	p := &model.Project{
		ID: id, Name: "n", DBType: "postgresql", ResourceTier: "free", Status: "running",
		CreatedAt: time.Now(),
	}
	api := projectToAPI(p)
	assert.Equal(t, "sql", api["db_type"])
	assert.Equal(t, id, api["id"])
}

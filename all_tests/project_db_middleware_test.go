package middleware

import (
	"net/http"
	"testing"

	"backend/internal/mocks"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestRequirePostgresProject(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	pg := projects.SeedProject(userID, "postgresql")
	mongo := projects.SeedProject(userID, "mongodb")

	r := gin.New()
	r.GET("/projects/:id/x", func(c *gin.Context) {
		c.Set(utils.UserIDContextKey, userID)
		RequirePostgresProject(projects)(c)
	}, func(c *gin.Context) { c.Status(http.StatusOK) })

	tests := []struct {
		name       string
		projectID  string
		wantStatus int
	}{
		{"postgres project", pg.ID.String(), http.StatusOK},
		{"mongo project", mongo.ID.String(), http.StatusBadRequest},
		{"unknown project", uuid.New().String(), http.StatusNotFound},
		{"invalid id", "not-uuid", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+tt.projectID+"/x", nil, nil)
			c.Params = gin.Params{{Key: "id", Value: tt.projectID}}
			r.HandleContext(c)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestCanonicalProjectDBType(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"postgres", "postgresql"},
		{"PostgreSQL", "postgresql"},
		{"mongodb", "mongodb"},
		{"nosql", "mongodb"},
		{"other", "other"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, canonicalProjectDBType(tt.in))
	}
}

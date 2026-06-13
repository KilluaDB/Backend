package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/mocks"
	"backend/internal/service"
	"backend/internal/service/servicetest"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProjectHandler(projects *mocks.ProjectStore, prov *servicetest.Provisioner) *ProjectHandler {
	return NewProjectHandler(service.NewProjectService(projects, prov, nil, nil))
}

func projectRouter(h *ProjectHandler, userID uuid.UUID) *gin.Engine {
	r := gin.New()
	withUser := func(c *gin.Context) {
		if userID != uuid.Nil {
			c.Set(utils.UserIDContextKey, userID)
		}
	}
	r.POST("/projects", withUser, h.CreateProject)
	r.GET("/projects", withUser, h.ListProjects)
	r.GET("/projects/:id", withUser, h.GetProject)
	r.GET("/projects/:id/access", withUser, h.GetProjectAccess)
	r.DELETE("/projects/:id", withUser, h.DeleteProject)
	return r
}

func TestProjectHandler_CreateProject(t *testing.T) {
	userID := uuid.New()
	projects := mocks.NewProjectStore()
	h := newProjectHandler(projects, &servicetest.Provisioner{})
	r := projectRouter(h, userID)

	tests := []struct {
		name       string
		body       map[string]string
		wantStatus int
	}{
		{"invalid body", map[string]string{}, http.StatusBadRequest},
		{"invalid password", map[string]string{"name": "p", "db_type": "postgres", "password": "short"}, http.StatusBadRequest},
		{"invalid db type", map[string]string{"name": "p", "db_type": "mysql", "password": "SecurePass123!"}, http.StatusBadRequest},
		{"invalid tier", map[string]string{"name": "p", "db_type": "postgres", "password": "SecurePass123!", "resource_tier": "enterprise"}, http.StatusBadRequest},
		{"success", map[string]string{"name": "my-db", "db_type": "postgres", "password": "SecurePass123!"}, http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := testutil.NewGinContext(http.MethodPost, "/projects", tt.body, nil)
			c.Set(utils.UserIDContextKey, userID)
			r.HandleContext(c)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestProjectHandler_GetProject(t *testing.T) {
	userID := uuid.New()
	projects := mocks.NewProjectStore()
	p := projects.SeedProject(userID, "postgresql")
	h := newProjectHandler(projects, &servicetest.Provisioner{})

	tests := []struct {
		name       string
		projectID  string
		routerUser uuid.UUID
		wantStatus int
	}{
		{"success", p.ID.String(), userID, http.StatusOK},
		{"not found", uuid.New().String(), userID, http.StatusNotFound},
		{"unauthorized", p.ID.String(), uuid.Nil, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := projectRouter(h, tt.routerUser)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/projects/"+tt.projectID, nil))
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestProjectHandler_ListProjects(t *testing.T) {
	userID := uuid.New()
	projects := mocks.NewProjectStore()
	projects.SeedProject(userID, "postgresql")
	projects.SeedProject(userID, "mongodb")
	h := newProjectHandler(projects, &servicetest.Provisioner{})
	r := projectRouter(h, userID)

	c, w := testutil.NewGinContext(http.MethodGet, "/projects", nil, nil)
	c.Set(utils.UserIDContextKey, userID)
	r.HandleContext(c)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Status string `json:"status"`
		Data   []any  `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "success", resp.Status)
	assert.Len(t, resp.Data, 2)
}

func TestProjectHandler_GetProjectAccess(t *testing.T) {
	userID := uuid.New()
	projects := mocks.NewProjectStore()
	p := projects.SeedProject(userID, "postgresql")
	p.Status = "running"
	_ = projects.Update(context.Background(), p)

	prov := &servicetest.Provisioner{
		External: true,
		GetConnFn: func(ctx context.Context, projectID uuid.UUID, dbType string) (*service.ProvisionResult, error) {
			return &service.ProvisionResult{DSN: "postgresql://app:secret@host:5432/app?sslmode=require"}, nil
		},
	}
	h := newProjectHandler(projects, prov)
	r := projectRouter(h, userID)

	t.Run("success", func(t *testing.T) {
		c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+p.ID.String()+"/access", nil, nil)
		c.Params = gin.Params{{Key: "id", Value: p.ID.String()}}
		c.Set(utils.UserIDContextKey, userID)
		r.HandleContext(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("external not configured", func(t *testing.T) {
		h2 := newProjectHandler(projects, &servicetest.Provisioner{External: false})
		r2 := projectRouter(h2, userID)
		c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+p.ID.String()+"/access", nil, nil)
		c.Params = gin.Params{{Key: "id", Value: p.ID.String()}}
		c.Set(utils.UserIDContextKey, userID)
		r2.HandleContext(c)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("not running", func(t *testing.T) {
		creating := projects.SeedProject(userID, "postgresql")
		creating.Status = "creating"
		_ = projects.Update(context.Background(), creating)
		c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+creating.ID.String()+"/access", nil, nil)
		c.Params = gin.Params{{Key: "id", Value: creating.ID.String()}}
		c.Set(utils.UserIDContextKey, userID)
		r.HandleContext(c)
		assert.Equal(t, http.StatusConflict, w.Code)
	})
}

func TestProjectHandler_DeleteProject(t *testing.T) {
	userID := uuid.New()
	projects := mocks.NewProjectStore()
	p := projects.SeedProject(userID, "postgresql")
	h := newProjectHandler(projects, &servicetest.Provisioner{})
	r := projectRouter(h, userID)

	c, w := testutil.NewGinContext(http.MethodDelete, "/projects/"+p.ID.String(), nil, nil)
	c.Params = gin.Params{{Key: "id", Value: p.ID.String()}}
	c.Set(utils.UserIDContextKey, userID)
	r.HandleContext(c)
	assert.Equal(t, http.StatusOK, w.Code)

	c2, w2 := testutil.NewGinContext(http.MethodDelete, "/projects/"+uuid.New().String(), nil, nil)
	c2.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c2.Set(utils.UserIDContextKey, userID)
	r.HandleContext(c2)
	assert.Equal(t, http.StatusNotFound, w2.Code)
}

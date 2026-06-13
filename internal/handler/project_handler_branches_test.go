package handler

import (
	"net/http"
	"testing"

	"backend/internal/mocks"
	"backend/internal/service"
	"backend/internal/service/servicetest"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestProjectHandler_CreateProject_unauthorized(t *testing.T) {
	h := newProjectHandler(mocks.NewProjectStore(), &servicetest.Provisioner{})
	r := projectRouter(h, uuid.Nil)

	c, w := testutil.NewGinContext(http.MethodPost, "/projects", map[string]string{
		"name": "p", "db_type": "postgres", "password": "SecurePass123!",
	}, nil)
	r.HandleContext(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestProjectHandler_DeleteProject_unauthorized(t *testing.T) {
	h := newProjectHandler(mocks.NewProjectStore(), &servicetest.Provisioner{})
	r := projectRouter(h, uuid.Nil)

	c, w := testutil.NewGinContext(http.MethodDelete, "/projects/"+uuid.New().String(), nil, nil)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	r.HandleContext(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// A non-sentinel error from the connection-info lookup maps to the default 500.
func TestProjectHandler_GetProjectAccess_serviceError(t *testing.T) {
	userID := uuid.New()
	projects := mocks.NewProjectStore()
	prov := &servicetest.Provisioner{External: true}
	svc := service.NewProjectService(&projectStoreFailingGetByIDAndUserID{ProjectStore: projects}, prov, nil, nil)
	h := NewProjectHandler(svc)
	r := projectRouter(h, userID)

	id := uuid.New().String()
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+id+"/access", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Set(utils.UserIDContextKey, userID)
	r.HandleContext(c)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

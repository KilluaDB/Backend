package backup

import (
	"testing"

	"backend/internal/mocks"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRoutes(t *testing.T) {
	h := NewHandler(NewService(mocks.NewProjectStore(), stubDSN{}))
	routes := NewRoutes(h)
	require.NotNil(t, routes)
	assert.Same(t, h, routes.handler)
}

func TestRegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(NewService(mocks.NewProjectStore(), stubDSN{}))
	routes := NewRoutes(h)

	engine := gin.New()
	routes.RegisterRoutes(engine.Group("/api/v1"))

	// Collect registered method+path pairs.
	registered := map[string]bool{}
	for _, ri := range engine.Routes() {
		registered[ri.Method+" "+ri.Path] = true
	}

	assert.True(t, registered["GET /api/v1/projects/:id/export"])
	assert.True(t, registered["POST /api/v1/projects/:id/import"])
}

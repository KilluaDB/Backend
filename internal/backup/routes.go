package backup

import (
	"backend/internal/middlewares"

	"github.com/gin-gonic/gin"
)

// Routes registers /api/v1/projects/:id/{export,import}.
// Both endpoints are authenticated; project ownership and DB-type dispatch are
// handled inside the service layer (since the same routes serve both Postgres
// and MongoDB projects).
type Routes struct {
	handler *Handler
}

func NewRoutes(handler *Handler) *Routes {
	return &Routes{handler: handler}
}

func (r *Routes) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/projects/:id")
	g.Use(middlewares.Authenticate)
	{
		g.GET("/export", r.handler.Export)
		g.POST("/import", r.handler.Import)
	}
}

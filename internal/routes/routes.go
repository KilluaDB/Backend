package routes

import (
	"my_project/internal/handlers"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(router *gin.Engine, authHandler *handlers.AuthHandler, userHandler *handlers.UserHandler, projectHandler *handlers.ProjectHandler, queryHandler *handlers.QueryHandler) {
	api := router.Group("/api/v1")

	authRoutes := NewAuthRoutes(authHandler)
	authRoutes.RegisterRoutes(api)

	userRoutes := NewUserRoutes(userHandler)
	userRoutes.RegisterRoutes(api)

	queryRoutes := NewQueryRoutes(queryHandler)
	queryRoutes.RegisterRoutes(api)

	projectRoutes := NewProjectRoutes(projectHandler)
	projectRoutes.RegisterRoutes(api)

	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})
}

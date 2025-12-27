package routes

import (
	"backend/internal/handlers"
	"backend/internal/repositories"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(router *gin.Engine, authHandler *handlers.AuthHandler, googleAuthHandler *handlers.GoogleAuthHandler, userHandler *handlers.UserHandler, userRepo *repositories.UserRepository, projectHandler *handlers.ProjectHandler, queryHandler *handlers.QueryHandler, schemaHandler *handlers.SchemaHandler, tableHandler *handlers.TableHandler) {
	api := router.Group("/api/v1")

	authRoutes := NewAuthRoutes(authHandler, googleAuthHandler)
	authRoutes.RegisterRoutes(api)

	userRoutes := NewUserRoutes(userHandler, userRepo)
	userRoutes.RegisterRoutes(api)

	queryRoutes := NewQueryRoutes(queryHandler)
	queryRoutes.RegisterRoutes(api)

	projectRoutes := NewProjectRoutes(projectHandler)
	projectRoutes.RegisterRoutes(api)

	schemaRoutes := NewSchemaRoutes(schemaHandler)
	schemaRoutes.RegisterRoutes(api)

	tableRoutes := NewTableRoutes(tableHandler)
	tableRoutes.RegisterRoutes(api)

	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})
}

package routes

import (
	"backend/internal/backup"
	"backend/internal/handlers"
	postgreshandler "backend/internal/postgres/handler"
	postgresroutes "backend/internal/postgres/routes"
	"backend/internal/repositories"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(
	router *gin.Engine,
	authHandler *handlers.AuthHandler,
	googleAuthHandler *handlers.GoogleAuthHandler,
	userHandler *handlers.UserHandler,
	userRepo *repositories.UserRepository,
	projectHandler *handlers.ProjectHandler,
	projectRepo *repositories.ProjectRepository,
	postgresHandler *postgreshandler.PostgresHandler,
	backupHandler *backup.Handler,
) {
	api := router.Group("/api/v1")

	authRoutes := NewAuthRoutes(authHandler, googleAuthHandler)
	authRoutes.RegisterRoutes(api)

	userRoutes := NewUserRoutes(userHandler, userRepo)
	userRoutes.RegisterRoutes(api)

	projectRoutes := NewProjectRoutes(projectHandler)
	projectRoutes.RegisterRoutes(api)

	postgresRoutes := postgresroutes.NewPostgresRoutes(projectRepo, postgresHandler)
	postgresRoutes.RegisterRoutes(api)

	backupRoutes := backup.NewRoutes(backupHandler)
	backupRoutes.RegisterRoutes(api)

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})
}

package route

import (
	"backend/internal/backup"
	"backend/internal/handler"
	mongohandler "backend/internal/mongodb/handler"
	mongoroute "backend/internal/mongodb/route"
	postgreshandler "backend/internal/postgres/handler"
	postgresroute "backend/internal/postgres/route"
	"backend/internal/repository"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(
	router *gin.Engine,
	authHandler *handler.AuthHandler,
	googleAuthHandler *handler.GoogleAuthHandler,
	userHandler *handler.UserHandler,
	userRepo *repository.UserRepository,
	projectHandler *handler.ProjectHandler,
	projectRepo *repository.ProjectRepository,
	postgresHandler *postgreshandler.PostgresHandler,
	mongoHandler *mongohandler.MongoHandler,
	backupHandler *backup.Handler,
) {
	api := router.Group("/api/v1")

	authRoutes := NewAuthRoutes(authHandler, googleAuthHandler)
	authRoutes.RegisterRoutes(api)

	userRoutes := NewUserRoutes(userHandler, userRepo)
	userRoutes.RegisterRoutes(api)

	projectRoutes := NewProjectRoutes(projectHandler)
	projectRoutes.RegisterRoutes(api)

	postgresRoutes := postgresroute.NewPostgresRoutes(projectRepo, postgresHandler)
	postgresRoutes.RegisterRoutes(api)

	mongodbRoute := mongoroute.NewMongoRoutes(projectRepo, mongoHandler)
	mongodbRoute.RegisterRoutes(api)

	backupRoutes := backup.NewRoutes(backupHandler)
	backupRoutes.RegisterRoutes(api)

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})
}

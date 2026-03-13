package routes

import (
	"backend/internal/handlers"
	"backend/internal/repositories"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(
	router *gin.Engine,
	authHandler *handlers.AuthHandler,
	googleAuthHandler *handlers.GoogleAuthHandler,
	userHandler *handlers.UserHandler,
	userRepo *repositories.UserRepository,
	projectHandler *handlers.ProjectHandler,
	queryHandler *handlers.QueryHandler,
	schemaHandler *handlers.SchemaHandler,
	tableHandler *handlers.TableHandler,
	projectRepo repositories.ProjectRepository,
	postgresHandler *handlers.PostgresHandler,
	mongodbHandler *handlers.MongoDBHandler,
) {
	api := router.Group("/api/v1")

	authRoutes := NewAuthRoutes(authHandler, googleAuthHandler)
	authRoutes.RegisterRoutes(api)

	userRoutes := NewUserRoutes(userHandler, userRepo)
	userRoutes.RegisterRoutes(api)

	projectRoutes := NewProjectRoutes(projectHandler)
	projectRoutes.RegisterRoutes(api)

	postgresRoutes := NewPostgresRoutes(projectRepo, postgresHandler, tableHandler, schemaHandler, queryHandler)
	postgresRoutes.RegisterRoutes(api)

	mongodbRoutes := NewMongoDBRoutes(projectRepo, mongodbHandler, queryHandler)
	mongodbRoutes.RegisterRoutes(api)

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	// Debug: which host is serving (pod name in K8s vs machine name when running locally)
	api.GET("/debug/host", func(c *gin.Context) {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "(unknown)"
		}
		c.JSON(http.StatusOK, gin.H{
			"host":   host,
			"tip":    "If you see a pod name (e.g. backend-xxx), requests use the in-cluster backend. If you see your PC name, use the K8s port-forward URL (e.g. http://localhost:8081).",
		})
	})
}

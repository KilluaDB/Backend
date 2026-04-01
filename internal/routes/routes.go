package routes

import (
	"backend/internal/handlers"
	mongodbhandler "backend/internal/mongodb/handler"
	mongodbroutes "backend/internal/mongodb/routes"
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
	schemaHandler *postgreshandler.SchemaHandler,
	tableHandler *postgreshandler.TableHandler,
	projectRepo repositories.ProjectRepository,
	postgresHandler *postgreshandler.PostgresHandler,
	postgresQueryHandler *postgreshandler.QueryHandler,
	mongodbHandler *mongodbhandler.MongoDBHandler,
	mongoQueryHandler *mongodbhandler.QueryHandler,
	textToSqlHandler *postgreshandler.TextToSQLHandler,
) {
	api := router.Group("/api/v1")

	authRoutes := NewAuthRoutes(authHandler, googleAuthHandler)
	authRoutes.RegisterRoutes(api)

	userRoutes := NewUserRoutes(userHandler, userRepo)
	userRoutes.RegisterRoutes(api)

	// queryRoutes := NewQueryRoutes(queryHandler)
	// queryRoutes.RegisterRoutes(api)

	textToSqlRoutes := NewTextToSqlRoutes(textToSqlHandler)
	textToSqlRoutes.RegisterRoutes(api)

	projectRoutes := NewProjectRoutes(projectHandler)
	projectRoutes.RegisterRoutes(api)

	postgresRoutes := postgresroutes.NewPostgresRoutes(projectRepo, postgresHandler, tableHandler, schemaHandler, postgresQueryHandler)
	postgresRoutes.RegisterRoutes(api)

	mongodbRoutes := mongodbroutes.NewMongoDBRoutes(projectRepo, mongodbHandler, mongoQueryHandler)
	mongodbRoutes.RegisterRoutes(api)

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})
}

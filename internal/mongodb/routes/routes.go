package routes

import (
	"backend/internal/middlewares"
	"backend/internal/mongodb/handler"
	"backend/internal/repositories"

	"github.com/gin-gonic/gin"
)

// MongoRoutes registers all /projects/:id/mongodb/* endpoints.
// Middleware ensures the project is MongoDB.
type MongoRoutes struct {
	projectRepo *repositories.ProjectRepository
	mongo       *handler.MongoHandler
}

func NewMongoRoutes(projectRepo *repositories.ProjectRepository, mongo *handler.MongoHandler) *MongoRoutes {
	return &MongoRoutes{
		projectRepo: projectRepo,
		mongo:       mongo,
	}
}

func (r *MongoRoutes) RegisterRoutes(router *gin.RouterGroup) {
	mongodb := router.Group("/projects/:id/mongodb")
	mongodb.Use(middlewares.Authenticate)
	mongodb.Use(middlewares.RequireMongoProject(r.projectRepo))
	h := r.mongo
	{
		// Collections
		mongodb.GET("/collections", h.Collection.ListCollections)
		mongodb.POST("/collections", h.Collection.CreateCollection)
		mongodb.DELETE("/collections/:collection", h.Collection.DeleteCollection)
		mongodb.POST("/collections/:collection/fields", h.Collection.AddField)
		mongodb.DELETE("/collections/:collection/fields/:field", h.Collection.RemoveField)
	}
}

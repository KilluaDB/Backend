package route

import (
	"backend/internal/middleware"
	"backend/internal/mongodb/handler"
	"backend/internal/repository"

	"github.com/gin-gonic/gin"
)

// MongoRoutes registers all /projects/:id/mongodb/* endpoints.
// Middleware ensures the project is MongoDB.
type MongoRoutes struct {
	projectRepo *repository.ProjectRepository
	mongo       *handler.MongoHandler
}

func NewMongoRoutes(projectRepo *repository.ProjectRepository, mongo *handler.MongoHandler) *MongoRoutes {
	return &MongoRoutes{
		projectRepo: projectRepo,
		mongo:       mongo,
	}
}

func (r *MongoRoutes) RegisterRoutes(router *gin.RouterGroup) {
	mongodb := router.Group("/projects/:id/mongodb")
	mongodb.Use(middleware.Authenticate)
	mongodb.Use(middleware.RequireMongoProject(r.projectRepo))
	h := r.mongo
	{
		// Metrics
		mongodb.GET("/dashboard/metrics", h.Dashboard.GetMetrics)

		// Collections
		mongodb.GET("/collections", h.Collection.ListCollections)
		mongodb.POST("/collections", h.Collection.CreateCollection)
		mongodb.DELETE("/collections/:collection", h.Collection.DeleteCollection)
		mongodb.POST("/collections/:collection/fields", h.Collection.AddField)
		mongodb.DELETE("/collections/:collection/fields/:field", h.Collection.RemoveField)

		// Documents
		mongodb.POST("/collections/:collection/documents/query", h.Document.QueryDocuments)
		mongodb.POST("/collections/:collection/documents/count", h.Document.CountDocuments)

		mongodb.GET("/collections/:collection/documents/:docId", h.Document.GetDocument)
		mongodb.DELETE("/collections/:collection/documents/:docId", h.Document.DeleteDocument)

		mongodb.PATCH("/collections/:collection/documents/:docId/fields/:field", h.Document.UpdateDocumentField)
		mongodb.POST("/collections/:collection/documents/:docId/fields", h.Document.AddDocumentField)
		mongodb.DELETE("/collections/:collection/documents/:docId/fields/:field", h.Document.DeleteDocumentField)
		
		mongodb.GET("/collections/:collection/documents", h.Document.GetDocuments)
		mongodb.POST("/collections/:collection/documents", h.Document.InsertDocuments)
		mongodb.PATCH("/collections/:collection/documents", h.Document.UpdateDocuments)
		mongodb.DELETE("/collections/:collection/documents", h.Document.DeleteDocuments)
	}
}

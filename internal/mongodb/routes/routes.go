package routes

//
//import (
//	"backend/internal/middlewares"
//	"backend/internal/mongodb/handler"
//	"backend/internal/repositories"
//
//	"github.com/gin-gonic/gin"
//)
//
//// MongoDBRoutes registers all /projects/:id/mongodb/* endpoints.
//// Middleware ensures the project is MongoDB.
//type MongoDBRoutes struct {
//	projectRepo    repositories.ProjectRepository
//	mongodbHandler *handler.MongoDBHandler
//	queryHandler   *handler.QueryHandler
//}
//
//func NewMongoDBRoutes(
//	projectRepo repositories.ProjectRepository,
//	mongodbHandler *handler.MongoDBHandler,
//	queryHandler *handler.QueryHandler,
//) *MongoDBRoutes {
//	return &MongoDBRoutes{
//		projectRepo:    projectRepo,
//		mongodbHandler: mongodbHandler,
//		queryHandler:   queryHandler,
//	}
//}
//
//func (r *MongoDBRoutes) RegisterRoutes(router *gin.RouterGroup) {
//	mongodb := router.Group("/projects/:id/mongodb")
//	mongodb.Use(middlewares.Authenticate)
//	mongodb.Use(middlewares.RequireMongoProject(r.projectRepo))
//	{
//		// Collections
//		mongodb.GET("/collections", r.mongodbHandler.ListCollections)
//		mongodb.POST("/collections", r.mongodbHandler.CreateCollection)
//		mongodb.DELETE("/collections/:collection", r.mongodbHandler.DeleteCollection)
//
//		// Documents
//		mongodb.GET("/collections/:collection/documents", r.mongodbHandler.GetDocuments)
//		mongodb.POST("/collections/:collection/documents", r.mongodbHandler.InsertDocument)
//		mongodb.PATCH("/collections/:collection/documents", r.mongodbHandler.UpdateDocuments)
//		mongodb.DELETE("/collections/:collection/documents", r.mongodbHandler.DeleteDocuments)
//
//		// Fields (optional)
//		mongodb.POST("/collections/:collection/fields", r.mongodbHandler.AddField)
//		mongodb.DELETE("/collections/:collection/fields/:field", r.mongodbHandler.RemoveField)
//
//		// Query
//		mongodb.POST("/query/execute", r.queryHandler.ExecuteQuery)
//		mongodb.GET("/query/history", r.queryHandler.GetQueryHistory)
//	}
//}

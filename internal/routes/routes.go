package routes

import (
	"my_project/internal/handlers"
	"my_project/internal/repositories"
	"net/http"

	"github.com/gin-gonic/gin"
)

<<<<<<< HEAD
<<<<<<< HEAD
func RegisterRoutes(router *gin.Engine, authHandler *handlers.AuthHandler, userHandler *handlers.UserHandler, projectHandler *handlers.ProjectHandler, queryHandler *handlers.QueryHandler, googleAuthHandler *handlers.GoogleAuthHandler) {
=======
func RegisterRoutes(router *gin.Engine, authHandler *handlers.AuthHandler, userHandler *handlers.UserHandler, projectHandler *handlers.ProjectHandler, queryHandler *handlers.QueryHandler, userRepo *repositories.UserRepository) {
>>>>>>> 0b8cb02 (Add Insert / Delete Row or Column and GET / Update / Delete user / me)
=======
func RegisterRoutes(
	router *gin.Engine, 
	authHandler *handlers.AuthHandler, 
	userHandler *handlers.UserHandler, 
	projectHandler *handlers.ProjectHandler, 
	queryHandler *handlers.QueryHandler, 
	googleAuthHandler *handlers.GoogleAuthHandler,
	tableHandler *handlers.TableHandler,
) {
>>>>>>> feature/oauth2.0
	api := router.Group("/api/v1")

	authRoutes := NewAuthRoutes(authHandler, googleAuthHandler)
	authRoutes.RegisterRoutes(api)

	userRoutes := NewUserRoutes(userHandler, userRepo)
	userRoutes.RegisterRoutes(api)

	queryRoutes := NewQueryRoutes(queryHandler)
	queryRoutes.RegisterRoutes(api)

	projectRoutes := NewProjectRoutes(projectHandler)
	projectRoutes.RegisterRoutes(api)

	tableRoutes := NewTableRoutes(tableHandler)
	tableRoutes.RegisterRoutes(api)

	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})
}

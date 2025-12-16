package routes

import (
	"my_project/internal/handlers"
	"my_project/internal/middlewares"
	"my_project/internal/repositories"

	"github.com/gin-gonic/gin"
)

type UserRoutes struct {
	userHandler *handlers.UserHandler
	userRepo    *repositories.UserRepository
}

func NewUserRoutes(userHandler *handlers.UserHandler, userRepo *repositories.UserRepository) *UserRoutes {
	return &UserRoutes{
		userHandler: userHandler,
		userRepo:    userRepo,
	}
}

func (r *UserRoutes) RegisterRoutes(router *gin.RouterGroup) {
	users := router.Group("/users")
	users.Use(middlewares.Authenticate) // All user routes require authentication
	{
		// User's own endpoints (no special authorization needed)
		users.GET("/me", r.userHandler.GetMe)
		users.PATCH("/me", r.userHandler.UpdateMe)
		users.DELETE("/me", r.userHandler.DeleteMe)

		// Admin-only routes
		users.GET("", middlewares.RequireAdmin(r.userRepo), r.userHandler.ListUsers)
		users.GET("/:user_id", middlewares.RequireAdmin(r.userRepo), r.userHandler.GetUser)
		users.PATCH("/:user_id", middlewares.RequireAdmin(r.userRepo), r.userHandler.UpdateUser)
		users.DELETE("/:user_id", middlewares.RequireAdmin(r.userRepo), r.userHandler.DeleteUser)
	}
}

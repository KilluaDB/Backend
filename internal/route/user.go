package route

import (
	"backend/internal/handler"
	"backend/internal/middleware"
	"backend/internal/repository"

	"github.com/gin-gonic/gin"
)

type UserRoutes struct {
	userHandler *handler.UserHandler
	userRepo    *repository.UserRepository
}

func NewUserRoutes(userHandler *handler.UserHandler, userRepo *repository.UserRepository) *UserRoutes {
	return &UserRoutes{
		userHandler: userHandler,
		userRepo:    userRepo,
	}
}

func (r *UserRoutes) RegisterRoutes(router *gin.RouterGroup) {
	users := router.Group("/users")
	users.Use(middleware.Authenticate) // All user routes require authentication
	{
		// User's own endpoints (no special authorization needed)
		users.GET("/me", r.userHandler.GetMe)
		users.PATCH("/me", r.userHandler.UpdateMe)
		users.DELETE("/me", r.userHandler.DeleteMe)

		// Admin-only routes
		users.GET("", middleware.RequireAdmin(r.userRepo), r.userHandler.ListUsers)
		users.GET("/:user_id", middleware.RequireAdmin(r.userRepo), r.userHandler.GetUser)
		users.PATCH("/:user_id", middleware.RequireAdmin(r.userRepo), r.userHandler.UpdateUser)
		users.DELETE("/:user_id", middleware.RequireAdmin(r.userRepo), r.userHandler.DeleteUser)
	}
}

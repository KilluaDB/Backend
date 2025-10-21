package routes

import (
	"my_project/internal/handlers"
	"my_project/internal/middlewares"

	"github.com/gin-gonic/gin"
)

type AuthRoutes struct {
	handler *handlers.AuthHandler
}

func NewAuthRoutes(hander *handlers.AuthHandler) *AuthRoutes {
	return &AuthRoutes{handler: hander}
}

func (r *AuthRoutes) RegisterRoutes(router *gin.RouterGroup) {
	auth := router.Group("/auth")
	{
		// Public routes
		auth.POST("/register", r.handler.Register)
		auth.POST("/login", r.handler.Login)

		// Protected routes
		protected := auth.Group("/")
		protected.Use(middlewares.Authenticate)
		protected.POST("/logout", r.handler.Logout)
		auth.POST("/refresh", r.handler.RefreshToken)
	}
}

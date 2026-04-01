package routes

import (
	"backend/internal/handlers"
	"backend/internal/middlewares"

	"github.com/gin-gonic/gin"
)

type AuthRoutes struct {
	handler           *handlers.AuthHandler
	googleAuthHandler *handlers.GoogleAuthHandler
}

func NewAuthRoutes(hander *handlers.AuthHandler, googleAuthHandler *handlers.GoogleAuthHandler) *AuthRoutes {
	return &AuthRoutes{
		handler:           hander,
		googleAuthHandler: googleAuthHandler,
	}
}

func (r *AuthRoutes) RegisterRoutes(router *gin.RouterGroup) {
	auth := router.Group("/auth")
	{
		// Public routes
		auth.POST("/register", r.handler.Register)
		auth.POST("/login", r.handler.Login)
		auth.GET("/google/login", r.googleAuthHandler.Login)       // the one it’s serving the static files for the UI
		auth.GET("/google/callback", r.googleAuthHandler.Callback) // the callback path, when you are developing a website which needs an external OAuth technology, at the moment you sent the data you will got a response to a callback endpoint of your API

		// Protected routes
		protected := auth.Group("/")
		protected.Use(middlewares.Authenticate)
		protected.POST("/logout", r.handler.Logout)
		auth.POST("/refresh", r.handler.Refresh)
	}
}

package routes

import (
	"my_project/internal/handlers"

	"github.com/gin-gonic/gin"
)

type UserRoutes struct {
	userHandler *handlers.UserHandler
}

func NewUserRoutes(userHander *handlers.UserHandler) *UserRoutes {
	return &UserRoutes{userHandler: userHander}
}

func (r *UserRoutes) RegisterRoutes(router *gin.RouterGroup) {
	router.Group("/users")
	{

	}
}

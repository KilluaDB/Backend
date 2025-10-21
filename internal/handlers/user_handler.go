package handlers

import (
	"my_project/internal/services"
)

type UserHandler struct {
	userService *services.UserService
}

func NewUserHandler(userService *services.UserService) *UserHandler {
	return &UserHandler{userService: userService}
}

// GetUsers
// GetUserById
// CreateUser
// UpdateUser
// DeleteUser

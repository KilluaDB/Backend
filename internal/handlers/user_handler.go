package handlers

import (
	"my_project/internal/services"
)

type UserHandler struct {
	authService *services.AuthService
}

func NewUserHandler(authService *services.AuthService) *UserHandler {
	return &UserHandler{authService: authService}
}

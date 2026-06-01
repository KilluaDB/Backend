package handler

import (
	"backend/internal/response"
	"backend/internal/utils"
	"backend/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type UserHandler struct {
	userService *service.UserService
}

func NewUserHandler(userService *service.UserService) *UserHandler {
	return &UserHandler{userService: userService}
}

// GetMe handles GET /api/v1/users/me
func (h *UserHandler) GetMe(c *gin.Context) {
	// Get authenticated user ID from context
	userUUID, ok := utils.UserIDFromGin(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	user, err := h.userService.GetUser(c, userUUID)
	if err != nil {
		if err.Error() == "user not found" {
			response.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		response.Fail(c, http.StatusInternalServerError, err, "Failed to retrieve user")
		return
	}

	response.Success(c, http.StatusOK, user, "User retrieved successfully")
}

// GetUser handles GET /api/v1/users/:user_id (admin only)
func (h *UserHandler) GetUser(c *gin.Context) {
	// Get user ID from URL parameter
	userIDStr := c.Param("user_id")
	userUUID, err := uuid.Parse(userIDStr)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	user, err := h.userService.GetUser(c, userUUID)
	if err != nil {
		if err.Error() == "user not found" {
			response.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		response.Fail(c, http.StatusInternalServerError, err, "Failed to retrieve user")
		return
	}

	response.Success(c, http.StatusOK, user, "User retrieved successfully")
}

// UpdateMe handles PATCH /api/v1/users/me
func (h *UserHandler) UpdateMe(c *gin.Context) {
	// Get authenticated user ID from context
	userUUID, ok := utils.UserIDFromGin(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	var req service.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	user, err := h.userService.UpdateUser(c, userUUID, userUUID, req)
	if err != nil {
		if err.Error() == "user not found" {
			response.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		// Check for policy errors
		if err.Error() == "only admins can change user roles" ||
			err.Error() == "admin cannot demote themselves" {
			response.Fail(c, http.StatusForbidden, err, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, err, "Failed to update user")
		return
	}

	response.Success(c, http.StatusOK, user, "User updated successfully")
}

// UpdateUser handles PATCH /api/v1/users/:user_id (admin only)
func (h *UserHandler) UpdateUser(c *gin.Context) {
	// Get authenticated user ID from context
	authenticatedUUID, ok := utils.UserIDFromGin(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	// Get user ID from URL parameter
	userIDStr := c.Param("user_id")
	userUUID, err := uuid.Parse(userIDStr)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	var req service.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	user, err := h.userService.UpdateUser(c, userUUID, authenticatedUUID, req)
	if err != nil {
		if err.Error() == "user not found" {
			response.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		// Check for policy errors
		if err.Error() == "only admins can change user roles" ||
			err.Error() == "admin cannot demote themselves" {
			response.Fail(c, http.StatusForbidden, err, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, err, "Failed to update user")
		return
	}

	response.Success(c, http.StatusOK, user, "User updated successfully")
}

// DeleteMe handles DELETE /api/v1/users/me
func (h *UserHandler) DeleteMe(c *gin.Context) {
	// Get authenticated user ID from context
	userUUID, ok := utils.UserIDFromGin(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	err := h.userService.DeleteUser(c, userUUID, userUUID)
	if err != nil {
		if err.Error() == "user not found" {
			response.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		// Check for policy errors
		if err.Error() == "admins cannot delete other admins" ||
			err.Error() == "cannot delete the last admin" {
			response.Fail(c, http.StatusForbidden, err, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, err, "Failed to delete user")
		return
	}

	// revoke the access token
	res := gin.H{
		"access_token": "",
	}

	// TODO: try to find a way to clear the access_token and use http.StatusNoContent
	response.Success(c, http.StatusOK, res, "User deleted successfully")
}

// DeleteUser handles DELETE /api/v1/users/:user_id (admin only)
func (h *UserHandler) DeleteUser(c *gin.Context) {
	// Get authenticated user ID from context
	authenticatedUUID, ok := utils.UserIDFromGin(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	// Get user ID from URL parameter
	userIDStr := c.Param("user_id")
	userUUID, err := uuid.Parse(userIDStr)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	err = h.userService.DeleteUser(c, userUUID, authenticatedUUID)
	if err != nil {
		if err.Error() == "user not found" {
			response.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		// Check for policy errors
		if err.Error() == "admins cannot delete other admins" ||
			err.Error() == "cannot delete the last admin" {
			response.Fail(c, http.StatusForbidden, err, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, err, "Failed to delete user")
		return
	}

	response.Success(c, http.StatusNoContent, nil, "User deleted successfully")
}

// ListUsers handles GET /api/v1/users
func (h *UserHandler) ListUsers(c *gin.Context) {
	users, err := h.userService.GetAllUsers(c)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err, "Failed to retrieve users")
		return
	}

	response.Success(c, http.StatusOK, users, "Users retrieved successfully")
}

package handlers

import (
	"backend/internal/responses"
	"backend/internal/services"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type UserHandler struct {
	userService *services.UserService
}

func NewUserHandler(userService *services.UserService) *UserHandler {
	return &UserHandler{userService: userService}
}

// GetMe handles GET /api/v1/users/me
func (h *UserHandler) GetMe(c *gin.Context) {
	// Get authenticated user ID from context (set by Authenticate middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	// Convert to UUID
	var userUUID uuid.UUID
	switch v := userID.(type) {
	case uuid.UUID:
		userUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
			return
		}
		userUUID = parsed
	default:
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	user, err := h.userService.GetUser(userUUID)
	if err != nil {
		if err.Error() == "user not found" {
			responses.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to retrieve user")
		return
	}

	responses.Success(c, http.StatusOK, user, "User retrieved successfully")
}

// GetUser handles GET /api/v1/users/:user_id (admin only)
func (h *UserHandler) GetUser(c *gin.Context) {
	// Get user ID from URL parameter
	userIDStr := c.Param("user_id")
	userUUID, err := uuid.Parse(userIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	user, err := h.userService.GetUser(userUUID)
	if err != nil {
		if err.Error() == "user not found" {
			responses.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to retrieve user")
		return
	}

	responses.Success(c, http.StatusOK, user, "User retrieved successfully")
}

// UpdateMe handles PATCH /api/v1/users/me
func (h *UserHandler) UpdateMe(c *gin.Context) {
	// Get authenticated user ID from context (set by Authenticate middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	// Convert to UUID
	var userUUID uuid.UUID
	switch v := userID.(type) {
	case uuid.UUID:
		userUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
			return
		}
		userUUID = parsed
	default:
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	var req services.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	user, err := h.userService.UpdateUser(userUUID, userUUID, req)
	if err != nil {
		if err.Error() == "user not found" {
			responses.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		// Check for policy errors
		if err.Error() == "only admins can change user roles" ||
			err.Error() == "admin cannot demote themselves" {
			responses.Fail(c, http.StatusForbidden, err, err.Error())
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to update user")
		return
	}

	responses.Success(c, http.StatusOK, user, "User updated successfully")
}

// UpdateUser handles PATCH /api/v1/users/:user_id (admin only)
func (h *UserHandler) UpdateUser(c *gin.Context) {
	// Get authenticated user ID from context (set by Authenticate middleware)
	authenticatedUserID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	// Convert authenticated user ID to UUID
	var authenticatedUUID uuid.UUID
	switch v := authenticatedUserID.(type) {
	case uuid.UUID:
		authenticatedUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
			return
		}
		authenticatedUUID = parsed
	default:
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	// Get user ID from URL parameter
	userIDStr := c.Param("user_id")
	userUUID, err := uuid.Parse(userIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	var req services.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	user, err := h.userService.UpdateUser(userUUID, authenticatedUUID, req)
	if err != nil {
		if err.Error() == "user not found" {
			responses.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		// Check for policy errors
		if err.Error() == "only admins can change user roles" ||
			err.Error() == "admin cannot demote themselves" {
			responses.Fail(c, http.StatusForbidden, err, err.Error())
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to update user")
		return
	}

	responses.Success(c, http.StatusOK, user, "User updated successfully")
}

// DeleteMe handles DELETE /api/v1/users/me
func (h *UserHandler) DeleteMe(c *gin.Context) {
	// Get authenticated user ID from context (set by Authenticate middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	// Convert to UUID
	var userUUID uuid.UUID
	switch v := userID.(type) {
	case uuid.UUID:
		userUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
			return
		}
		userUUID = parsed
	default:
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	err := h.userService.DeleteUser(userUUID, userUUID)
	if err != nil {
		if err.Error() == "user not found" {
			responses.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		// Check for policy errors
		if err.Error() == "admins cannot delete other admins" ||
			err.Error() == "cannot delete the last admin" {
			responses.Fail(c, http.StatusForbidden, err, err.Error())
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete user")
		return
	}

	// revoke the access token
	res := gin.H{
		"access_token": "",
	}

	// TODO: try to find a way to clear the access_token and use http.StatusNoContent
	responses.Success(c, http.StatusOK, res, "User deleted successfully")
}

// DeleteUser handles DELETE /api/v1/users/:user_id (admin only)
func (h *UserHandler) DeleteUser(c *gin.Context) {
	// Get authenticated user ID from context (set by Authenticate middleware)
	authenticatedUserID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	// Convert authenticated user ID to UUID
	var authenticatedUUID uuid.UUID
	switch v := authenticatedUserID.(type) {
	case uuid.UUID:
		authenticatedUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
			return
		}
		authenticatedUUID = parsed
	default:
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	// Get user ID from URL parameter
	userIDStr := c.Param("user_id")
	userUUID, err := uuid.Parse(userIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID format")
		return
	}

	err = h.userService.DeleteUser(userUUID, authenticatedUUID)
	if err != nil {
		if err.Error() == "user not found" {
			responses.Fail(c, http.StatusNotFound, err, "User not found")
			return
		}
		// Check for policy errors
		if err.Error() == "admins cannot delete other admins" ||
			err.Error() == "cannot delete the last admin" {
			responses.Fail(c, http.StatusForbidden, err, err.Error())
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete user")
		return
	}

	responses.Success(c, http.StatusNoContent, nil, "User deleted successfully")
}

// ListUsers handles GET /api/v1/users
func (h *UserHandler) ListUsers(c *gin.Context) {
	users, err := h.userService.GetAllUsers()
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to retrieve users")
		return
	}

	responses.Success(c, http.StatusOK, users, "Users retrieved successfully")
}

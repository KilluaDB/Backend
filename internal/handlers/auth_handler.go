package handlers

import (
	"my_project/internal/models"
	"my_project/internal/responses"
	"my_project/internal/services"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	userService *services.UserService
}

func NewAuthHandler(userService *services.UserService) *AuthHandler {
	return &AuthHandler{userService: userService}
}

func (h *AuthHandler) Register(c *gin.Context) {
	// 1. Validate input
	var user models.User
	if err := c.ShouldBind(&user); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Please provide your email and password correctly")
		return
	}

	user.Prepare()

	// 2. Register user (and get tokens)
	accessToken, refreshToken, sessionID, err := h.userService.Register(&user)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Could not register user")
		return
	}

	// 3. Return tokens and user info
	res := gin.H{
		"message":                  "User registered successfully",
		"user":                     user,
		"session_id":               sessionID,
		"access_token":             accessToken,
		"refresh_token":            refreshToken,
		"access_token_expires_in":  "15m",
		"refresh_token_expires_in": "24h",
	}

	responses.Success(c, http.StatusOK, res, "New user registered successfully!")
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	access, refresh, sessionID, err := h.userService.Login(req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	res := gin.H{
		"session_id":               sessionID,
		"access_token":             access,
		"refresh_token":            refresh,
		"access_token_expires_at":  time.Now().Add(15 * time.Minute),
		"refresh_token_expires_at": time.Now().Add(24 * time.Hour),
	}

	responses.Success(c, http.StatusOK, res, "User Login Successfully!")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	userID, exists := c.Get("userID") // Extracted from access token
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	if err := h.userService.LogoutByUserID(userID.(uint)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not revoke token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}

	// 1. Parse request body
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Refresh token required"})
		return
	}

	// 2. Ask the service layer to issue a new access token
	accessToken, err := h.userService.Refresh(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired refresh token"})
		return
	}

	// 3. Return the new access token
	c.JSON(http.StatusOK, gin.H{
		"access_token": accessToken,
		"message":      "Access token refreshed successfully",
	})
}

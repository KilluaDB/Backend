package handlers

import (
	"backend/internal/models"
	"backend/internal/responses"
	"backend/internal/services"
	_ "log"

	"net/http"

	"github.com/gin-gonic/gin"
)

// Cookie configuration
const (
	RefreshTokenCookieName = "refresh_token"
	RefreshTokenMaxAge     = 30 * 24 * 3600 // 30 days in seconds
)

type AuthHandler struct {
	authService *services.AuthService
}

func NewAuthHandler(authService *services.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) Register(c *gin.Context) {
	// 1. Validate input
	var req struct {
		Email    string `json:"email"    binding:"required,email"`
		Password string `json:"password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Please provide your email and password correctly")
		return
	}

	// 2. Register user (and get tokens)
	user := &models.User{
		Email:    req.Email,
		Password: req.Password,
	}
	accessToken, refreshToken, err := h.authService.Register(user)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Could not register user")
		return
	}

	c.SetCookie("refresh_token", refreshToken, 30*24*3600, "/", "", true, true)

	// 4. Return only access token in response body
	res := gin.H{
		"access_token": accessToken,
	}

	responses.Success(c, http.StatusCreated, res, "New user registered successfully!")
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email"    binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid Format")
		return
	}

	accessToken, refreshToken, err := h.authService.Login(req.Email, req.Password)
	if err != nil {
		responses.Fail(c, http.StatusUnauthorized, err, "Failed to login")
		return
	}

	c.SetCookie("refresh_token", refreshToken, 30*24*3600, "/", "", true, true)

	res := gin.H{
		"access_token": accessToken,
	}

	responses.Success(c, http.StatusOK, res, "User Login Successfully!")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	// refreshToken, err := c.Cookie("refresh_token")
	// if err != nil {
	// 	responses.Fail(c, http.StatusBadRequest, nil, "Missing refresh token")
	// 	return
	// }

	// _, exists := c.Get("userId") // Extracted from access token
	// if !exists {
	// 	responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
	// 	return
	// }

	// if err := h.userService.Logout(refreshToken); err != nil {
	// 	responses.Fail(c, http.StatusUnauthorized, err, "Could not revoke token")
	// 	return
	// }

	c.SetCookie("refresh_token", "", -1, "/", "", true, true)

	responses.Success(c, http.StatusOK, nil, "Logged out successfully")
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	// 1. Get refresh token from HttpOnly cookie
	refreshToken, err := c.Cookie(RefreshTokenCookieName)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Missing refresh token")
		return
	}

	// 2. Validate and generate new tokens (with rotation)
	accessToken, newRefreshToken, err := h.authService.Refresh(refreshToken)
	if err != nil {
		c.SetCookie("refresh_token", "", -1, "/", "", true, true)
		responses.Fail(c, http.StatusUnauthorized, err, "Invalid or expired refresh token")
		return
	}

	c.SetCookie("refresh_token", newRefreshToken, 30*24*3600, "/", "", true, true)

	res := gin.H{
		"access_token": accessToken,
	}

	responses.Success(c, http.StatusOK, res, "Access token refreshed successfully")
}

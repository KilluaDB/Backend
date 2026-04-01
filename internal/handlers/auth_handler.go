package handlers

import (
	"backend/internal/models"
	"backend/internal/responses"
	"backend/internal/services"
	"errors"
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
	userID, accessToken, refreshToken, err := h.authService.Register(user)
	if err != nil {
		if errors.Is(err, services.ErrUserAlreadyExists) {
			responses.Fail(c, http.StatusConflict, err, "An account with this email already exists")
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Could not register user")
		return
	}

	c.SetCookie("refresh_token", refreshToken, 30*24*3600, "/", "", true, true)

	res := gin.H{
		"user_id":      userID.String(),
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
		responses.Fail(c, http.StatusBadRequest, err, "Invalid email or password format")
		return
	}

	userID, accessToken, refreshToken, err := h.authService.Login(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, services.ErrUserNotFound) || errors.Is(err, services.ErrInvalidPassword) {
			responses.Fail(c, http.StatusUnauthorized, err, "Invalid email or password")
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Could not complete login")
		return
	}

	c.SetCookie("refresh_token", refreshToken, 30*24*3600, "/", "", true, true)

	res := gin.H{
		"user_id":      userID.String(),
		"access_token": accessToken,
	}

	responses.Success(c, http.StatusOK, res, "User logged in successfully")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	refreshToken, err := c.Cookie(RefreshTokenCookieName)
	if err != nil {
		// No cookie is acceptable (e.g. already logged out); clear cookie and succeed
		c.SetCookie(RefreshTokenCookieName, "", -1, "/", "", true, true)
		responses.Success(c, http.StatusOK, nil, "Logged out successfully")
		return
	}

	if revokeErr := h.authService.Logout(refreshToken); revokeErr != nil {
		responses.Fail(c, http.StatusInternalServerError, revokeErr, "Could not revoke session")
		return
	}

	c.SetCookie(RefreshTokenCookieName, "", -1, "/", "", true, true)
	responses.Success(c, http.StatusOK, nil, "Logged out successfully")
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(RefreshTokenCookieName)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Missing refresh token")
		return
	}

	userID, accessToken, newRefreshToken, err := h.authService.Refresh(refreshToken)
	if err != nil {
		c.SetCookie(RefreshTokenCookieName, "", -1, "/", "", true, true)
		responses.Fail(c, http.StatusUnauthorized, err, "Invalid or expired refresh token")
		return
	}

	c.SetCookie(RefreshTokenCookieName, newRefreshToken, RefreshTokenMaxAge, "/", "", true, true)

	res := gin.H{
		"user_id":      userID.String(),
		"access_token": accessToken,
	}

	responses.Success(c, http.StatusOK, res, "Access token refreshed successfully")
}

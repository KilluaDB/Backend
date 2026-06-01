package handler

import (
	"backend/internal/model"
	"backend/internal/response"
	"backend/internal/service"
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
	authService *service.AuthService
}

func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) Register(c *gin.Context) {
	// 1. Validate input
	var req struct {
		Email    string `json:"email"    binding:"required,email"`
		Password string `json:"password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Please provide your email and password correctly")
		return
	}

	// 2. Register user (and get tokens)
	user := &model.User{
		Email:    req.Email,
		Password: req.Password,
	}
	userID, accessToken, refreshToken, err := h.authService.Register(c, user)
	if err != nil {
		if errors.Is(err, service.ErrUserAlreadyExists) {
			response.Fail(c, http.StatusConflict, err, "An account with this email already exists")
			return
		}
		response.Fail(c, http.StatusInternalServerError, err, "Could not register user")
		return
	}

	c.SetCookie("refresh_token", refreshToken, 30*24*3600, "/", "", true, true)

	res := gin.H{
		"user_id":      userID.String(),
		"access_token": accessToken,
	}

	response.Success(c, http.StatusCreated, res, "New user registered successfully!")
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email"    binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid email or password format")
		return
	}

	userID, accessToken, refreshToken, err := h.authService.Login(c, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) || errors.Is(err, service.ErrInvalidPassword) {
			response.Fail(c, http.StatusUnauthorized, err, "Invalid email or password")
			return
		}
		response.Fail(c, http.StatusInternalServerError, err, "Could not complete login")
		return
	}

	c.SetCookie("refresh_token", refreshToken, 30*24*3600, "/", "", true, true)

	res := gin.H{
		"user_id":      userID.String(),
		"access_token": accessToken,
	}

	response.Success(c, http.StatusOK, res, "User logged in successfully")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	refreshToken, err := c.Cookie(RefreshTokenCookieName)
	if err != nil {
		// No cookie is acceptable (e.g. already logged out); clear cookie and succeed
		c.SetCookie(RefreshTokenCookieName, "", -1, "/", "", true, true)
		response.Success(c, http.StatusOK, nil, "Logged out successfully")
		return
	}

	if revokeErr := h.authService.Logout(c, refreshToken); revokeErr != nil {
		response.Fail(c, http.StatusInternalServerError, revokeErr, "Could not revoke session")
		return
	}

	c.SetCookie(RefreshTokenCookieName, "", -1, "/", "", true, true)
	response.Success(c, http.StatusOK, nil, "Logged out successfully")
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(RefreshTokenCookieName)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Missing refresh token")
		return
	}

	userID, accessToken, newRefreshToken, err := h.authService.Refresh(c, refreshToken)
	if err != nil {
		c.SetCookie(RefreshTokenCookieName, "", -1, "/", "", true, true)
		response.Fail(c, http.StatusUnauthorized, err, "Invalid or expired refresh token")
		return
	}

	c.SetCookie(RefreshTokenCookieName, newRefreshToken, RefreshTokenMaxAge, "/", "", true, true)

	res := gin.H{
		"user_id":      userID.String(),
		"access_token": accessToken,
	}

	response.Success(c, http.StatusOK, res, "Access token refreshed successfully")
}

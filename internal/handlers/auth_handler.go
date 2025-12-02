package handlers

import (
	_ "log"
	"my_project/internal/responses"
	"my_project/internal/services"
	"net/http"

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
	var req struct {
		Email    string `json:"email"    binding:"required,email"`
		Password string `json:"password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Please provide your email and password correctly")
		return
	}

	// 2. Register user (and get tokens)
	ctx := c.Request.Context()
	accessToken, refreshToken, err := h.userService.Register(req.Email, req.Password, ctx)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Could not register user")
		return
	}

	c.SetCookie("refresh_token", refreshToken, 30*24*3600, "/", "", true, true)

	// 3. Return tokens
	res := gin.H{
		"message":                  "User registered successfully",
		"access_token":             accessToken,
		"refresh_token":            refreshToken,
		"access_token_expires_in":  "15m",
		"refresh_token_expires_in": "24h",
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

	ctx := c.Request.Context()
	accessToken, refreshToken, err := h.userService.Login(req.Email, req.Password, ctx)
	if err != nil {
		responses.Fail(c, http.StatusUnauthorized, err, "Failed to login")
		return
	}

	c.SetCookie("refresh_token", refreshToken, 30*24*3600, "/", "", true, true)

	res := gin.H{
		"access_token":             accessToken,
	}

	responses.Success(c, http.StatusOK, res, "User Login Successfully!")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, nil, "Missing refresh token")
		return
	}
	
	_, exists := c.Get("userId") // Extracted from access token
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	ctx := c.Request.Context()
	if err := h.userService.Logout(ctx, refreshToken); err != nil {
		responses.Fail(c, http.StatusUnauthorized, err, "Could not revoke token")
		return
	}

	c.SetCookie("refresh_token", "", -1, "/", "", true, true)

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

func (h *AuthHandler) Refresh(c *gin.Context) {	
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Missing refresh token")
		return
	}

	// 2. Ask the service layer to issue a new access token
	ctx := c.Request.Context()
	accessToken, refresToken, err := h.userService.Refresh(ctx, refreshToken)
	if err != nil {
		responses.Fail(c, http.StatusUnauthorized, err, "Invalid or expired refresh token")
		return
	}
	
	c.SetCookie("refresh_token", refresToken, 30*24*3600, "/", "", true, true)

	// 3. Return the new access token
	c.JSON(http.StatusOK, gin.H{
		"access_token": accessToken,
		"message":      "Access token refreshed successfully",
	})
}

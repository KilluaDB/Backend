package handlers

import (
	"backend/internal/responses"
	"backend/internal/services"
	"backend/internal/utils"
	_ "log"

	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

type GoogleAuthHandler struct {
	googleAuthService *services.GoogleAuthService
	googleOauthConfig *oauth2.Config
}

func NewGoogleAuthHandler(googleAuthService *services.GoogleAuthService, oauthConfig *oauth2.Config) *GoogleAuthHandler {
	return &GoogleAuthHandler{
		googleAuthService: googleAuthService,
		googleOauthConfig: oauthConfig,
	}
}

func (h *GoogleAuthHandler) Login(c *gin.Context) {
	/*
		You generate a state value but never validate it on callback. Store state in a cookie/session and compare in
		Callback before exchanging the code; reject if it doesn’t match.
	*/
	oauthState, err := utils.GenerateStateOauthCookie()
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to generate state")
	}
	c.SetCookie("oauth_state", oauthState, 3600, "/", "", false, true)

	authURL := h.googleOauthConfig.AuthCodeURL(
		oauthState,
		// oauth2.AccessTypeOffline,
		// oauth2.ApprovalForce,
	)

	c.Redirect(http.StatusTemporaryRedirect, authURL)
}

func (h *GoogleAuthHandler) Callback(c *gin.Context) {
	// Validate state from query parameter against cookie
	queryState := c.Query("state")
	if queryState == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Missing state parameter")
		return
	}

	cookieState, err := c.Cookie("oauth_state")
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Missing state cookie")
		return
	}

	if queryState != cookieState {
		responses.Fail(c, http.StatusForbidden, nil, "State mismatch - possible CSRF attack")
		return
	}

	// Clear the state cookie
	c.SetCookie("oauth_state", "", -1, "/", "", false, true)

	// Get authorization code
	code := c.Query("code")
	if code == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Missing code")
		return
	}

	// Exchange code for token
	token, err := h.googleOauthConfig.Exchange(c.Request.Context(), code)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Token exchange failed")
		return
	}

	// Get user info and create/update user
	accessToken, err := h.googleAuthService.Callback(c.Request.Context(), token)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to login")
		return
	}

	res := gin.H{
		"access_token": accessToken,
	}

	responses.Success(c, http.StatusOK, res, "User Login Successfully!")
}

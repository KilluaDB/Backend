package handlers

import (
	_ "log"
	"my_project/internal/responses"
	"my_project/internal/services"
	"my_project/internal/utils"
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


	authURL := 	h.googleOauthConfig.AuthCodeURL(
		oauthState,
		// oauth2.AccessTypeOffline,
		// oauth2.ApprovalForce,
	)

	c.Redirect(http.StatusTemporaryRedirect, authURL)
}


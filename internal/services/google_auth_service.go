package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"my_project/internal/models"
	"my_project/internal/repositories"
	"my_project/internal/utils"

	"golang.org/x/oauth2"
)

const (
	oauthGoogleUrlAPI = "https://www.googleapis.com/oauth2/v2/userinfo?access_token="
)

type GoogleAuthService struct {
	userRepo    *repositories.UserRepository
	// sessionRepo *repositories.SessionRepository
}

func NewGoogleAuthService(userRepo *repositories.UserRepository) *GoogleAuthService {
	return &GoogleAuthService{
		userRepo:    userRepo,
		// sessionRepo: sessionRepo,
	}
}

func (s *GoogleAuthService) Callback(ctx context.Context, token *oauth2.Token) (string, error) {
	// Create OAuth2 HTTP client with the token
	oauthClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

	// Fetch user info from Google
	response, err := oauthClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get user info: %w", err)
	}
	defer response.Body.Close()

	var googleUser struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %s", err.Error())
	}

	if err := json.Unmarshal(body, &googleUser); err != nil {
		return "", fmt.Errorf("failed to parse user info: %w", err)
	}

	if !googleUser.VerifiedEmail {
		return "", fmt.Errorf("email is not verified by Google")
	}

	user, err := s.userRepo.FindUserByEmail(googleUser.Email)
	if err != nil || user == nil {
		// User doesn't exist, create new one
		newUser := &models.User{
			Email: googleUser.Email,
		}

		if err := s.userRepo.Create(newUser); err != nil {
			return "", fmt.Errorf("failed to create user: %w", err)
		}

		user = newUser
	}

	accessToken, err := utils.GenerateJWT(user.ID, 15*time.Minute, utils.AccessTokenSecret)
	if err != nil {
		return "", fmt.Errorf("failed to generate access token: %w", err)
	}

	return accessToken, nil
}
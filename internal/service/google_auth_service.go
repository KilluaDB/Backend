package service

import (
	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/utils"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)



type GoogleAuthService struct {
	userRepo *repository.UserRepository
}

func NewGoogleAuthService(userRepo *repository.UserRepository) *GoogleAuthService {
	return &GoogleAuthService{
		userRepo: userRepo,
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

	user, err := s.userRepo.FindUserByEmail(ctx, googleUser.Email)
	if err != nil || user == nil {
		// User doesn't exist, create new one
		newUser := &model.User{
			Email: googleUser.Email,
		}

		if err := s.userRepo.Create(ctx, newUser); err != nil {
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

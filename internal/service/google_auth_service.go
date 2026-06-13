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

// GoogleUserInfoClient fetches Google user profile data (mocked in tests).
type GoogleUserInfoClient interface {
	FetchUserInfo(ctx context.Context, accessToken string) (email string, verified bool, err error)
}

type defaultGoogleUserInfoClient struct{}

func (defaultGoogleUserInfoClient) FetchUserInfo(ctx context.Context, accessToken string) (string, bool, error) {
	oauthClient := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", false, fmt.Errorf("failed to create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	response, err := oauthClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("failed to get user info: %w", err)
	}
	defer response.Body.Close()

	var googleUser struct {
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", false, fmt.Errorf("failed to read response: %w", err)
	}
	if err := json.Unmarshal(body, &googleUser); err != nil {
		return "", false, fmt.Errorf("failed to parse user info: %w", err)
	}
	return googleUser.Email, googleUser.VerifiedEmail, nil
}

type GoogleAuthService struct {
	userRepo repository.UserStore
	userInfo GoogleUserInfoClient
}

func NewGoogleAuthService(userRepo repository.UserStore) *GoogleAuthService {
	return &GoogleAuthService{
		userRepo: userRepo,
		userInfo: defaultGoogleUserInfoClient{},
	}
}

// NewGoogleAuthServiceWithClient allows injecting a mock Google userinfo client in tests.
func NewGoogleAuthServiceWithClient(userRepo repository.UserStore, client GoogleUserInfoClient) *GoogleAuthService {
	return &GoogleAuthService{userRepo: userRepo, userInfo: client}
}

func (s *GoogleAuthService) Callback(ctx context.Context, token *oauth2.Token) (string, error) {
	email, verified, err := s.userInfo.FetchUserInfo(ctx, token.AccessToken)
	if err != nil {
		return "", err
	}
	if !verified {
		return "", fmt.Errorf("email is not verified by Google")
	}

	user, err := s.userRepo.FindUserByEmail(ctx, email)
	if err != nil || user == nil {
		newUser := &model.User{Email: email}
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

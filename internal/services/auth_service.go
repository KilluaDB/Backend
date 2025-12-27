package services

import (
	"backend/internal/models"
	"backend/internal/repositories"
	"backend/internal/utils"
	"errors"
	"time"
)

const (
	AccessTokenDuration  = 15 * time.Minute
	RefreshTokenDuration = 30 * 24 * time.Hour // 7 days
)

type AuthService struct {
	userRepo *repositories.UserRepository
}

func NewAuthService(userRepo *repositories.UserRepository) *AuthService {
	return &AuthService{
		userRepo: userRepo,
	}
}

func (s *AuthService) Register(user *models.User) (string, string, error) {
	// 1. Check if user already exists
	existing, _ := s.userRepo.FindUserByEmail(user.Email)
	if existing != nil {
		return "", "", errors.New("user already exists")
	}

	// 2. Hash password before saving
	passwordToHash := user.Password
	if passwordToHash == "" {
		passwordToHash = user.PasswordHash // Fallback if PasswordHash was set directly
	}
	hashedPassword, err := utils.Hash(passwordToHash)
	if err != nil {
		return "", "", err
	}
	user.PasswordHash = string(hashedPassword)
	user.Password = "" // Clear plain password

	// 3. Save user in DB
	if err := s.userRepo.Create(user); err != nil {
		return "", "", err
	}

	// 4. Generate tokens (no database session - tokens are self-contained)
	accessToken, err := utils.GenerateJWT(user.ID, AccessTokenDuration, utils.AccessTokenSecret)
	if err != nil {
		return "", "", err
	}

	refreshToken, err := utils.GenerateJWT(user.ID, RefreshTokenDuration, utils.RefreshTokenSecret)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (s *AuthService) Login(email, password string) (string, string, error) {
	user, err := s.userRepo.FindUserByEmail(email)
	if err != nil {
		return "", "", errors.New("user not found")
	}

	// Check if user is nil (user doesn't exist)
	if user == nil {
		return "", "", errors.New("user not found")
	}

	if err := utils.VerifyPassword(user.PasswordHash, password); err != nil {
		return "", "", errors.New("invalid password")
	}

	// Generate access + refresh tokens (no database session - tokens are self-contained)
	accessToken, err := utils.GenerateJWT(user.ID, AccessTokenDuration, utils.AccessTokenSecret)
	if err != nil {
		return "", "", err
	}

	refreshToken, err := utils.GenerateJWT(user.ID, RefreshTokenDuration, utils.RefreshTokenSecret)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

// Refresh validates the refresh token from cookie and issues a new access token.
// Since tokens are stored in HttpOnly cookies (not database), validation is done via JWT signature only.
func (s *AuthService) Refresh(refreshToken string) (string, string, error) {
	// 1. Validate refresh token signature and expiration
	claims, err := utils.VerifyJWT(refreshToken, utils.RefreshTokenSecret)
	if err != nil {
		return "", "", errors.New("invalid or expired refresh token")
	}

	// 2. Verify user still exists
	user, err := s.userRepo.FindUserByID(claims.UserID)
	if err != nil || user == nil {
		return "", "", errors.New("user not found")
	}

	// 3. Generate new token pair (token rotation for security)
	newAccessToken, err := utils.GenerateJWT(claims.UserID, AccessTokenDuration, utils.AccessTokenSecret)
	if err != nil {
		return "", "", errors.New("could not generate new access token")
	}

	newRefreshToken, err := utils.GenerateJWT(claims.UserID, RefreshTokenDuration, utils.RefreshTokenSecret)
	if err != nil {
		return "", "", errors.New("could not generate new refresh token")
	}

	return newAccessToken, newRefreshToken, nil
}

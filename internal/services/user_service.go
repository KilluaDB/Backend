package services

import (
	"errors"
	"time"

	"my_project/internal/models"
	"my_project/internal/repositories"
	"my_project/internal/utils"

	"github.com/google/uuid"
)

type UserService struct {
	userRepo    *repositories.UserRepository
	sessionRepo *repositories.SessionRepository
}

func NewUserService(userRepo *repositories.UserRepository, sessionRepo *repositories.SessionRepository) *UserService {
	return &UserService{
		userRepo:    userRepo,
		sessionRepo: sessionRepo,
	}
}

func (s *UserService) Register(user *models.User) (string, string, uuid.UUID, error) {
	// 1. Check if it already exists
	existing, _ := s.userRepo.FindUserByEmail(user.Email)
	if existing != nil {
		return "", "", uuid.Nil, errors.New("user already exists")
	}

	// 2. Hash password before saving
	hashedPassword, err := utils.Hash(user.Password)
	if err != nil {
		return "", "", uuid.Nil, err
	}
	user.Password = string(hashedPassword)

	// 3. Save user in DB
	if err := s.userRepo.Create(user); err != nil {
		return "", "", uuid.Nil, err
	}

	// 4. Generate tokens
	accessToken, err := utils.GenerateJWT(user.ID, 15*time.Minute, utils.AccessTokenSecret)
	if err != nil {
		return "", "", uuid.Nil, err
	}

	refreshToken, err := utils.GenerateJWT(user.ID, 24*time.Hour, utils.RefreshTokenSecret)
	if err != nil {
		return "", "", uuid.Nil, err
	}

	// 5. Create a session for the refresh token
	session := &models.Session{
		UserID:       user.ID,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}

	if err := s.sessionRepo.Create(session); err != nil {
		return "", "", uuid.Nil, err
	}

	return accessToken, refreshToken, session.ID, nil
}

func (s *UserService) Login(email, password string) (string, string, uuid.UUID, error) {
	user, err := s.userRepo.FindUserByEmail(email)
	if err != nil {
		return "", "", uuid.Nil, errors.New("user not found")
	}

	if err := utils.VerifyPassword(user.Password, password); err != nil {
		return "", "", uuid.Nil, errors.New("invalid password")
	}

	// Generate access + refresh tokens
	accessToken, err := utils.GenerateJWT(user.ID, 15*time.Minute, utils.AccessTokenSecret)
	if err != nil {
		return "", "", uuid.Nil, err
	}

	refreshToken, err := utils.GenerateJWT(user.ID, 24*time.Hour, utils.RefreshTokenSecret)
	if err != nil {
		return "", "", uuid.Nil, err
	}

	// Create session
	session := &models.Session{
		UserID:       user.ID,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}

	if err := s.sessionRepo.Create(session); err != nil {
		return "", "", uuid.Nil, err
	}

	return accessToken, refreshToken, session.ID, nil
}

func (s *UserService) Logout(refreshToken string) error {
	return s.sessionRepo.Revoke(refreshToken)
}

func (s *UserService) Refresh(refreshToken string) (string, error) {
	// 1. Validate refresh token in database
	session, err := s.sessionRepo.FindByToken(refreshToken)
	if err != nil {
		return "", errors.New("refresh token not found")
	}

	if session.IsRevoked {
		return "", errors.New("refresh token revoked")
	}

	if time.Now().After(session.ExpiresAt) {
		return "", errors.New("refresh token expired")
	}

	// 2. Validate refresh token signature
	claims, err := utils.VerifyJWT(refreshToken, utils.RefreshTokenSecret)
	if err != nil {
		return "", errors.New("invalid refresh token")
	}

	// 3. Generate new access token
	accessToken, err := utils.GenerateJWT(claims.UserID, 15*time.Minute, utils.AccessTokenSecret)
	if err != nil {
		return "", errors.New("could not generate new access token")
	}

	return accessToken, nil
}

func (s *UserService) LogoutByUserID(userID uint) error {
	return s.userRepo.DeleteRefreshTokensByUserID(userID)
}

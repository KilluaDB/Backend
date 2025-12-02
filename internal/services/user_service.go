package services

import (
	"context"
	"errors"
	"log"
	"net/mail"
	"time"

	"my_project/internal/models"
	"my_project/internal/repositories"
	"my_project/internal/utils"

	"github.com/google/uuid"
)

type UserService struct {
	userRepo    *repositories.UserRepository
	redisRepo   *repositories.RedisRepository
}

func NewUserService(userRepo *repositories.UserRepository, redisRepo *repositories.RedisRepository) *UserService {
	return &UserService{
		userRepo:    userRepo,
		redisRepo:   redisRepo,
	}
}

func (s *UserService) Register(email, password string, ctx context.Context) (string, string, error) {
	// 1. Check the email format
	_, err := mail.ParseAddress(email)
	if err != nil {
		return "", "", err
	}
	
	// 2. Check if it already exists
	existing, err := s.userRepo.FindUserByEmail(email)

	if err != nil {
		return "", "", err
	}

	if existing != nil {
		return "", "", errors.New("user already exists")
	}

	// 3. Hash password before saving
	hashedPassword, err := utils.Hash(password)
	if err != nil {
		return "", "", err
	}

	// 4. Create and save user in DB
	user := &models.User{
		Email:        email,
		PasswordHash: string(hashedPassword),
	}
	if err := s.userRepo.Create(user); err != nil {
		return "", "", err
	}

	// 5. Generate tokens
	accessToken, refreshToken, jti, err := utils.GenerateTokens(user.ID)
	if err != nil {
		return "", "", err
	}

	//TODO: jti instead of refreshToken
	if err := s.redisRepo.StoreSession(ctx, jti, user.ID.String()); err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (s *UserService) Login(email, password string, ctx context.Context) (string, string, error) {
	user, err := s.userRepo.FindUserByEmail(email)
	if err != nil {
		return "", "", errors.New("user not found")
	}
	
	if err := utils.VerifyPassword(user.PasswordHash, password); err != nil {
		return "", "", errors.New("invalid password")
	}

	// Generate access + refresh tokens
	accessToken, refreshToken, jti, err := utils.GenerateTokens(user.ID)
	if err != nil {
		return "", "", err
	}

	//TODO: jti instead of refreshToken
	if err := s.redisRepo.StoreSession(ctx, jti, user.ID.String()); err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (s *UserService) Logout(ctx context.Context, refreshToken string) error {
	claims, err := utils.VerifyJWT(refreshToken, utils.RefreshTokenSecret)
	if err != nil {
		log.Print(err)
		return errors.New("refresh token not found")
	}
	
	_ = s.redisRepo.Blacklist(ctx, claims.ID)
	_ = s.redisRepo.DeleteSession(ctx, claims.ID)

	return nil
}

func (s *UserService) Refresh(ctx context.Context, refreshToken string) (string, string, error) {
	claims, err := utils.VerifyJWT(refreshToken, utils.RefreshTokenSecret)
	if err != nil {
		return "", "", errors.New("refresh token not found")
	}

	blocked, err := s.redisRepo.IsBlacklisted(ctx, claims.ID)
	if err != nil {
		return "", "", err
	}
	if blocked {
		return "", "", errors.New("token revoked")
	}
 
	if time.Now().After(claims.ExpiresAt.Time) {
		return "", "", errors.New("refresh token expired")
	}

	// 3. Generate new access token
	sub := claims.RegisteredClaims.Subject
	userId, err := uuid.Parse(sub)
	if err != nil {
    	return "", "", errors.New("invalid UUID in JWT sub claim")
	}

	accessToken, refreshToken, newJTI, err := utils.GenerateTokens(userId)
	if err != nil {
		return "", "", err
	}

	err = s.redisRepo.Blacklist(ctx, claims.ID)
	if err != nil {
		return "", "", err
	}

	err = s.redisRepo.StoreSession(ctx, newJTI, userId.String())
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}
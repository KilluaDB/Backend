package service

import (
	"backend/internal/config"
	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/utils"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Sentinel errors for auth so handlers can return proper HTTP status and messages.
var (
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrInvalidPassword   = errors.New("invalid password")
)

const (
	AccessTokenDuration  = 15 * time.Minute
	RefreshTokenDuration = 30 * 24 * time.Hour // 30 days
)

type refreshTokenStorer interface {
	Set(ctx context.Context, token string, userID uuid.UUID) error
	Get(ctx context.Context, token string) (uuid.UUID, error)
	Delete(ctx context.Context, token string) error
}

type AuthService struct {
	userRepo     repository.UserStore
	refreshStore refreshTokenStorer
}

func NewAuthService(userRepo repository.UserStore, refreshStore refreshTokenStorer) *AuthService {
	return &AuthService{
		userRepo:     userRepo,
		refreshStore: refreshStore,
	}
}

func (s *AuthService) Register(ctx context.Context, user *model.User) (userID uuid.UUID, accessToken, refreshToken string, err error) {
	// 1. Check if active user already exists
	existing, _ := s.userRepo.FindUserByEmail(ctx, user.Email)
	if existing != nil {
		return uuid.Nil, "", "", ErrUserAlreadyExists
	}

	// 2. If a soft-deleted user exists with this email, hard-delete so we can create a new user
	if deleted, _ := s.userRepo.FindUserByEmailIncludingDeleted(ctx, user.Email); deleted != nil && deleted.DeletedAt != nil {
		if delErr := s.userRepo.HardDeleteSoftDeletedByEmail(ctx, user.Email); delErr != nil {
			return uuid.Nil, "", "", delErr
		}
	}

	// 3. Hash password before saving
	passwordToHash := user.Password
	if passwordToHash == "" {
		passwordToHash = user.PasswordHash // Fallback if PasswordHash was set directly
	}
	hashedPassword, hashErr := utils.Hash(passwordToHash)
	if hashErr != nil {
		return uuid.Nil, "", "", hashErr
	}
	user.PasswordHash = string(hashedPassword)
	user.Password = "" // Clear plain password

	// 4. Save user in DB
	if createErr := s.userRepo.Create(ctx, user); createErr != nil {
		var pgErr *pgconn.PgError
		if errors.As(createErr, &pgErr) && pgErr.Code == "23505" {
			return uuid.Nil, "", "", ErrUserAlreadyExists
		}
		return uuid.Nil, "", "", createErr
	}

	// 5. Generate tokens (no database session - tokens are self-contained)
	accessToken, jwtErr := utils.GenerateJWT(user.ID, AccessTokenDuration, utils.AccessTokenSecret)
	if jwtErr != nil {
		return uuid.Nil, "", "", jwtErr
	}

	refreshToken, jwtErr = utils.GenerateJWT(user.ID, RefreshTokenDuration, utils.RefreshTokenSecret)
	if jwtErr != nil {
		return uuid.Nil, "", "", jwtErr
	}

	if s.refreshStore != nil {
		if setErr := s.refreshStore.Set(ctx, refreshToken, user.ID); setErr != nil {
			return uuid.Nil, "", "", setErr
		}
	}

	return user.ID, accessToken, refreshToken, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (userID uuid.UUID, accessToken, refreshToken string, err error) {
	user, findErr := s.userRepo.FindUserByEmail(ctx, email)
	if findErr != nil {
		return uuid.Nil, "", "", ErrUserNotFound
	}

	if user == nil {
		return uuid.Nil, "", "", ErrUserNotFound
	}

	if verifyErr := utils.VerifyPassword(user.PasswordHash, password); verifyErr != nil {
		return uuid.Nil, "", "", ErrInvalidPassword
	}

	accessToken, jwtErr := utils.GenerateJWT(user.ID, AccessTokenDuration, utils.AccessTokenSecret)
	if jwtErr != nil {
		return uuid.Nil, "", "", jwtErr
	}

	refreshToken, jwtErr = utils.GenerateJWT(user.ID, RefreshTokenDuration, utils.RefreshTokenSecret)
	if jwtErr != nil {
		return uuid.Nil, "", "", jwtErr
	}

	if s.refreshStore != nil {
		if setErr := s.refreshStore.Set(ctx, refreshToken, user.ID); setErr != nil {
			return uuid.Nil, "", "", setErr
		}
	}

	return user.ID, accessToken, refreshToken, nil
}

// Logout revokes the refresh token by removing it from the store.
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	if s.refreshStore == nil {
		return nil
	}
	return s.refreshStore.Delete(ctx, refreshToken)
}

// Refresh validates the refresh token (JWT + Redis), then rotates it: issues new tokens and stores new refresh token in Redis.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (userID uuid.UUID, accessToken, newRefreshToken string, err error) {

	// 1. Check token exists in Redis (persisted and not revoked/expired)
	if s.refreshStore != nil {
		storedID, getErr := s.refreshStore.Get(ctx, refreshToken)
		if getErr != nil {
			if errors.Is(getErr, config.ErrRefreshTokenNotFound) {
				return uuid.Nil, "", "", errors.New("invalid or expired refresh token")
			}
			return uuid.Nil, "", "", getErr
		}
		_ = storedID // used implicitly by JWT validation matching same user
	}

	// 2. Validate JWT signature and expiration
	claims, err := utils.VerifyJWT(refreshToken, utils.RefreshTokenSecret)
	if err != nil {
		return uuid.Nil, "", "", errors.New("invalid or expired refresh token")
	}

	// 3. Verify user still exists
	user, err := s.userRepo.FindUserByID(ctx, claims.UserID)
	if err != nil || user == nil {
		return uuid.Nil, "", "", errors.New("user not found")
	}

	// 4. Revoke old refresh token (rotation)
	if s.refreshStore != nil {
		_ = s.refreshStore.Delete(ctx, refreshToken)
	}

	// 5. Generate new token pair
	newAccessToken, err := utils.GenerateJWT(claims.UserID, AccessTokenDuration, utils.AccessTokenSecret)
	if err != nil {
		return uuid.Nil, "", "", errors.New("could not generate new access token")
	}

	newRefreshToken, err = utils.GenerateJWT(claims.UserID, RefreshTokenDuration, utils.RefreshTokenSecret)
	if err != nil {
		return uuid.Nil, "", "", errors.New("could not generate new refresh token")
	}

	// 6. Persist new refresh token in Redis
	if s.refreshStore != nil {
		if setErr := s.refreshStore.Set(ctx, newRefreshToken, claims.UserID); setErr != nil {
			return uuid.Nil, "", "", setErr
		}
	}

	return claims.UserID, newAccessToken, newRefreshToken, nil
}

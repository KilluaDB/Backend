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
	// Use Password field from JSON input, hash it, and store in PasswordHash
	passwordToHash := user.Password
	if passwordToHash == "" {
		passwordToHash = user.PasswordHash // Fallback if PasswordHash was set directly
	}
	hashedPassword, err := utils.Hash(passwordToHash)
	if err != nil {
		return "", "", uuid.Nil, err
	}
	user.PasswordHash = string(hashedPassword)
	user.Password = "" // Clear plain password

	// 3. Policy: First user becomes admin
	userCount, err := s.userRepo.CountUsers()
	if err != nil {
		return "", "", uuid.Nil, err
	}
	if userCount == 0 {
		user.Role = "admin"
	} else if user.Role == "" {
		user.Role = "user"
	}

	// 4. Save user in DB
	if err := s.userRepo.Create(user); err != nil {
		return "", "", uuid.Nil, err
	}

	// 5. Generate tokens
	accessToken, err := utils.GenerateJWT(user.ID, 15*time.Minute, utils.AccessTokenSecret)
	if err != nil {
		return "", "", uuid.Nil, err
	}

	refreshToken, err := utils.GenerateJWT(user.ID, 24*time.Hour, utils.RefreshTokenSecret)
	if err != nil {
		return "", "", uuid.Nil, err
	}

	// 6. Create a session for the refresh token
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

	// Check if user is nil (user doesn't exist)
	if user == nil {
		return "", "", uuid.Nil, errors.New("user not found")
	}

	if err := utils.VerifyPassword(user.PasswordHash, password); err != nil {
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

func (s *UserService) LogoutByUserID(userID uuid.UUID) error {
	return s.userRepo.DeleteRefreshTokensByUserID(userID)
}

// GetUser retrieves a user by ID
func (s *UserService) GetUser(userID uuid.UUID) (*models.User, error) {
	user, err := s.userRepo.FindUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}
	// Clear sensitive data before returning
	user.PasswordHash = ""
	return user, nil
}

// UpdateUserRequest represents the request body for updating a user
type UpdateUserRequest struct {
	Email *string `json:"email,omitempty"`
	Role  *string `json:"role,omitempty"`
}

// UpdateUser updates a user's information
// authenticatedUserID is the ID of the user making the request (for policy checks)
func (s *UserService) UpdateUser(userID uuid.UUID, authenticatedUserID uuid.UUID, req UpdateUserRequest) (*models.User, error) {
	// Get existing user
	user, err := s.userRepo.FindUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}

	// Get authenticated user to check their role
	authenticatedUser, err := s.userRepo.FindUserByID(authenticatedUserID)
	if err != nil {
		return nil, err
	}
	if authenticatedUser == nil {
		return nil, errors.New("authenticated user not found")
	}

	// Policy: Only admins can promote/demote others (change role)
	if req.Role != nil && *req.Role != user.Role {
		if authenticatedUser.Role != "admin" {
			return nil, errors.New("only admins can change user roles")
		}

		// Policy: Admin cannot demote themselves
		if authenticatedUserID == userID && *req.Role != "admin" {
			return nil, errors.New("admin cannot demote themselves")
		}
	}

	// Update fields if provided
	if req.Email != nil {
		user.Email = *req.Email
	}
	if req.Role != nil {
		user.Role = *req.Role
	}

	// Save updated user
	if err := s.userRepo.Update(user); err != nil {
		return nil, err
	}

	// Clear sensitive data before returning
	user.PasswordHash = ""
	return user, nil
}

// DeleteUser deletes a user by ID
// authenticatedUserID is the ID of the user making the request (for policy checks)
func (s *UserService) DeleteUser(userID uuid.UUID, authenticatedUserID uuid.UUID) error {
	// Check if user exists
	user, err := s.userRepo.FindUserByID(userID)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("user not found")
	}
	// Get authenticated user to check their role
	authenticatedUser, err := s.userRepo.FindUserByID(authenticatedUserID)
	if err != nil {
		return err
	}
	if authenticatedUser == nil {
		return errors.New("authenticated user not found")
	}
	if err != nil {
		return err
	}
	// Policy: Admins cannot delete admins
	if user.Role == "admin" && authenticatedUser.Role == "admin" && user.ID != authenticatedUser.ID {
		return errors.New("admins cannot delete other admins")
	}
	// Policy: Cannot delete last admin
	if user.Role == "admin" {
		adminCount, err := s.userRepo.CountAdmins()
		if err != nil {
			return err
		}
		if adminCount <= 1 {
			return errors.New("cannot delete the last admin")
		}
	}

	// Delete user (CASCADE will handle related records)
	return s.userRepo.Delete(userID)
}

// GetAllUsers retrieves all users
func (s *UserService) GetAllUsers() ([]models.User, error) {
	users, err := s.userRepo.FindAll()
	if err != nil {
		return nil, err
	}

	// Clear sensitive data before returning
	for i := range users {
		users[i].PasswordHash = ""
	}

	return users, nil
}

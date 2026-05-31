package services

import (
	"backend/internal/models"
	"backend/internal/repositories"
	"context"
	"errors"

	// "time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserService struct {
	userRepo    *repositories.UserRepository
	projectRepo *repositories.PostgresProjectRepository
	pool        *pgxpool.Pool
}

func NewUserService(userRepo *repositories.UserRepository, projectRepo *repositories.PostgresProjectRepository, pool *pgxpool.Pool) *UserService {
	return &UserService{
		userRepo:    userRepo,
		projectRepo: projectRepo,
		pool:        pool,
	}
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
	// if err != nil {
	// 	return err
	// }
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

	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := s.projectRepo.DeleteByUserIDTx(ctx, tx, userID); err != nil {
		return err
	}

	if err := s.userRepo.DeleteTx(ctx, tx, userID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	return nil
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

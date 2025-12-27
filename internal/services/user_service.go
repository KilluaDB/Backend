package services

import (
	"errors"
	// "time"

	"my_project/internal/models"
	"my_project/internal/repositories"
	// "my_project/internal/utils"

	"github.com/google/uuid"
)

type UserService struct {
	userRepo    *repositories.UserRepository
}

func NewUserService(userRepo *repositories.UserRepository) *UserService {
	return &UserService{
		userRepo:    userRepo,
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

package repositories

import (
	"errors"
	"my_project/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(user *models.User) error {
	return r.db.Debug().Create(user).Error
}

func (r *UserRepository) FindUserByID(id uuid.UUID) (*models.User, error) {
	var user models.User
	err := r.db.Debug().Where("id = ?", id).First(&user).Error
	if err != nil {
		return &models.User{}, err
	}
	return &user, nil
}

func (r *UserRepository) FindUserByEmail(email string) (*models.User, error) {
	var user models.User
	err := r.db.Debug().Where("email = ?", email).First(&user).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return &models.User{}, err
	}
	return &user, nil
}

func (r *UserRepository) FindUserByName(username string) (*models.User, error) {
	var user models.User
	if err := r.db.Debug().Where("name = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// func (r *UserRepository) CreateRefreshToken(token *models.RefreshToken) error {
// 	return r.db.Debug().Create(token).Error
// }

func (r *UserRepository) DeleteRefreshTokensByUserID(userID uint) error {
	return r.db.Where("user_id = ?", userID).Delete(&models.Session{}).Error
}

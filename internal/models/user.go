package models

import (
	"html"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	Name          string    `gorm:"size:255;not null;unique" json:"name" validate:"required,min=2,max=100"`
	Email         string    `gorm:"size:100;not null;unique" json:"email" validate:"required,email"`
	Password      string    `gorm:"size:100;not null" json:"password" validate:"required,min=6"`
	CreatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
	LastLoginAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"last_login_at"`
	RefreshToken  string
	RefreshExpiry time.Time
}

func (u *User) BeforeCreate(tx *gorm.DB) (err error) {
	// Ensure UUID exists
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return
}

func (u *User) Prepare() {
	u.Name = html.EscapeString(strings.TrimSpace(u.Name))
	u.Email = html.EscapeString(strings.TrimSpace(u.Email))
}

// func (u *User) BeforeSave(tx *gorm.DB) error {
// 	if !strings.HasPrefix(u.Password, "$2a$") && !strings.HasPrefix(u.Password, "$2b$") {
// 		hashedPassword, err := Hash(u.Password)
// 		if err != nil {
// 			return err
// 		}
// 		u.Password = string(hashedPassword)
// 	}

// 	if err := u.Validate(); err != nil {
// 		return fmt.Errorf("validation failed: %w", err)
// 	}

// 	return nil
// }

// var validate = validator.New()

// func (u *User) Validate() error {
// 	return validate.Struct(u)
// }

// func (u *User) SaveUser(db *gorm.DB) (*User, error) {
// 	err := db.Debug().Create(&u).Error
// 	if err != nil {
// 		return &User{}, err
// 	}
// 	return u, nil
// }

// func (u *User) FindAllUsers(db *gorm.DB) (*[]User, error) {
// 	users := []User{}
// 	err := db.Debug().Model(&User{}).Limit(100).Find(&users).Error
// 	if err != nil {
// 		return &[]User{}, err
// 	}
// 	return &users, nil
// }

// func (u *User) FindUserByID(db *gorm.DB, id uuid.UUID) (*User, error) {
// 	err := db.Debug().First(u, "id = ?", id).Error
// 	// err = db.Debug().Model(User{}).Where("id = ?", id).Take(u).Error
// 	if errors.Is(err, gorm.ErrRecordNotFound) {
// 		return nil, errors.New("user not found")
// 	}
// 	if err != nil {
// 		return &User{}, err
// 	}
// 	return u, nil
// }

// func (u *User) UpdateUser(db *gorm.DB, id uuid.UUID) (*User, error) {
// 	err := u.BeforeSave(db)
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	db = db.Debug().Model(&User{}).Where("id = ?", id).Take(&User{}).UpdateColumns(
// 		map[string]interface{}{
// 			"password":   u.Password,
// 			"name":       u.Name,
// 			"email":      u.Email,
// 			"updated_at": time.Now(),
// 		},
// 	)

// 	if db.Error != nil {
// 		return &User{}, db.Error
// 	}

// 	err = db.Debug().Model(&User{}).Where("id = ?", id).Take(u).Error
// 	if err != nil {
// 		return &User{}, err
// 	}

// 	return u, nil
// }

// func (u *User) DeleteUser(db *gorm.DB, id uuid.UUID) (int64, error) {
// 	db = db.Debug().Model(&User{}).Where("id = ?", id).Take(&User{}).Delete(&User{})

// 	if db.Error != nil {
// 		return 0, db.Error
// 	}

// 	return db.RowsAffected, nil
// }

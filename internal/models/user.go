package models

import (
	"html"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User matches users table defined in script.sql
// Columns: id, name (nullable), email (NOT NULL UNIQUE), password_hash, created_at, last_login_at
type User struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	Email        string     `gorm:"type:text;not null;unique" json:"email"`
	PasswordHash string     `gorm:"type:text;not null" json:"password_hash"`
	CreatedAt    time.Time  `gorm:"type:timestamptz;autoCreateTime" json:"created_at"`
	LastLoginAt  *time.Time `gorm:"type:timestamptz" json:"last_login_at,omitempty"`
}

func (u *User) BeforeCreate(tx *gorm.DB) (err error) {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return
}

func (u *User) Prepare() {
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

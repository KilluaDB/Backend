package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Session struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID       uuid.UUID `gorm:"type:uuid;not null" json:"user_id"`
	RefreshToken string    `gorm:"type:text;not null" json:"refresh_token"`
	IsRevoked    bool      `gorm:"default:false" json:"is_revoked"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`

	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

func (s *Session) BeforeCreate(tx *gorm.DB) (err error) {
	// Ensure UUID exists
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return
}

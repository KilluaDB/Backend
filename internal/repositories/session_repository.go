package repositories

import (
	"my_project/internal/models"
	"time"

	"gorm.io/gorm"
)

type SessionRepository struct {
	db *gorm.DB
}

func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) Create(session *models.Session) error {
	return r.db.Create(session).Error
}

func (r *SessionRepository) FindByToken(token string) (*models.Session, error) {
	var s models.Session
	if err := r.db.Where("refresh_token = ?", token).First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SessionRepository) Revoke(token string) error {
	return r.db.Model(&models.Session{}).Where("refresh_token = ?", token).Update("is_revoked", true).Error
}

func (r *SessionRepository) DeleteExpired() error {
	return r.db.Where("expires_at < ?", time.Now()).Delete(&models.Session{}).Error
}

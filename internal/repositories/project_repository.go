package repositories

import (
	"backend/internal/models"
	"context"

	"github.com/google/uuid"
)

// ProjectRepository defines the operations for managing projects in the
// metadata store.
type ProjectRepository interface {
	Create(ctx context.Context, project *models.Project) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Project, error)
	GetByIDAndUserID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.Project, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) ([]models.Project, error)
	UpdateRuntimeStatus(ctx context.Context, id uuid.UUID, status string) error
	Update(ctx context.Context, project *models.Project) error
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteByIDAndUserID(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
}

package repository

import (
	"backend/internal/model"
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UserStore abstracts user persistence for services and middleware tests.
type UserStore interface {
	Create(ctx context.Context, user *model.User) error
	FindUserByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	FindUserByEmail(ctx context.Context, email string) (*model.User, error)
	FindUserByEmailIncludingDeleted(ctx context.Context, email string) (*model.User, error)
	HardDeleteSoftDeletedByEmail(ctx context.Context, email string) error
	Update(ctx context.Context, user *model.User) error
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	FindAll(ctx context.Context) ([]model.User, error)
	CountUsers(ctx context.Context) (int, error)
	CountAdmins(ctx context.Context) (int, error)
}

// ProjectStore abstracts project persistence for services and middleware tests.
type ProjectStore interface {
	Create(ctx context.Context, project *model.Project) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Project, error)
	GetByIDAndUserID(ctx context.Context, id, userID uuid.UUID) (*model.Project, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) ([]model.Project, error)
	UpdateRuntimeStatus(ctx context.Context, id uuid.UUID, status string) error
	Update(ctx context.Context, project *model.Project) error
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteByIDAndUserID(ctx context.Context, id, userID uuid.UUID) error
	DeleteByUserIDTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error
}

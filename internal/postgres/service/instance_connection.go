package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InstanceConnectionService is the subset of internal/services.InstanceConnectionService
// needed by Postgres DB-facing services. Using an interface avoids circular dependencies.
// GetPool / GetPoolWithMeta return shared cached pools; callers must not Close them.
type InstanceConnectionService interface {
	GetPool(ctx context.Context, userID, projectID uuid.UUID) (*pgxpool.Pool, error)
	GetPoolWithMeta(ctx context.Context, userID, projectID uuid.UUID) (*pgxpool.Pool, uuid.UUID, error)
	GetInstanceID(ctx context.Context, userID, projectID uuid.UUID) (uuid.UUID, error)
}


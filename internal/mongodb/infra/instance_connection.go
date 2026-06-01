package infra

import (
	"context"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// InstanceConnectionService provides access to a project database.
type InstanceConnectionService interface {
	GetDatabase(ctx context.Context, userID, projectID uuid.UUID) (*mongo.Database, error)
}

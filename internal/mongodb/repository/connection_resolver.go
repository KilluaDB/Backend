package repository

import (
	"backend/internal/repositories"
	"backend/internal/utils"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/mongo"
)

// ConnectionResolver resolves a MongoDB client and database for a project using the meta store.
type ConnectionResolver struct {
	metaPool *pgxpool.Pool
}

// NewConnectionResolver creates a resolver that uses the given meta pool to look up instance and credentials.
func NewConnectionResolver(metaPool *pgxpool.Pool) *ConnectionResolver {
	return &ConnectionResolver{metaPool: metaPool}
}

// GetClient resolves the running instance for a project and opens a MongoDB client to the project's "app" database.
// Caller must call client.Disconnect(ctx) when done.
func (r *ConnectionResolver) GetClient(ctx context.Context, projectID string) (*mongo.Client, *mongo.Database, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid project ID: %w", err)
	}

	instRepo := repositories.NewDatabaseInstanceRepository(r.metaPool)
	credRepo := repositories.NewDatabaseCredentialRepository(r.metaPool)

	inst, err := instRepo.GetConnectableByProjectID(projectUUID)
	if err != nil {
		return nil, nil, err
	}
	if inst == nil || inst.Host == nil || inst.Port == nil {
		return nil, nil, errors.New("no connectable MongoDB instance for project")
	}

	cred, err := credRepo.GetLatestByInstanceID(inst.ID)
	if err != nil {
		return nil, nil, err
	}
	if cred == nil {
		return nil, nil, errors.New("no credentials configured for MongoDB instance")
	}

	password, err := utils.DecryptString(cred.PasswordEncrypted)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt database credentials: %w", err)
	}

	client, err := ConnectToMongoProject(*inst.Host, *inst.Port, cred.Username, password, "app")
	if err != nil {
		return nil, nil, err
	}

	return client, client.Database("app"), nil
}

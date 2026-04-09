package services

import (
	"backend/internal/database"
	"backend/internal/models"
	"backend/internal/repositories"
	"backend/internal/utils"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors for instance/connection so callers can return proper HTTP status.
var (
	ErrProjectNotAccessible = errors.New("project not found or not accessible")
	ErrNoRunningInstance    = errors.New("no running database instance for this project")
)

// InstanceConnectionService resolves project, connectable instance, credentials,
// and optional healing; returns a pgxpool for project database connections.
type InstanceConnectionService struct {
	projectRepo  repositories.ProjectRepository
	instanceRepo *repositories.DatabaseInstanceRepository
	credRepo     *repositories.DatabaseCredentialRepository
	provisioner  *OperatorProvisioner

	pgPoolMu sync.Mutex
	pgPools  map[poolCacheKey]*pgxpool.Pool
}

type poolCacheKey struct {
	userID    uuid.UUID
	projectID uuid.UUID
}

// NewInstanceConnectionService creates the central connection service.
func NewInstanceConnectionService(
	projectRepo repositories.ProjectRepository,
	instanceRepo *repositories.DatabaseInstanceRepository,
	credRepo *repositories.DatabaseCredentialRepository,
	provisioner *OperatorProvisioner,
) *InstanceConnectionService {
	return &InstanceConnectionService{
		projectRepo:  projectRepo,
		instanceRepo: instanceRepo,
		credRepo:     credRepo,
		provisioner:  provisioner,
		pgPools:      make(map[poolCacheKey]*pgxpool.Pool),
	}
}

// connectionParams holds resolved instance connection parameters.
type connectionParams struct {
	host       string
	port       int
	username   string
	password   string
	instanceID uuid.UUID
}

// getConnectionParams validates project ownership, resolves connectable instance
// (with optional heal when missing), fetches credentials, and returns params or error.
func (s *InstanceConnectionService) getConnectionParams(ctx context.Context, userID, projectID uuid.UUID) (*connectionParams, error) {
	project, err := s.projectRepo.GetByIDAndUserID(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, ErrProjectNotAccessible
	}

	inst, err := s.instanceRepo.GetConnectableByProjectID(projectID)
	if err != nil {
		return nil, err
	}

	// Heal when no connectable instance: discover from K8s and update instance/creds
	if inst == nil && s.provisioner != nil {
		inst2, _ := s.instanceRepo.GetByProjectID(projectID)
		dbType := project.DBType
		validDBType := dbType == "postgresql" || dbType == "mongodb"
		if inst2 != nil && validDBType {
			needHeal := (inst2.Host == nil || *inst2.Host == "") || inst2.Port == nil
			if !needHeal {
				inst = inst2
			} else {
				resourceRef := s.provisioner.ResourceRefForProject(projectID, dbType)
				healCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				result, healErr := s.provisioner.GetConnectionByResourceRef(healCtx, resourceRef, dbType)
				cancel()
				if healErr == nil {
					_ = s.instanceRepo.UpdateConnectionInfo(inst2.ID, result.Host, result.Port)
					encrypted, encErr := utils.EncryptString(result.Password)
					if encErr == nil {
						c := &models.DatabaseCredential{DBInstanceID: inst2.ID, Username: "admin", PasswordEncrypted: encrypted}
						_ = s.credRepo.Upsert(c)
					}
					inst, err = s.instanceRepo.GetConnectableByProjectID(projectID)
					if err != nil {
						return nil, err
					}
				}
			}
		}
	}

	if inst == nil {
		return nil, ErrNoRunningInstance
	}

	cred, err := s.credRepo.GetLatestByInstanceID(inst.ID)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, errors.New("no credentials configured for this database instance")
	}

	if inst.Host == nil || *inst.Host == "" {
		return nil, errors.New("database instance host not configured")
	}
	if inst.Port == nil {
		return nil, errors.New("database instance port not configured")
	}

	password, err := utils.DecryptString(cred.PasswordEncrypted)
	if err != nil {
		return nil, errors.New("failed to decrypt database credentials")
	}

	return &connectionParams{
		host:       *inst.Host,
		port:       *inst.Port,
		username:   cred.Username,
		password:   password,
		instanceID: inst.ID,
	}, nil
}

// GetInstanceID returns the database instance ID for the project's connectable instance.
// This works for both Postgres and MongoDB projects (the metadata lookup is shared).
func (s *InstanceConnectionService) GetInstanceID(ctx context.Context, userID, projectID uuid.UUID) (uuid.UUID, error) {
	params, err := s.getConnectionParams(ctx, userID, projectID)
	if err != nil {
		return uuid.Nil, err
	}
	return params.instanceID, nil
}

// connectPostgresPoolForParams dials the project database, pings, and optionally heals on auth failure.
// The returned pool is not yet registered in the cache.
func (s *InstanceConnectionService) connectPostgresPoolForParams(ctx context.Context, userID, projectID uuid.UUID, params *connectionParams) (*pgxpool.Pool, error) {
	pool, err := database.ConnectToPostgresProject(params.host, params.port, params.username, params.password, "app")
	if err != nil {
		return nil, err
	}

	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	pingErr := pool.Ping(pingCtx)
	pingCancel()
	if pingErr != nil && s.provisioner != nil {
		var pgErr *pgconn.PgError
		shouldHeal := true
		if errors.As(pingErr, &pgErr) && pgErr.Code != "28P01" {
			shouldHeal = false
		}
		if shouldHeal {
			project, _ := s.projectRepo.GetByIDAndUserID(ctx, projectID, userID)
			if project != nil {
				resourceRef := s.provisioner.ResourceRefForProject(projectID, project.DBType)
				healCtx, healCancel := context.WithTimeout(ctx, 30*time.Second)
				healResult, healErr := s.provisioner.GetConnectionByResourceRef(healCtx, resourceRef, project.DBType)
				healCancel()
				if healErr == nil && healResult != nil {
					pool.Close()
					_ = s.instanceRepo.UpdateConnectionInfo(params.instanceID, healResult.Host, healResult.Port)
					if encrypted, encErr := utils.EncryptString(healResult.Password); encErr == nil {
						_ = s.credRepo.Upsert(&models.DatabaseCredential{
							DBInstanceID:      params.instanceID,
							Username:          "admin",
							PasswordEncrypted: encrypted,
						})
					}
					pool, err = database.ConnectToPostgresProject(healResult.Host, healResult.Port, healResult.User, healResult.Password, "app")
					if err != nil {
						return nil, err
					}
					pingCtx2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
					pingErr = pool.Ping(pingCtx2)
					cancel2()
				}
			}
		}
	}
	if pingErr != nil {
		pool.Close()
		return nil, pingErr
	}

	return pool, nil
}

// acquireCachedPostgresPool returns a shared pool for (userID, projectID). Callers must not Close the pool.
func (s *InstanceConnectionService) acquireCachedPostgresPool(ctx context.Context, userID, projectID uuid.UUID, params *connectionParams) (*pgxpool.Pool, error) {
	key := poolCacheKey{userID: userID, projectID: projectID}

	s.pgPoolMu.Lock()
	if p, ok := s.pgPools[key]; ok {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		pingErr := p.Ping(pingCtx)
		cancel()
		if pingErr == nil {
			s.pgPoolMu.Unlock()
			return p, nil
		}
		delete(s.pgPools, key)
		p.Close()
	}
	s.pgPoolMu.Unlock()

	pool, err := s.connectPostgresPoolForParams(ctx, userID, projectID, params)
	if err != nil {
		return nil, err
	}

	s.pgPoolMu.Lock()
	defer s.pgPoolMu.Unlock()
	if existing, ok := s.pgPools[key]; ok {
		pool.Close()
		return existing, nil
	}
	s.pgPools[key] = pool
	return pool, nil
}

// GetPool returns a shared connection pool for the project's database instance.
// Do not Close the returned pool; it is cached for reuse across requests.
func (s *InstanceConnectionService) GetPool(ctx context.Context, userID, projectID uuid.UUID) (*pgxpool.Pool, error) {
	params, err := s.getConnectionParams(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	return s.acquireCachedPostgresPool(ctx, userID, projectID, params)
}

// GetPoolWithMeta returns a shared pool and the instance ID (for query history).
// Do not Close the returned pool.
func (s *InstanceConnectionService) GetPoolWithMeta(ctx context.Context, userID, projectID uuid.UUID) (*pgxpool.Pool, uuid.UUID, error) {
	params, err := s.getConnectionParams(ctx, userID, projectID)
	if err != nil {
		return nil, uuid.Nil, err
	}
	pool, err := s.acquireCachedPostgresPool(ctx, userID, projectID, params)
	if err != nil {
		return nil, uuid.Nil, err
	}
	return pool, params.instanceID, nil
}

// GetMongoClient returns a MongoDB client for the project's database instance.
// Caller must defer client.Disconnect(ctx) on the returned client.
//func (s *InstanceConnectionService) GetMongoClient(ctx context.Context, userID, projectID uuid.UUID) (*mongo.Client, error) {
//	params, err := s.getConnectionParams(ctx, userID, projectID)
//	if err != nil {
//		return nil, err
//	}
//
//	client, err := repository.ConnectToMongoProject(params.host, params.port, params.username, params.password, "app")
//	if err != nil {
//		return nil, err
//	}
//
//	return client, nil
//}

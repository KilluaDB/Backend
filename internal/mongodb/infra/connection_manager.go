package infra

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// DSNProvider provides raw connection strings for MongoDB instances.
type DSNProvider interface {
	GetConnectionDSN(ctx context.Context, userID, projectID uuid.UUID) (dsn string, instanceID uuid.UUID, err error)
}

// MongoConnectionManager manages a client cache for project MongoDB connections.
type MongoConnectionManager struct {
	provider DSNProvider

	clientMu sync.Mutex
	clients  map[uuid.UUID]cachedMongoClient
}

type cachedMongoClient struct {
	dsn    string
	dbName string
	client *mongo.Client
}

// NewMongoConnectionManager creates a manager for MongoDB clients using a DSNProvider.
func NewMongoConnectionManager(provider DSNProvider) *MongoConnectionManager {
	return &MongoConnectionManager{
		provider: provider,
		clients:  make(map[uuid.UUID]cachedMongoClient),
	}
}

// GetDatabase returns a shared database handle for the project's MongoDB instance.
// Callers must not Disconnect the returned client.
func (s *MongoConnectionManager) GetDatabase(ctx context.Context, userID, projectID uuid.UUID) (*mongo.Database, error) {
	dsn, _, err := s.provider.GetConnectionDSN(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	client, dbName, err := s.acquireCachedClient(ctx, projectID, dsn)
	if err != nil {
		return nil, err
	}
	return client.Database(dbName), nil
}

func (s *MongoConnectionManager) acquireCachedClient(ctx context.Context, projectID uuid.UUID, dsn string) (*mongo.Client, string, error) {
	s.clientMu.Lock()
	if entry, ok := s.clients[projectID]; ok {
		if entry.dsn != dsn || entry.client == nil {
			delete(s.clients, projectID)
			s.clientMu.Unlock()
			if entry.client != nil {
				_ = entry.client.Disconnect(context.Background())
			}
			return s.connectAndCacheClient(ctx, projectID, dsn)
		}

		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		pingErr := entry.client.Ping(pingCtx, nil)
		cancel()
		if pingErr == nil {
			s.clientMu.Unlock()
			return entry.client, entry.dbName, nil
		}

		delete(s.clients, projectID)
		s.clientMu.Unlock()
		_ = entry.client.Disconnect(context.Background())
		return s.connectAndCacheClient(ctx, projectID, dsn)
	}
	s.clientMu.Unlock()

	return s.connectAndCacheClient(ctx, projectID, dsn)
}

func (s *MongoConnectionManager) connectAndCacheClient(ctx context.Context, projectID uuid.UUID, dsn string) (*mongo.Client, string, error) {
	client, err := s.connectMongoClient(ctx, dsn)
	if err != nil {
		return nil, "", err
	}
	name := databaseNameFromDSN(dsn)

	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	// Another goroutine may have connected while we were dialing.
	if existing, ok := s.clients[projectID]; ok {
		if existing.dsn == dsn && existing.client != nil {
			_ = client.Disconnect(context.Background())
			return existing.client, existing.dbName, nil
		}
		if existing.client != nil {
			_ = existing.client.Disconnect(context.Background())
		}
	}
	s.clients[projectID] = cachedMongoClient{dsn: dsn, dbName: name, client: client}
	return client, name, nil
}

func (s *MongoConnectionManager) connectMongoClient(ctx context.Context, dsn string) (*mongo.Client, error) {
	// mongo-driver v2: Connect does not accept context; use Ping to verify.
	client, err := mongo.Connect(options.Client().ApplyURI(dsn))
	if err != nil {
		return nil, fmt.Errorf("connect instance mongo: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping instance mongo: %w", err)
	}

	return client, nil
}

// EvictProject closes and removes a cached project client if present.
func (s *MongoConnectionManager) EvictProject(projectID uuid.UUID) {
	s.clientMu.Lock()
	entry, ok := s.clients[projectID]
	if ok {
		delete(s.clients, projectID)
	}
	s.clientMu.Unlock()

	if ok && entry.client != nil {
		_ = entry.client.Disconnect(context.Background())
	}
}

// CloseAll closes and clears all cached project clients.
func (s *MongoConnectionManager) CloseAll() {
	s.clientMu.Lock()
	entries := make([]cachedMongoClient, 0, len(s.clients))
	for _, entry := range s.clients {
		entries = append(entries, entry)
	}
	s.clients = make(map[uuid.UUID]cachedMongoClient)
	s.clientMu.Unlock()

	for _, entry := range entries {
		if entry.client != nil {
			_ = entry.client.Disconnect(context.Background())
		}
	}
}

func databaseNameFromDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "admin"
	}
	path := strings.TrimPrefix(parsed.Path, "/")
	if path == "" {
		return "admin"
	}
	if idx := strings.Index(path, "/"); idx >= 0 {
		path = path[:idx]
	}
	return path
}
package infra

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"backend/internal/metrics"

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
	provider  DSNProvider
	connectFn func(ctx context.Context, dsn string) (*mongo.Client, error)

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
		provider:  provider,
		connectFn: defaultConnectClient,
		clients:   make(map[uuid.UUID]cachedMongoClient),
	}
}

// GetDatabase returns a shared database handle for the project's MongoDB instance.
// Callers must not Disconnect the returned client.
func (s *MongoConnectionManager) GetDatabase(ctx context.Context, userID, projectID uuid.UUID) (*mongo.Database, error) {
	s.clientMu.Lock()
	entry, ok := s.clients[projectID]
	if ok && entry.client != nil {
		client := entry.client
		dbName := entry.dbName
		s.clientMu.Unlock()
		s.emitClientMetrics()
		return client.Database(dbName), nil
	}
	s.clientMu.Unlock()

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

// GetInstanceID returns the Kubernetes instance ID for the project's MongoDB.
func (s *MongoConnectionManager) GetInstanceID(ctx context.Context, userID, projectID uuid.UUID) (uuid.UUID, error) {
	s.clientMu.Lock()
	if entry, ok := s.clients[projectID]; ok && entry.client != nil {
		s.clientMu.Unlock()
		return projectID, nil
	}
	s.clientMu.Unlock()

	_, instanceID, err := s.provider.GetConnectionDSN(ctx, userID, projectID)
	if err != nil {
		return uuid.Nil, err
	}
	return instanceID, nil
}

func (s *MongoConnectionManager) acquireCachedClient(ctx context.Context, projectID uuid.UUID, dsn string) (*mongo.Client, string, error) {
	s.clientMu.Lock()
	entry, ok := s.clients[projectID]
	if ok && (entry.dsn != dsn || entry.client == nil) {
		// DSN changed or client is nil — evict and reconnect.
		delete(s.clients, projectID)
		s.clientMu.Unlock()
		if entry.client != nil {
			_ = entry.client.Disconnect(context.Background())
		}
		return s.connectAndCacheClient(ctx, projectID, dsn)
	}
	if ok {
		// Copy the entry and release the lock.
		cachedClient := entry.client
		cachedDBName := entry.dbName
		s.clientMu.Unlock()

		s.emitClientMetrics()
		return cachedClient, cachedDBName, nil
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

	var retClient *mongo.Client
	var retName string
	var discardClient *mongo.Client

	s.clientMu.Lock()
	// Another goroutine may have connected while we were dialing.
	if existing, ok := s.clients[projectID]; ok {
		if existing.dsn == dsn && existing.client != nil {
			// Use the winner; discard our connection after releasing the lock.
			discardClient = client
			retClient = existing.client
			retName = existing.dbName
		} else {
			if existing.client != nil {
				discardClient = existing.client
			}
			s.clients[projectID] = cachedMongoClient{dsn: dsn, dbName: name, client: client}
			retClient = client
			retName = name
		}
	} else {
		s.clients[projectID] = cachedMongoClient{dsn: dsn, dbName: name, client: client}
		retClient = client
		retName = name
	}
	s.clientMu.Unlock()

	if discardClient != nil {
		_ = discardClient.Disconnect(context.Background())
	}
	s.emitClientMetrics()
	return retClient, retName, nil
}

func defaultConnectClient(ctx context.Context, dsn string) (*mongo.Client, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(dsn))
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "connect").Inc()
		return nil, fmt.Errorf("connect instance mongo: %w", err)
	}

	pingStart := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "ping").Inc()
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping instance mongo: %w", err)
	}
	metrics.MongoPingLatency.Observe(time.Since(pingStart).Seconds())

	return client, nil
}

func (s *MongoConnectionManager) connectMongoClient(ctx context.Context, dsn string) (*mongo.Client, error) {
	return s.connectFn(ctx, dsn)
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
	s.emitClientMetrics()
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
	s.emitClientMetrics()
}

func (s *MongoConnectionManager) emitClientMetrics() {
	s.clientMu.Lock()
	count := len(s.clients)
	s.clientMu.Unlock()
	metrics.MongoClientCount.Set(float64(count))
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

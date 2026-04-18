package infra

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DSNProvider provides raw connection strings to the Postgres Connection Manager.
type DSNProvider interface {
	GetConnectionDSN(ctx context.Context, userID, projectID uuid.UUID) (dsn string, instanceID uuid.UUID, err error)
}

// PostgresConnectionManager manages a pool cache for project database connections.
type PostgresConnectionManager struct {
	provider DSNProvider

	pgPoolMu sync.Mutex
	pgPools  map[uuid.UUID]cachedPgPool // keyed by projectID
}

type cachedPgPool struct {
	dsn  string
	pool *pgxpool.Pool
}

// NewPostgresConnectionManager creates a manager for Postgres pools using a DSNProvider.
func NewPostgresConnectionManager(provider DSNProvider) *PostgresConnectionManager {
	return &PostgresConnectionManager{
		provider: provider,
		pgPools:  make(map[uuid.UUID]cachedPgPool),
	}
}

// acquireCachedPool returns a shared pool for the project, creating one if needed.
// The cached pool is validated with a ping before returning; stale pools are evicted.
// Callers must not Close the returned pool.
func (s *PostgresConnectionManager) acquireCachedPool(ctx context.Context, projectID uuid.UUID, dsn string) (*pgxpool.Pool, error) {
	s.pgPoolMu.Lock()
	if entry, ok := s.pgPools[projectID]; ok {
		if entry.dsn != dsn {
			// DSN changed (e.g., operator secret rotation or reprovision) — reconnect.
			delete(s.pgPools, projectID)
			if entry.pool != nil {
				entry.pool.Close()
			}
			s.pgPoolMu.Unlock()
			return s.connectAndCachePool(ctx, projectID, dsn)
		}

		if entry.pool == nil {
			delete(s.pgPools, projectID)
			s.pgPoolMu.Unlock()
			return s.connectAndCachePool(ctx, projectID, dsn)
		}

		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		pingErr := entry.pool.Ping(pingCtx)
		cancel()
		if pingErr == nil {
			s.pgPoolMu.Unlock()
			return entry.pool, nil
		}

		// Stale pool — evict and reconnect.
		delete(s.pgPools, projectID)
		entry.pool.Close()
	}
	s.pgPoolMu.Unlock()

	return s.connectAndCachePool(ctx, projectID, dsn)
}

func (s *PostgresConnectionManager) connectAndCachePool(ctx context.Context, projectID uuid.UUID, dsn string) (*pgxpool.Pool, error) {
	pool, err := s.connectPostgresPool(ctx, dsn)
	if err != nil {
		return nil, err
	}

	s.pgPoolMu.Lock()
	defer s.pgPoolMu.Unlock()
	// Another goroutine may have connected while we were dialing.
	if existing, ok := s.pgPools[projectID]; ok {
		if existing.dsn == dsn && existing.pool != nil {
			pool.Close()
			return existing.pool, nil
		}
		if existing.pool != nil {
			existing.pool.Close()
		}
	}
	s.pgPools[projectID] = cachedPgPool{dsn: dsn, pool: pool}
	return pool, nil
}

func (s *PostgresConnectionManager) connectPostgresPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse instance postgres DSN: %w", err)
	}

	if config.MaxConns == 0 {
		config.MaxConns = 5
	}
	if config.MinConns == 0 {
		config.MinConns = 1
	}
	if config.MaxConnLifetime == 0 {
		config.MaxConnLifetime = 5 * time.Minute
	}
	if config.MaxConnIdleTime == 0 {
		config.MaxConnIdleTime = 1 * time.Minute
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create instance postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping instance postgres: %w", err)
	}

	return pool, nil
}

// GetPool returns a shared connection pool for the project's PostgreSQL instance.
// Do not Close the returned pool; it is cached for reuse across requests.
func (s *PostgresConnectionManager) GetPool(ctx context.Context, userID, projectID uuid.UUID) (*pgxpool.Pool, error) {
	dsn, _, err := s.provider.GetConnectionDSN(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	return s.acquireCachedPool(ctx, projectID, dsn)
}

// GetPoolWithMeta returns a shared pool and the instance ID (for query history).
// Do not Close the returned pool.
func (s *PostgresConnectionManager) GetPoolWithMeta(ctx context.Context, userID, projectID uuid.UUID) (*pgxpool.Pool, uuid.UUID, error) {
	dsn, instanceID, err := s.provider.GetConnectionDSN(ctx, userID, projectID)
	if err != nil {
		return nil, uuid.Nil, err
	}
	pool, err := s.acquireCachedPool(ctx, projectID, dsn)
	if err != nil {
		return nil, uuid.Nil, err
	}
	return pool, instanceID, nil
}

// GetInstanceID returns the database instance ID for the project.
func (s *PostgresConnectionManager) GetInstanceID(ctx context.Context, userID, projectID uuid.UUID) (uuid.UUID, error) {
	_, instanceID, err := s.provider.GetConnectionDSN(ctx, userID, projectID)
	return instanceID, err
}

// EvictProject closes and removes a cached project pool if present.
func (s *PostgresConnectionManager) EvictProject(projectID uuid.UUID) {
	s.pgPoolMu.Lock()
	entry, ok := s.pgPools[projectID]
	if ok {
		delete(s.pgPools, projectID)
	}
	s.pgPoolMu.Unlock()

	if ok && entry.pool != nil {
		entry.pool.Close()
	}
}

// CloseAll closes and clears all cached project pools.
func (s *PostgresConnectionManager) CloseAll() {
	s.pgPoolMu.Lock()
	entries := make([]cachedPgPool, 0, len(s.pgPools))
	for _, entry := range s.pgPools {
		entries = append(entries, entry)
	}
	s.pgPools = make(map[uuid.UUID]cachedPgPool)
	s.pgPoolMu.Unlock()

	for _, entry := range entries {
		if entry.pool != nil {
			entry.pool.Close()
		}
	}
}

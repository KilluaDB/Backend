package infra

import (
	"context"
	"fmt"
	"sync"
	"time"

	"backend/internal/metrics"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	mgr := &PostgresConnectionManager{
		provider: provider,
		pgPools:  make(map[uuid.UUID]cachedPgPool),
	}
	go mgr.backgroundTask()
	return mgr
}

func (s *PostgresConnectionManager) backgroundTask() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.emitPoolMetrics()
		s.evictIdlePools()
	}
}

func (s *PostgresConnectionManager) evictIdlePools() {
	s.pgPoolMu.Lock()
	var toClose []*pgxpool.Pool
	var evictIDs []string
	for id, entry := range s.pgPools {
		if entry.pool != nil && entry.pool.Stat().TotalConns() == 0 {
			toClose = append(toClose, entry.pool)
			evictIDs = append(evictIDs, id.String()[:8])
			delete(s.pgPools, id)
		}
	}
	s.pgPoolMu.Unlock()

	for _, p := range toClose {
		p.Close()
	}
	for _, idStr := range evictIDs {
		metrics.PgPoolAcquiredConns.DeleteLabelValues(idStr)
		metrics.PgPoolIdleConns.DeleteLabelValues(idStr)
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
			s.pgPoolMu.Unlock()
			if entry.pool != nil {
				entry.pool.Close()
			}
			return s.connectAndCachePool(ctx, projectID, dsn)
		}

		if entry.pool == nil {
			delete(s.pgPools, projectID)
			s.pgPoolMu.Unlock()
			return s.connectAndCachePool(ctx, projectID, dsn)
		}

		// To avoid holding the global lock during a potentially slow network ping, unlock first.
		s.pgPoolMu.Unlock()

		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		pingErr := entry.pool.Ping(pingCtx)
		cancel()
		if pingErr == nil {
					return entry.pool, nil
		}

		// Stale pool — evict and reconnect.
		s.pgPoolMu.Lock()
		delete(s.pgPools, projectID)
		s.pgPoolMu.Unlock()

		entry.pool.Close()
		return s.connectAndCachePool(ctx, projectID, dsn)
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
	var oldPoolToClose *pgxpool.Pool
	if existing, ok := s.pgPools[projectID]; ok {
		if existing.dsn == dsn && existing.pool != nil {
			s.pgPoolMu.Unlock()
			pool.Close()
					return existing.pool, nil
		}
		if existing.pool != nil {
			oldPoolToClose = existing.pool
		}
	}
	s.pgPools[projectID] = cachedPgPool{dsn: dsn, pool: pool}
	s.pgPoolMu.Unlock()

	if oldPoolToClose != nil {
		oldPoolToClose.Close()
	}
	
	return pool, nil
}

func (s *PostgresConnectionManager) connectPostgresPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		metrics.PgPoolErrorsTotal.WithLabelValues("connect").Inc()
		return nil, fmt.Errorf("parse instance postgres DSN: %w", err)
	}

	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec

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
		metrics.PgPoolErrorsTotal.WithLabelValues("connect").Inc()
		return nil, fmt.Errorf("create instance postgres pool: %w", err)
	}

	pingStart := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		metrics.PgPoolErrorsTotal.WithLabelValues("ping").Inc()
		pool.Close()
		return nil, fmt.Errorf("ping instance postgres: %w", err)
	}
	metrics.PgPingLatency.Observe(time.Since(pingStart).Seconds())

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

	if ok {
		if entry.pool != nil {
			entry.pool.Close()
		}
		idStr := projectID.String()[:8]
		metrics.PgPoolAcquiredConns.DeleteLabelValues(idStr)
		metrics.PgPoolIdleConns.DeleteLabelValues(idStr)
	}
}

// CloseAll closes and clears all cached project pools.
func (s *PostgresConnectionManager) CloseAll() {
	s.pgPoolMu.Lock()
	entries := make(map[uuid.UUID]cachedPgPool, len(s.pgPools))
	for k, v := range s.pgPools {
		entries[k] = v
	}
	s.pgPools = make(map[uuid.UUID]cachedPgPool)
	s.pgPoolMu.Unlock()

	for id, entry := range entries {
		if entry.pool != nil {
			entry.pool.Close()
		}
		idStr := id.String()[:8]
		metrics.PgPoolAcquiredConns.DeleteLabelValues(idStr)
		metrics.PgPoolIdleConns.DeleteLabelValues(idStr)
	}
}

func (s *PostgresConnectionManager) emitPoolMetrics() {
	s.pgPoolMu.Lock()
	count := len(s.pgPools)
	pools := make(map[uuid.UUID]cachedPgPool, count)
	for k, v := range s.pgPools {
		pools[k] = v
	}
	s.pgPoolMu.Unlock()

	metrics.PgPoolCount.Set(float64(count))
	for id, entry := range pools {
		if entry.pool == nil {
			continue
		}
		stat := entry.pool.Stat()
		idStr := id.String()[:8]
		metrics.PgPoolAcquiredConns.WithLabelValues(idStr).Set(float64(stat.AcquiredConns()))
		metrics.PgPoolIdleConns.WithLabelValues(idStr).Set(float64(stat.IdleConns()))
	}
}

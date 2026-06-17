package infra

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type mockDSNProvider struct {
	dsn        string
	instanceID uuid.UUID
	err        error
}

func (m *mockDSNProvider) GetConnectionDSN(ctx context.Context, userID, projectID uuid.UUID) (string, uuid.UUID, error) {
	return m.dsn, m.instanceID, m.err
}

type fakePoolConnector struct {
	pool *pgxpool.Pool
	err  error
}

func (f *fakePoolConnector) Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.pool, nil
}

func setupTestPostgres(t *testing.T) (*pgxpool.Pool, func()) {
	ctx := context.Background()
	postgresContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	require.NoError(t, err)

	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	config, err := pgxpool.ParseConfig(connStr)
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)

	cleanup := func() {
		pool.Close()
		postgresContainer.Terminate(ctx)
	}

	return pool, cleanup
}

func TestPostgresConnectionManager(t *testing.T) {
	pool, cleanup := setupTestPostgres(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	projectID := uuid.New()
	instanceID := uuid.New()
	dsn := "fake-dsn"

	provider := &mockDSNProvider{
		dsn:        dsn,
		instanceID: instanceID,
	}
	connector := &fakePoolConnector{
		pool: pool,
	}

	mgr := &PostgresConnectionManager{
		provider:  provider,
		connector: connector,
		pgPools:   make(map[uuid.UUID]cachedPgPool),
	}

	t.Run("GetPool - cache miss", func(t *testing.T) {
		gotPool, err := mgr.GetPool(ctx, userID, projectID)
		assert.NoError(t, err)
		assert.NotNil(t, gotPool)

		mgr.pgPoolMu.Lock()
		entry, ok := mgr.pgPools[projectID]
		mgr.pgPoolMu.Unlock()

		assert.True(t, ok)
		assert.Equal(t, dsn, entry.dsn)
	})

	t.Run("GetPool - cache hit", func(t *testing.T) {
		gotPool, err := mgr.GetPool(ctx, userID, projectID)
		assert.NoError(t, err)
		assert.NotNil(t, gotPool)
		// Should return the exact same pool instance
		assert.Equal(t, pool, gotPool)
	})

	t.Run("GetPoolWithMeta", func(t *testing.T) {
		gotPool, gotInstanceID, err := mgr.GetPoolWithMeta(ctx, userID, projectID)
		assert.NoError(t, err)
		assert.Equal(t, pool, gotPool)
		assert.Equal(t, projectID, gotInstanceID)
	})

	t.Run("GetInstanceID", func(t *testing.T) {
		gotInstanceID, err := mgr.GetInstanceID(ctx, userID, projectID)
		assert.NoError(t, err)
		assert.Equal(t, projectID, gotInstanceID)
	})

	t.Run("acquireCachedPool - DSN changed", func(t *testing.T) {
		newDSN := "new-fake-dsn"
		provider.dsn = newDSN

		// Should trigger reconnect (using the same fake pool, but tests logic)
		gotPool, err := mgr.acquireCachedPool(ctx, projectID, newDSN)
		assert.NoError(t, err)
		assert.NotNil(t, gotPool)

		mgr.pgPoolMu.Lock()
		entry := mgr.pgPools[projectID]
		mgr.pgPoolMu.Unlock()
		assert.Equal(t, newDSN, entry.dsn)

		// Reset DSN
		provider.dsn = dsn
		mgr.acquireCachedPool(ctx, projectID, dsn)
	})

	t.Run("GetPool - provider error", func(t *testing.T) {
		expectedErr := errors.New("provider error")
		provider.err = expectedErr

		newProjectID := uuid.New()
		_, err := mgr.GetPool(ctx, userID, newProjectID)
		assert.ErrorIs(t, err, expectedErr)

		provider.err = nil // reset
	})

	t.Run("connectAndCachePool - connect error", func(t *testing.T) {
		expectedErr := errors.New("connect error")
		connector.err = expectedErr

		newProjectID := uuid.New()
		_, err := mgr.GetPool(ctx, userID, newProjectID)
		assert.ErrorIs(t, err, expectedErr)

		connector.err = nil // reset
	})

	t.Run("acquireCachedPool - ping error", func(t *testing.T) {
		// Mock pool with bad ping by closing it
		mgr.pgPoolMu.Lock()
		pool.Close()
		mgr.pgPoolMu.Unlock()

		// Ping will fail, should reconnect
		_, err := mgr.GetPool(ctx, userID, projectID)
		// We expect it to reconnect successfully because connector.err is nil and pool is reused in fake
		// Wait, fake connector returns the closed pool again. So it will return no error but the pool is closed.
		assert.NoError(t, err)

		// Setup a fresh pool for subsequent tests
		freshPool, cleanupFresh := setupTestPostgres(t)
		defer cleanupFresh()
		pool = freshPool
		connector.pool = freshPool
		mgr.pgPoolMu.Lock()
		mgr.pgPools[projectID] = cachedPgPool{
			dsn:  dsn,
			pool: freshPool,
		}
		mgr.pgPoolMu.Unlock()
	})

	t.Run("acquireCachedPool - entry pool is nil", func(t *testing.T) {
		mgr.pgPoolMu.Lock()
		mgr.pgPools[projectID] = cachedPgPool{
			dsn:  dsn,
			pool: nil,
		}
		mgr.pgPoolMu.Unlock()

		gotPool, err := mgr.GetPool(ctx, userID, projectID)
		assert.NoError(t, err)
		assert.NotNil(t, gotPool)
	})

	t.Run("connectAndCachePool - concurrent old pool close", func(t *testing.T) {
		// We can directly invoke connectAndCachePool to simulate race condition where pool was updated
		oldPool, cleanupOld := setupTestPostgres(t)
		defer cleanupOld()

		mgr.pgPoolMu.Lock()
		mgr.pgPools[projectID] = cachedPgPool{
			dsn:  "old-dsn",
			pool: oldPool,
		}
		mgr.pgPoolMu.Unlock()

		_, err := mgr.connectAndCachePool(ctx, projectID, dsn)
		assert.NoError(t, err)
		// oldPool should be closed
		// if we try to ping it, it should fail
		err = oldPool.Ping(ctx)
		assert.Error(t, err)
	})

	t.Run("connectAndCachePool - concurrent existing pool matches dsn", func(t *testing.T) {
		// DSN is same but we hit connectAndCachePool anyway
		mgr.pgPoolMu.Lock()
		mgr.pgPools[projectID] = cachedPgPool{
			dsn:  dsn,
			pool: pool,
		}
		mgr.pgPoolMu.Unlock()

		// Instead of testing a real concurrent case, we just call it directly
		gotPool, err := mgr.connectAndCachePool(ctx, projectID, dsn)
		assert.NoError(t, err)
		// It should return the existing pool and close the new one from connector
		assert.Equal(t, pool, gotPool)
	})
}

func TestEvictIdlePools(t *testing.T) {
	// For this test, we can just use an empty struct or simple mock
	// Actually, we need a pool that has Stat().TotalConns() == 0
	pool, cleanup := setupTestPostgres(t)
	defer cleanup()

	projectID := uuid.New()

	mgr := &PostgresConnectionManager{
		pgPools: make(map[uuid.UUID]cachedPgPool),
	}

	// Add idle pool
	mgr.pgPools[projectID] = cachedPgPool{
		dsn:  "test-dsn",
		pool: pool,
	}

	// Call evict
	mgr.evictIdlePools()

	mgr.pgPoolMu.Lock()
	_, ok := mgr.pgPools[projectID]
	mgr.pgPoolMu.Unlock()

	assert.False(t, ok, "pool should be evicted when total conns is 0")
}

func TestEvictProjectAndCloseAll(t *testing.T) {
	pool1, cleanup1 := setupTestPostgres(t)
	defer cleanup1()
	pool2, cleanup2 := setupTestPostgres(t)
	defer cleanup2()

	projectID1 := uuid.New()
	projectID2 := uuid.New()

	mgr := &PostgresConnectionManager{
		pgPools: make(map[uuid.UUID]cachedPgPool),
	}

	mgr.pgPools[projectID1] = cachedPgPool{dsn: "dsn1", pool: pool1}
	mgr.pgPools[projectID2] = cachedPgPool{dsn: "dsn2", pool: pool2}

	// Evict one project
	mgr.EvictProject(projectID1)
	mgr.pgPoolMu.Lock()
	_, ok1 := mgr.pgPools[projectID1]
	_, ok2 := mgr.pgPools[projectID2]
	mgr.pgPoolMu.Unlock()

	assert.False(t, ok1)
	assert.True(t, ok2)

	// Close all
	mgr.CloseAll()
	mgr.pgPoolMu.Lock()
	assert.Empty(t, mgr.pgPools)
	mgr.pgPoolMu.Unlock()
}

func TestEmitPoolMetrics(t *testing.T) {
	pool, cleanup := setupTestPostgres(t)
	defer cleanup()

	projectID := uuid.New()
	mgr := &PostgresConnectionManager{
		pgPools: make(map[uuid.UUID]cachedPgPool),
	}

	mgr.pgPools[projectID] = cachedPgPool{dsn: "dsn", pool: pool}

	// Acquire a connection to change stats
	conn, err := pool.Acquire(context.Background())
	require.NoError(t, err)

	mgr.emitPoolMetrics()
	conn.Release()
}

func TestDefaultPoolConnector(t *testing.T) {
	ctx := context.Background()
	connector := &defaultPoolConnector{}

	// invalid DSN
	_, err := connector.Connect(ctx, "invalid-dsn")
	assert.Error(t, err)

	// valid DSN
	pool, cleanup := setupTestPostgres(t)
	defer cleanup()

	// get the conn string
	config := pool.Config()
	connStr := config.ConnString()

	newPool, err := connector.Connect(ctx, connStr)
	assert.NoError(t, err)
	if newPool != nil {
		newPool.Close()
	}
}

func TestNewPostgresConnectionManager(t *testing.T) {
	provider := &mockDSNProvider{}
	mgr := NewPostgresConnectionManager(provider)
	assert.NotNil(t, mgr)
	assert.NotNil(t, mgr.connector)
}

func TestPostgresConnectionManager_GetPoolWithMeta(t *testing.T) {
	mgr := NewPostgresConnectionManager(&mockDSNProvider{dsn: "test-dsn"})
	pID := uuid.New()

	pool, cleanup := setupTestPostgres(t)
	defer cleanup()
	mgr.pgPools[pID] = cachedPgPool{dsn: "test-dsn", pool: pool}

	gotPool, instanceID, err := mgr.GetPoolWithMeta(context.Background(), uuid.New(), pID)
	assert.NoError(t, err)
	assert.Equal(t, pool, gotPool)
	assert.Equal(t, pID, instanceID)
}

func TestPostgresConnectionManager_EvictProject(t *testing.T) {
	mgr := NewPostgresConnectionManager(&mockDSNProvider{})
	pID := uuid.New()

	pool, cleanup := setupTestPostgres(t)
	defer cleanup()
	mgr.pgPools[pID] = cachedPgPool{dsn: "test-dsn", pool: pool}

	mgr.EvictProject(pID)

	mgr.pgPoolMu.Lock()
	_, ok := mgr.pgPools[pID]
	mgr.pgPoolMu.Unlock()
	assert.False(t, ok)
}

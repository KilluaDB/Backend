//go:build integration

package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupTestPostgres(t *testing.T) (*postgres.PostgresContainer, string, func()) {
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

	cleanup := func() {
		postgresContainer.Terminate(ctx)
	}

	return postgresContainer, connStr, cleanup
}

func TestEnsureDatabaseExists_Integration(t *testing.T) {
	container, connStr, cleanup := setupTestPostgres(t)
	defer cleanup()

	// Parse host and port from connStr
	ctx := context.Background()
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	// Set env vars
	os.Setenv("DB_HOST", host)
	os.Setenv("DB_PORT", port.Port())
	os.Setenv("DB_USERNAME", "testuser")
	os.Setenv("DB_PASSWORD", "testpass")
	os.Setenv("DB_DATABASE", "newdb")
	defer func() {
		os.Unsetenv("DB_HOST")
		os.Unsetenv("DB_PORT")
		os.Unsetenv("DB_USERNAME")
		os.Unsetenv("DB_PASSWORD")
		os.Unsetenv("DB_DATABASE")
	}()

	// 1. Database does not exist -> creates it
	err = EnsureDatabaseExists()
	assert.NoError(t, err)

	// Verify creation
	config, _ := pgxpool.ParseConfig(connStr)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	defer pool.Close()

	var exists bool
	err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = 'newdb')").Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists)

	// 2. Database already exists -> no-op
	err = EnsureDatabaseExists()
	assert.NoError(t, err)

	// 3. Bad DSN
	os.Setenv("DB_PORT", "invalid")
	err = EnsureDatabaseExists()
	assert.Error(t, err)
	
	// 4. Missing Env
	os.Unsetenv("DB_HOST")
	err = EnsureDatabaseExists()
	assert.ErrorContains(t, err, "DB_HOST environment variable is required")
}

func TestConnect_Integration(t *testing.T) {
	container, _, cleanup := setupTestPostgres(t)
	defer cleanup()

	ctx := context.Background()
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	// Set env vars
	os.Setenv("DB_HOST", host)
	os.Setenv("DB_PORT", port.Port())
	os.Setenv("DB_USERNAME", "testuser")
	os.Setenv("DB_PASSWORD", "testpass")
	os.Setenv("DB_DATABASE", "testdb")
	defer func() {
		os.Unsetenv("DB_HOST")
		os.Unsetenv("DB_PORT")
		os.Unsetenv("DB_USERNAME")
		os.Unsetenv("DB_PASSWORD")
		os.Unsetenv("DB_DATABASE")
	}()

	// 1. Happy path
	pool, err := Connect()
	assert.NoError(t, err)
	assert.NotNil(t, pool)
	assert.Equal(t, pool, Pool)
	pool.Close()
	Close() // Test global close

	// 2. Unreachable host
	os.Setenv("DB_HOST", "invalid_host")
	_, err = Connect()
	assert.Error(t, err)

	// 3. Wrong password
	os.Setenv("DB_HOST", host)
	os.Setenv("DB_PASSWORD", "wrongpass")
	_, err = Connect()
	assert.Error(t, err)
	
	// 4. Missing Env
	os.Unsetenv("DB_HOST")
	_, err = Connect()
	assert.ErrorContains(t, err, "DB_HOST environment variable is required")
}

func TestRunMigrations_Integration(t *testing.T) {
	_, connStr, cleanup := setupTestPostgres(t)
	defer cleanup()

	ctx := context.Background()
	config, err := pgxpool.ParseConfig(connStr)
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	defer pool.Close()

	// 1. Apply migrations
	err = RunMigrations(pool)
	assert.NoError(t, err)

	// Verify tables
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename IN ('users', 'projects')").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// 2. Idempotent on re-run
	err = RunMigrations(pool)
	assert.NoError(t, err)
}

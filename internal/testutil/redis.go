package testutil

import (
	"testing"
	"time"

	"backend/internal/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// NewRedisClient starts an in-memory Redis instance and returns a client and cleanup function.
func NewRedisClient(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cleanup := func() {
		_ = client.Close()
		mr.Close()
	}
	return client, cleanup
}

// NewRefreshTokenStore starts an in-memory Redis and returns a RefreshTokenStore.
func NewRefreshTokenStore(t *testing.T) (*config.RefreshTokenStore, func()) {
	t.Helper()
	client, cleanup := NewRedisClient(t)
	store := config.NewRefreshTokenStore(client, 24*time.Hour)
	return store, cleanup
}

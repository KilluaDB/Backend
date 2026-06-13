package config

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshTokenStore(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	store := NewRefreshTokenStore(client, 24*time.Hour)
	ctx := context.Background()
	userID := uuid.New()
	token := "refresh-token-value"

	require.NoError(t, store.Set(ctx, token, userID))

	got, err := store.Get(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, userID, got)

	require.NoError(t, store.Delete(ctx, token))

	_, err = store.Get(ctx, token)
	assert.ErrorIs(t, err, ErrRefreshTokenNotFound)
}

// Get must distinguish a redis.Nil miss (ErrRefreshTokenNotFound) from a real
// transport error, which is surfaced verbatim.
func TestRefreshTokenStore_GetTransportError(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewRefreshTokenStore(client, time.Hour)

	// Closing the client makes every command fail with a non-Nil error.
	require.NoError(t, client.Close())

	_, err = store.Get(context.Background(), "tok")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrRefreshTokenNotFound)
}

// Get returns a parse error when the stored value is not a valid UUID.
func TestRefreshTokenStore_GetInvalidUUID(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRefreshTokenStore(client, time.Hour)

	// Seed a malformed value directly under the prefixed key.
	require.NoError(t, mr.Set(key("tok"), "not-a-uuid"))

	_, err = store.Get(context.Background(), "tok")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrRefreshTokenNotFound)
}

func TestRedisClient_MissingAddr(t *testing.T) {
	t.Setenv("REDIS_ADDR", "")
	client, err := RedisClient()
	require.Error(t, err)
	assert.Nil(t, client)
}

func TestRedisClient_Success(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	t.Setenv("REDIS_ADDR", mr.Addr())
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "2")

	client, err := RedisClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	t.Cleanup(func() { _ = client.Close() })
	assert.Equal(t, 2, client.Options().DB)
}

// A non-numeric REDIS_DB is ignored (defaults to 0) rather than failing.
func TestRedisClient_InvalidDBDefaultsToZero(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	t.Setenv("REDIS_ADDR", mr.Addr())
	t.Setenv("REDIS_DB", "not-a-number")

	client, err := RedisClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	t.Cleanup(func() { _ = client.Close() })
	assert.Equal(t, 0, client.Options().DB)
}

// When the address points at a dead server, the ping fails and an error is returned.
func TestRedisClient_DialFailure(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	addr := mr.Addr()
	mr.Close() // nothing is listening on addr anymore

	t.Setenv("REDIS_ADDR", addr)
	client, err := RedisClient()
	require.Error(t, err)
	assert.Nil(t, client)
}

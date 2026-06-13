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

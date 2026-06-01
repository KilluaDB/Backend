package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ErrRefreshTokenNotFound is returned when the token is not in Redis (expired or revoked).
var ErrRefreshTokenNotFound = errors.New("refresh token not found or expired")

const (
	redisKeyPrefix = "refreshtoken:"
)

// RedisClient returns a Redis client. REDIS_ADDR (host:port) is required; use redis:6379 in-cluster, localhost:6379 locally.
// Optional: REDIS_PASSWORD, REDIS_DB (default 0).
func RedisClient() (*redis.Client, error) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		return nil, fmt.Errorf("REDIS_ADDR is required (e.g. redis:6379 in-cluster, localhost:6379 locally)")
	}
	db := 0
	if s := os.Getenv("REDIS_DB"); s != "" {
		if d, err := strconv.Atoi(s); err == nil {
			db = d
		}
	}
	opts := &redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       db,
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return client, nil
}

// RefreshTokenStore persists refresh tokens in Redis for validation and revocation.
type RefreshTokenStore struct {
	client *redis.Client
	TTL    time.Duration
}

// NewRefreshTokenStore creates a store with the given TTL for new keys.
func NewRefreshTokenStore(client *redis.Client, ttl time.Duration) *RefreshTokenStore {
	return &RefreshTokenStore{client: client, TTL: ttl}
}

func key(token string) string {
	return redisKeyPrefix + token
}

// Set stores a refresh token for the given user with TTL.
func (s *RefreshTokenStore) Set(ctx context.Context, refreshToken string, userID uuid.UUID) error {
	k := key(refreshToken)
	return s.client.Set(ctx, k, userID.String(), s.TTL).Err()
}

// Get returns the user ID for the token if it exists and is not expired.
func (s *RefreshTokenStore) Get(ctx context.Context, refreshToken string) (uuid.UUID, error) {
	k := key(refreshToken)
	val, err := s.client.Get(ctx, k).Result()
	if err != nil {
		if err == redis.Nil {
			return uuid.Nil, ErrRefreshTokenNotFound
		}
		return uuid.Nil, err
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// Delete removes the refresh token (e.g. on logout or after rotation).
func (s *RefreshTokenStore) Delete(ctx context.Context, refreshToken string) error {
	return s.client.Del(ctx, key(refreshToken)).Err()
}

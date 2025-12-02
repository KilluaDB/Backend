package repositories

import (
	"context"
	_ "errors"
	_ "fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisRepository struct {
	rdb *redis.Client
}

func NewRedisRepository(rdb *redis.Client) *RedisRepository {
	return &RedisRepository{rdb: rdb}
}

func (r *RedisRepository) StoreSession(ctx context.Context, jti string, userId string) error {
	key := "session:" + jti
	ttl := 30 * 24 * time.Hour
	return r.rdb.Set(ctx, key, userId, ttl).Err()
}

func (r *RedisRepository) IsBlacklisted(ctx context.Context, jti string) (bool, error) {
	key := "blacklist:" + jti
	exists, err := r.rdb.Exists(ctx, key).Result()
	return exists == 1, err
}

func (r *RedisRepository) Blacklist(ctx context.Context, jti string) error {
	key := "blacklist:" + jti
	ttl := 30 * 24 * time.Hour
	return r.rdb.Set(ctx, key, "true", ttl).Err()
}

func (r *RedisRepository) DeleteSession(ctx context.Context, jti string) error {
	key := "session:" + jti
	return r.rdb.Del(ctx, key).Err()
}
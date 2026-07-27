package runner

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rediskey"
)

const (
	redisRemovedStoreTTL   = 7 * 24 * time.Hour
	redisStoreScanPageSize = 100
)

// RedisStore keeps bounded value pools in Redis so runner replicas can share
// captured values and preserve them across Pod restarts.
type RedisStore struct {
	client    redis.UniversalClient
	keyPrefix string
	limits    map[string]int
}

var _ ValueStore = (*RedisStore)(nil)

func NewRedisStore(client redis.UniversalClient, scope rediskey.Scope, configs []StoreConfig) (*RedisStore, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client must not be nil")
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	validated, err := validateStoreConfigs(configs)
	if err != nil {
		return nil, err
	}

	return &RedisStore{
		client:    client,
		keyPrefix: scope.StorePrefix(),
		limits:    validated.capacities,
	}, nil
}

func (s *RedisStore) Put(ctx context.Context, store, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if value == "" {
		return ErrEmptyStoreValue
	}

	limit, exists := s.limits[store]
	if !exists {
		return fmt.Errorf("%w: %q", ErrStoreNotFound, store)
	}
	key := s.key(store)
	if _, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.RPush(ctx, key, value)
		pipe.LTrim(ctx, key, -int64(limit), -1)
		return nil
	}); err != nil {
		return fmt.Errorf("put Redis store %q: %w", store, err)
	}
	return nil
}

func (s *RedisStore) Random(ctx context.Context, store string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if _, exists := s.limits[store]; !exists {
		return "", false, fmt.Errorf("%w: %q", ErrStoreNotFound, store)
	}

	key := s.key(store)
	length, err := s.client.LLen(ctx, key).Result()
	if err != nil {
		return "", false, fmt.Errorf("read Redis store %q length: %w", store, err)
	}
	if length == 0 {
		return "", false, nil
	}

	value, err := s.client.LIndex(ctx, key, rand.Int64N(length)).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read random value from Redis store %q: %w", store, err)
	}
	return value, true, nil
}

// ReconcileKeys keeps active store keys persistent and gives removed stores a
// seven-day rollback window. ExpireNX prevents repeated reconciliation from
// extending that window, while Persist restores a store removed by mistake.
func (s *RedisStore) ReconcileKeys(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	keys := make([]string, 0, len(s.limits))
	iterator := s.client.Scan(ctx, 0, s.keyPrefix+":*", redisStoreScanPageSize).Iterator()
	for iterator.Next(ctx) {
		key := iterator.Val()
		store := strings.TrimPrefix(key, s.keyPrefix+":")
		if strings.Contains(store, ":") || validateName(store) != nil {
			continue
		}
		keys = append(keys, key)
	}
	if err := iterator.Err(); err != nil {
		return fmt.Errorf("scan Redis store keys: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}

	if _, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, key := range keys {
			store := strings.TrimPrefix(key, s.keyPrefix+":")
			if _, active := s.limits[store]; active {
				pipe.Persist(ctx, key)
				continue
			}
			pipe.ExpireNX(ctx, key, redisRemovedStoreTTL)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("reconcile Redis store keys: %w", err)
	}
	return nil
}

func (s *RedisStore) key(store string) string {
	return s.keyPrefix + ":" + store
}

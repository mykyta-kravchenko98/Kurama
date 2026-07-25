package runner

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisStoreKeyPrefix    = "kurama:v1"
	redisRemovedStoreTTL   = 7 * 24 * time.Hour
	redisStoreScanPageSize = 100
)

// RedisStoreScope isolates values belonging to one TrafficScenario. Kubernetes
// metadata is supplied by the runner wiring rather than repeated in every
// store declaration.
type RedisStoreScope struct {
	Namespace string
	Scenario  string
	UID       string
}

// RedisStore keeps bounded value pools in Redis so runner replicas can share
// captured values and preserve them across Pod restarts.
type RedisStore struct {
	client    redis.UniversalClient
	keyPrefix string
	limits    map[string]int
}

var _ ValueStore = (*RedisStore)(nil)

func NewRedisStore(client redis.UniversalClient, scope RedisStoreScope, configs []StoreConfig) (*RedisStore, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client must not be nil")
	}
	if err := validateRedisScope(scope); err != nil {
		return nil, err
	}

	limits := make(map[string]int, len(configs))
	for i, config := range configs {
		if err := validateName(config.Name); err != nil {
			return nil, fmt.Errorf("stores[%d].name: %w", i, err)
		}
		if config.Capacity < 1 || config.Capacity > MaxStoreCapacity {
			return nil, fmt.Errorf("stores[%d].capacity must be between 1 and %d", i, MaxStoreCapacity)
		}
		if _, exists := limits[config.Name]; exists {
			return nil, fmt.Errorf("stores[%d].name %q is duplicated", i, config.Name)
		}
		limits[config.Name] = config.Capacity
	}

	return &RedisStore{
		client:    client,
		keyPrefix: strings.Join([]string{redisStoreKeyPrefix, scope.Namespace, scope.Scenario, scope.UID}, ":"),
		limits:    limits,
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

func validateRedisScope(scope RedisStoreScope) error {
	if scope.Namespace == "" {
		return fmt.Errorf("redis scope namespace must not be empty")
	}
	if scope.Scenario == "" {
		return fmt.Errorf("redis scope scenario must not be empty")
	}
	if scope.UID == "" {
		return fmt.Errorf("redis scope UID must not be empty")
	}
	if strings.Contains(scope.Namespace, ":") {
		return fmt.Errorf("redis scope namespace must not contain colon")
	}
	if strings.Contains(scope.Scenario, ":") {
		return fmt.Errorf("redis scope scenario must not contain colon")
	}
	if strings.Contains(scope.UID, ":") {
		return fmt.Errorf("redis scope UID must not contain colon")
	}
	return nil
}

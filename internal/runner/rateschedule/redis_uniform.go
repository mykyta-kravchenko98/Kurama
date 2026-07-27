package rateschedule

import (
	"context"
	_ "embed"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rediskey"
)

//go:embed select_uniform_window.lua
var selectUniformWindowLua string

var selectUniformWindowScript = redis.NewScript(selectUniformWindowLua)

// RedisUniformConfig defines the inclusive RPM range and window duration.
type RedisUniformConfig struct {
	MinRequestsPerMinute int
	MaxRequestsPerMinute int
	Window               time.Duration
}

// RedisUniform atomically selects one uniformly distributed RPM value for a
// time window. The first runner reaching Redis proposes the value; all other
// replicas read that same value until Redis TIME enters the next window.
type RedisUniform struct {
	client      redis.UniversalClient
	key         string
	minRequests int
	rangeSize   int
	window      time.Duration
	random      integerRandomSource
}

var _ Schedule = (*RedisUniform)(nil)

type integerRandomSource interface {
	IntN(n int) int
}

// NewRedisUniform creates a Redis-coordinated uniform window schedule.
func NewRedisUniform(
	client redis.UniversalClient,
	scope rediskey.Scope,
	config RedisUniformConfig,
) (*RedisUniform, error) {
	return newRedisUniform(client, scope, config, globalIntegerRandomSource{})
}

func newRedisUniform(
	client redis.UniversalClient,
	scope rediskey.Scope,
	config RedisUniformConfig,
	random integerRandomSource,
) (*RedisUniform, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client must not be nil")
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if config.MinRequestsPerMinute < 1 {
		return nil, fmt.Errorf("minimum requests per minute must be positive")
	}
	if config.MaxRequestsPerMinute < config.MinRequestsPerMinute {
		return nil, fmt.Errorf("maximum requests per minute must be greater than or equal to minimum")
	}
	if config.Window < time.Minute || config.Window%time.Minute != 0 {
		return nil, fmt.Errorf("rate schedule window must be a positive whole number of minutes")
	}
	if random == nil {
		return nil, fmt.Errorf("random source must not be nil")
	}
	rangeSize := config.MaxRequestsPerMinute - config.MinRequestsPerMinute + 1
	if rangeSize < 1 {
		return nil, fmt.Errorf("requests-per-minute range is too large")
	}
	return &RedisUniform{
		client: client,
		key: strings.Join([]string{
			scope.RateSchedulePrefix(),
			strconv.Itoa(config.MinRequestsPerMinute),
			strconv.Itoa(config.MaxRequestsPerMinute),
			strconv.FormatInt(config.Window.Microseconds(), 10),
		}, ":"),
		minRequests: config.MinRequestsPerMinute,
		rangeSize:   rangeSize,
		window:      config.Window,
		random:      random,
	}, nil
}

func (s *RedisUniform) RequestsPerMinute(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	candidate := s.minRequests + s.random.IntN(s.rangeSize)
	selected, err := selectUniformWindowScript.Run(
		ctx,
		s.client,
		[]string{s.key},
		s.window.Microseconds(),
		candidate,
	).Int()
	if err != nil {
		return 0, fmt.Errorf("select Redis rate schedule value: %w", err)
	}
	return selected, nil
}

type globalIntegerRandomSource struct{}

func (globalIntegerRandomSource) IntN(n int) int {
	return rand.IntN(n)
}

package idempotency

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	StatusProcessing = "PROCESSING"
	DefaultTTL       = 24 * time.Hour
)

type Store struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewStore(rdb *redis.Client) *Store {
	return &Store{rdb: rdb, ttl: DefaultTTL}
}

func key(k string) string { return "idempotency:" + k }

// Begin tries to claim the idempotency key. ok=true means this caller owns the work.
// If already completed, completedValue holds the stored result (e.g. payment id).
func (s *Store) Begin(ctx context.Context, idemKey string) (ok bool, completedValue string, err error) {
	res, err := s.rdb.SetArgs(ctx, key(idemKey), StatusProcessing, redis.SetArgs{
		Mode: "NX",
		TTL:  s.ttl,
	}).Result()
	if err == nil && res == "OK" {
		return true, "", nil
	}
	if err != nil && err != redis.Nil {
		// SET NX returns nil/empty when key exists in some versions — check GET
	}
	val, gerr := s.rdb.Get(ctx, key(idemKey)).Result()
	if gerr == redis.Nil {
		// race: try once more
		res, err = s.rdb.SetArgs(ctx, key(idemKey), StatusProcessing, redis.SetArgs{
			Mode: "NX",
			TTL:  s.ttl,
		}).Result()
		if err == nil && res == "OK" {
			return true, "", nil
		}
		val, gerr = s.rdb.Get(ctx, key(idemKey)).Result()
	}
	if gerr != nil {
		return false, "", fmt.Errorf("idempotency get: %w", gerr)
	}
	if val == StatusProcessing {
		return false, "", nil
	}
	return false, val, nil
}

func (s *Store) Complete(ctx context.Context, idemKey, value string) error {
	return s.rdb.Set(ctx, key(idemKey), value, s.ttl).Err()
}

func (s *Store) Release(ctx context.Context, idemKey string) error {
	return s.rdb.Del(ctx, key(idemKey)).Err()
}

// WaitForCompletion polls until the key is no longer PROCESSING or timeout.
func (s *Store) WaitForCompletion(ctx context.Context, idemKey string, timeout time.Duration) (string, bool, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		val, err := s.rdb.Get(ctx, key(idemKey)).Result()
		if err == redis.Nil {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		if val != StatusProcessing {
			return val, true, nil
		}
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return "", false, nil
}

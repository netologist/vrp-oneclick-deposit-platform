package idempotency_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/idempotency"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) (*idempotency.Store, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return idempotency.NewStore(rdb), mr
}

func TestStore_BeginComplete(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	ctx := context.Background()
	key := "pay-1"

	ok, completed, err := store.Begin(ctx, key)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Empty(t, completed)

	// Second caller while processing: no ownership, no completed value.
	ok, completed, err = store.Begin(ctx, key)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, completed)

	require.NoError(t, store.Complete(ctx, key, "payment-uuid-1"))

	ok, completed, err = store.Begin(ctx, key)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "payment-uuid-1", completed)
}

func TestStore_ReleaseAllowsReclaim(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	ctx := context.Background()
	key := "pay-release"

	ok, _, err := store.Begin(ctx, key)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, store.Release(ctx, key))

	ok, completed, err := store.Begin(ctx, key)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Empty(t, completed)
}

func TestStore_WaitForCompletion(t *testing.T) {
	t.Parallel()

	t.Run("returns completed value", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ctx := context.Background()
		key := "wait-ok"

		ok, _, err := store.Begin(ctx, key)
		require.NoError(t, err)
		require.True(t, ok)

		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(30 * time.Millisecond)
			_ = store.Complete(context.Background(), key, "result-42")
		}()

		val, found, err := store.WaitForCompletion(ctx, key, 500*time.Millisecond)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "result-42", val)
		<-done
	})

	t.Run("timeout while processing", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ctx := context.Background()
		key := "wait-timeout"

		ok, _, err := store.Begin(ctx, key)
		require.NoError(t, err)
		require.True(t, ok)

		start := time.Now()
		val, found, err := store.WaitForCompletion(ctx, key, 50*time.Millisecond)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, val)
		assert.Less(t, time.Since(start), 300*time.Millisecond)
	})

	t.Run("missing key", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		val, found, err := store.WaitForCompletion(context.Background(), "missing", 50*time.Millisecond)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, val)
	})

	t.Run("context canceled", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		key := "wait-cancel"
		ok, _, err := store.Begin(context.Background(), key)
		require.NoError(t, err)
		require.True(t, ok)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		val, found, err := store.WaitForCompletion(ctx, key, time.Second)
		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, found)
		assert.Empty(t, val)
	})
}

func TestStore_BeginIndependentKeys(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	ctx := context.Background()

	ok1, _, err := store.Begin(ctx, "a")
	require.NoError(t, err)
	ok2, _, err := store.Begin(ctx, "b")
	require.NoError(t, err)
	assert.True(t, ok1)
	assert.True(t, ok2)
}

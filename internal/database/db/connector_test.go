package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConnect_Error(t *testing.T) {
	ctx := context.Background()
	pool, err := Connect(ctx, "invalid-url")
	assert.Error(t, err)
	assert.Nil(t, pool)
	assert.Contains(t, err.Error(), "failed to parse database url")
}

func TestDBWrapper_WithRetry(t *testing.T) {
	db := &DBWrapper{}
	ctx := context.Background()

	t.Run("success first try", func(t *testing.T) {
		calls := 0
		err := db.WithRetry(ctx, func() error {
			calls++
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 1, calls)
	})

	t.Run("fail then success", func(t *testing.T) {
		calls := 0
		err := db.WithRetry(ctx, func() error {
			calls++
			if calls < 2 {
				return fmt.Errorf("temporary error")
			}
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 2, calls)
	})

	t.Run("always fail", func(t *testing.T) {
		// Mock config to make it faster
		// Note: retry.Attempts(3) is hardcoded in WithRetry
		start := time.Now()
		err := db.WithRetry(ctx, func() error {
			return fmt.Errorf("permanent error")
		})
		assert.Error(t, err)
		assert.WithinDuration(t, start.Add(1000*time.Millisecond), time.Now(), 1000*time.Millisecond)
	})
}

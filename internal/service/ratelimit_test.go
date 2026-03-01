package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimiter(t *testing.T) {
	// Use local instance for testing logic, avoiding singleton issues
	rl := &RateLimiter{
		attempts:    make(map[int64]int),
		bans:        make(map[int64]time.Time),
		maxAttempts: 3,               // Lower limit for testing
		banDuration: 1 * time.Second, // Short ban for testing
	}

	userID := int64(12345)

	// Attempt 1: OK
	err := rl.CheckAndRecord(userID, "user1")
	assert.NoError(t, err)

	// Attempt 2: OK
	err = rl.CheckAndRecord(userID, "user2")
	assert.NoError(t, err)

	// Attempt 3: Error (limit reached)
	// count becomes 3. 3 > 3 is False: still allowed, no error.
	err = rl.CheckAndRecord(userID, "user3")
	assert.NoError(t, err)

	// Attempt 4: limit exceeded (3 > maxAttempts=3? Actually after increment becomes 4): should ban
	err = rl.CheckAndRecord(userID, "user4")
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "rate_limit_exceeded")
	}

	// Same user, another attempt while banned
	err = rl.CheckAndRecord(userID, "user1")
	assert.Error(t, err)

	// Wait for ban to expire
	time.Sleep(1100 * time.Millisecond)

	// Attempt 5: Should be allowed (reset after ban expired)
	err = rl.CheckAndRecord(userID, "user5")
	assert.NoError(t, err)
}

func TestGetRateLimiter(t *testing.T) {
	rl := GetRateLimiter()
	assert.NotNil(t, rl)
	assert.Equal(t, 6, rl.maxAttempts)
	// Second call returns same instance (singleton)
	rl2 := GetRateLimiter()
	assert.Equal(t, rl, rl2)
}

package service

import (
	"fmt"
	"sync"
	"time"
)

type RateLimiter struct {
	mu          sync.Mutex
	attempts    map[int64]int       // map[telegramID]count
	bans        map[int64]time.Time // map[telegramID]banUntil
	maxAttempts int
	banDuration time.Duration
}

var (
	globalRateLimiter *RateLimiter
	once              sync.Once
)

// GetRateLimiter returns the singleton rate limiter instance
func GetRateLimiter() *RateLimiter {
	once.Do(func() {
		globalRateLimiter = &RateLimiter{
			attempts:    make(map[int64]int),
			bans:        make(map[int64]time.Time),
			maxAttempts: 6,
			banDuration: 24 * time.Hour,
		}
		// Start cleanup routine
		go globalRateLimiter.cleanupLoop()
	})
	return globalRateLimiter
}

// CheckAndRecord ensures the user is not banned and records the attempt.
// Returns error if banned or limit exceeded.
func (rl *RateLimiter) CheckAndRecord(telegramID int64, login string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Check if banned
	if banTime, ok := rl.bans[telegramID]; ok {
		if time.Now().Before(banTime) {
			return fmt.Errorf("rate_limit_exceeded: blocked until %s", banTime.Format(time.TimeOnly))
		}
		// Ban expired
		delete(rl.bans, telegramID)
		delete(rl.attempts, telegramID)
	}

	// Record attempt
	rl.attempts[telegramID]++

	// Check limit
	if rl.attempts[telegramID] > rl.maxAttempts {
		banUntil := time.Now().Add(rl.banDuration)
		rl.bans[telegramID] = banUntil
		return fmt.Errorf("rate_limit_exceeded: too many attempts, try after 24h")
	}

	return nil
}

// Reset clears attempts and ban for a user (e.g., after successful verification).
func (rl *RateLimiter) Reset(telegramID int64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, telegramID)
	delete(rl.bans, telegramID)
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for id, banTime := range rl.bans {
			if now.After(banTime) {
				delete(rl.bans, id)
				delete(rl.attempts, id)
			}
		}
		rl.mu.Unlock()
	}
}

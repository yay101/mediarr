package indexer

import (
	"sync"
	"time"
)

// RateLimiter implements per-indexer rate limiting with exponential backoff.
// Prevents overwhelming indexers with requests and handles recovery after failures.
type RateLimiter struct {
	mu          sync.RWMutex
	lastRequest map[uint32]time.Time     // Last request time per indexer
	minInterval time.Duration            // Base minimum interval between requests
	backoff     map[uint32]time.Duration // Current backoff duration per indexer
	maxBackoff  time.Duration            // Maximum backoff (e.g., 5 minutes)
	failures    map[uint32]int           // Consecutive failure count per indexer
	cooldown    map[uint32]time.Time     // Cooldown end time per indexer
}

// NewRateLimiter creates a rate limiter with the specified minimum interval.
// Indexers should use at least 2 seconds between requests to avoid bans.
func NewRateLimiter(minInterval time.Duration) *RateLimiter {
	return &RateLimiter{
		lastRequest: make(map[uint32]time.Time),
		minInterval: minInterval,
		backoff:     make(map[uint32]time.Duration),
		maxBackoff:  5 * time.Minute,
		failures:    make(map[uint32]int),
		cooldown:    make(map[uint32]time.Time),
	}
}

// Allow checks if a request can be made immediately without waiting.
// Returns true if enough time has passed since the last request and
// the indexer is not in cooldown.
func (rl *RateLimiter) Allow(indexerID uint32) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Check if in cooldown period after repeated failures
	if cooldown, exists := rl.cooldown[indexerID]; exists {
		if now.Before(cooldown) {
			return false
		}
		delete(rl.cooldown, indexerID)
	}

	// Check if backoff interval has passed
	if interval, exists := rl.backoff[indexerID]; exists {
		if lastReq, exists := rl.lastRequest[indexerID]; exists {
			if now.Sub(lastReq) < interval {
				return false
			}
		}
	}

	rl.lastRequest[indexerID] = now
	return true
}

// Wait blocks until enough time has passed to make a request.
// Call this before making a request to ensure rate limiting is respected.
func (rl *RateLimiter) Wait(indexerID uint32) {
	rl.mu.Lock()

	// Calculate wait time if backoff is active
	if interval, exists := rl.backoff[indexerID]; exists {
		if lastReq, exists := rl.lastRequest[indexerID]; exists {
			waitTime := interval - time.Since(lastReq)
			if waitTime > 0 {
				rl.mu.Unlock()
				time.Sleep(waitTime)
				rl.mu.Lock()
			}
		}
	}

	rl.lastRequest[indexerID] = time.Now()
	rl.mu.Unlock()
}

// RecordSuccess clears failure state and resets backoff on successful request.
func (rl *RateLimiter) RecordSuccess(indexerID uint32) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.failures, indexerID)
	delete(rl.backoff, indexerID)
}

// RecordFailure increments failure count and increases backoff exponentially.
// After 3 consecutive failures, puts the indexer in a cooldown period.
func (rl *RateLimiter) RecordFailure(indexerID uint32) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.failures[indexerID]++
	failCount := rl.failures[indexerID]

	// Calculate new backoff (double previous, starting from minInterval)
	currentBackoff := rl.backoff[indexerID]
	if currentBackoff == 0 {
		currentBackoff = rl.minInterval
	}

	newBackoff := currentBackoff * 2
	if newBackoff > rl.maxBackoff {
		newBackoff = rl.maxBackoff
	}

	rl.backoff[indexerID] = newBackoff

	// Enter cooldown after 3 failures to prevent hammering a struggling indexer
	if failCount >= 3 {
		rl.cooldown[indexerID] = time.Now().Add(newBackoff)
	}
}

// GetBackoff returns the current backoff duration for an indexer.
func (rl *RateLimiter) GetBackoff(indexerID uint32) time.Duration {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.backoff[indexerID]
}

// IsCooldown checks if an indexer is currently in cooldown.
func (rl *RateLimiter) IsCooldown(indexerID uint32) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	if cooldown, exists := rl.cooldown[indexerID]; exists {
		return time.Now().Before(cooldown)
	}
	return false
}

// Reset clears all rate limiting state for an indexer.
// Use when re-enabling a previously disabled indexer.
func (rl *RateLimiter) Reset(indexerID uint32) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.lastRequest, indexerID)
	delete(rl.backoff, indexerID)
	delete(rl.failures, indexerID)
	delete(rl.cooldown, indexerID)
}

// ResetAll clears rate limiting state for all indexers.
func (rl *RateLimiter) ResetAll() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.lastRequest = make(map[uint32]time.Time)
	rl.backoff = make(map[uint32]time.Duration)
	rl.failures = make(map[uint32]int)
	rl.cooldown = make(map[uint32]time.Time)
}

// SearchLimiter manages separate rate limiters for multiple indexers.
// Lazily creates limiters on first use with a default minimum interval.
type SearchLimiter struct {
	rateLimiters map[uint32]*RateLimiter
	mu           sync.RWMutex
	defaultMin   time.Duration
}

// NewSearchLimiter creates a search limiter with a default minimum interval.
func NewSearchLimiter(defaultInterval time.Duration) *SearchLimiter {
	return &SearchLimiter{
		rateLimiters: make(map[uint32]*RateLimiter),
		defaultMin:   defaultInterval,
	}
}

// GetLimiter returns the rate limiter for an indexer, creating if needed.
func (sl *SearchLimiter) GetLimiter(indexerID uint32) *RateLimiter {
	sl.mu.RLock()
	limiter, exists := sl.rateLimiters[indexerID]
	sl.mu.RUnlock()

	if exists {
		return limiter
	}

	// Double-checked locking for lazy initialization
	sl.mu.Lock()
	defer sl.mu.Unlock()

	if limiter, exists = sl.rateLimiters[indexerID]; exists {
		return limiter
	}

	limiter = NewRateLimiter(sl.defaultMin)
	sl.rateLimiters[indexerID] = limiter
	return limiter
}

func (sl *SearchLimiter) Allow(indexerID uint32) bool {
	return sl.GetLimiter(indexerID).Allow(indexerID)
}

func (sl *SearchLimiter) Wait(indexerID uint32) {
	sl.GetLimiter(indexerID).Wait(indexerID)
}

func (sl *SearchLimiter) RecordSuccess(indexerID uint32) {
	sl.GetLimiter(indexerID).RecordSuccess(indexerID)
}

func (sl *SearchLimiter) RecordFailure(indexerID uint32) {
	sl.GetLimiter(indexerID).RecordFailure(indexerID)
}

func (sl *SearchLimiter) Reset(indexerID uint32) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	delete(sl.rateLimiters, indexerID)
}

// GlobalSearchLimiter is the default rate limiter for all indexer searches.
// Uses 2-second minimum interval between requests.
var GlobalSearchLimiter = NewSearchLimiter(2 * time.Second)

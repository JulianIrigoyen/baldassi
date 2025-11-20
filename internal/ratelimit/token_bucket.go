package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// TokenBucket implements the token bucket rate limiting algorithm
type TokenBucket struct {
	// Configuration
	capacity     int           // Maximum number of tokens
	refillRate   int           // Tokens added per interval
	refillPeriod time.Duration // How often to add tokens

	// State
	tokens     int
	lastRefill time.Time
	mu         sync.Mutex

	// Metrics
	totalRequests  int64
	deniedRequests int64
	waitTime       time.Duration
}

// NewTokenBucket creates a new token bucket rate limiter
// capacity: maximum burst size
// refillRate: number of tokens added per refillPeriod
// refillPeriod: how often tokens are added
func NewTokenBucket(capacity, refillRate int, refillPeriod time.Duration) *TokenBucket {
	return &TokenBucket{
		capacity:     capacity,
		refillRate:   refillRate,
		refillPeriod: refillPeriod,
		tokens:       capacity, // Start with full bucket
		lastRefill:   time.Now(),
	}
}

// Allow checks if a request is allowed and consumes a token if available
// Returns true if allowed, false if rate limit exceeded
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	tb.totalRequests++

	if tb.tokens > 0 {
		tb.tokens--
		return true
	}

	tb.deniedRequests++
	return false
}

// AllowN checks if n tokens are available and consumes them if so
func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	tb.totalRequests++

	if tb.tokens >= n {
		tb.tokens -= n
		return true
	}

	tb.deniedRequests++
	return false
}

// Wait blocks until a token is available
func (tb *TokenBucket) Wait(ctx context.Context) error {
	for {
		if tb.Allow() {
			return nil
		}

		// Calculate wait time until next refill
		tb.mu.Lock()
		nextRefill := tb.lastRefill.Add(tb.refillPeriod)
		waitDuration := time.Until(nextRefill)
		tb.mu.Unlock()

		// Wait for next refill or context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
			// Try again after refill
		}
	}
}

// WaitN blocks until n tokens are available
func (tb *TokenBucket) WaitN(ctx context.Context, n int) error {
	startTime := time.Now()

	for {
		if tb.AllowN(n) {
			tb.mu.Lock()
			tb.waitTime += time.Since(startTime)
			tb.mu.Unlock()
			return nil
		}

		// Calculate wait time until enough tokens
		tb.mu.Lock()
		tokensNeeded := n - tb.tokens
		refillsNeeded := (tokensNeeded + tb.refillRate - 1) / tb.refillRate
		nextRefill := tb.lastRefill.Add(time.Duration(refillsNeeded) * tb.refillPeriod)
		waitDuration := time.Until(nextRefill)
		tb.mu.Unlock()

		// Wait for tokens or context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
			// Try again after refill
		}
	}
}

// refill adds tokens based on elapsed time
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)

	// Calculate how many refill periods have passed
	periods := int(elapsed / tb.refillPeriod)
	if periods > 0 {
		// Add tokens for each period
		tokensToAdd := periods * tb.refillRate
		tb.tokens = min(tb.tokens+tokensToAdd, tb.capacity)

		// Update last refill time
		tb.lastRefill = tb.lastRefill.Add(time.Duration(periods) * tb.refillPeriod)
	}
}

// GetMetrics returns rate limiter metrics
func (tb *TokenBucket) GetMetrics() Metrics {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	return Metrics{
		TotalRequests:  tb.totalRequests,
		DeniedRequests: tb.deniedRequests,
		CurrentTokens:  tb.tokens,
		Capacity:       tb.capacity,
		RefillRate:     tb.refillRate,
		AverageWait:    tb.getAverageWait(),
	}
}

func (tb *TokenBucket) getAverageWait() time.Duration {
	if tb.totalRequests == 0 {
		return 0
	}
	return tb.waitTime / time.Duration(tb.totalRequests)
}

// Metrics contains rate limiter statistics
type Metrics struct {
	TotalRequests  int64
	DeniedRequests int64
	CurrentTokens  int
	Capacity       int
	RefillRate     int
	AverageWait    time.Duration
}

// LogMetrics logs the current metrics
func (m Metrics) LogMetrics(name string) {
	deniedPercent := float64(0)
	if m.TotalRequests > 0 {
		deniedPercent = float64(m.DeniedRequests) / float64(m.TotalRequests) * 100
	}

	log.Debug().
		Str("limiter", name).
		Int64("total_requests", m.TotalRequests).
		Int64("denied_requests", m.DeniedRequests).
		Float64("denied_percent", deniedPercent).
		Int("current_tokens", m.CurrentTokens).
		Int("capacity", m.Capacity).
		Dur("avg_wait", m.AverageWait).
		Msg("Rate limiter metrics")
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

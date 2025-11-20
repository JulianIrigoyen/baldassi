package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTokenBucketBurstCapacity(t *testing.T) {
	tests := []struct {
		name        string
		capacity    int
		refillRate  int
		burstCount  int
		shouldAllow int
	}{
		{
			name:        "small bucket burst",
			capacity:    5,
			refillRate:  5,
			burstCount:  5,
			shouldAllow: 5,
		},
		{
			name:        "large bucket burst",
			capacity:    20,
			refillRate:  20,
			burstCount:  15,
			shouldAllow: 15,
		},
		{
			name:        "exceeds capacity",
			capacity:    3,
			refillRate:  3,
			burstCount:  5,
			shouldAllow: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("\n=== Testing Scenario: %s ===", tt.name)
			t.Logf("Bucket Configuration:")
			t.Logf("  Capacity:    %d tokens", tt.capacity)
			t.Logf("  Refill Rate: %d tokens/second", tt.refillRate)
			t.Logf("  Burst Test:  %d requests", tt.burstCount)

			limiter := NewTokenBucket(
				tt.capacity,
				tt.refillRate,
				1*time.Second,
			)

			allowed := 0
			denied := 0

			t.Logf("\nBurst Request Results:")
			startTime := time.Now()

			for i := 0; i < tt.burstCount; i++ {
				if limiter.Allow() {
					allowed++
					t.Logf("  Request %d: ✅ ALLOWED (at %v)", i+1, time.Since(startTime))
				} else {
					denied++
					t.Logf("  Request %d: ❌ DENIED (at %v)", i+1, time.Since(startTime))
				}
			}

			t.Logf("\nVerification:")
			t.Logf("  Expected Allowed: %d", tt.shouldAllow)
			t.Logf("  Actual Allowed:   %d", allowed)
			t.Logf("  Actual Denied:    %d", denied)

			assert.Equal(t, tt.shouldAllow, allowed,
				"Burst capacity mismatch for %s", tt.name)

			if allowed == tt.shouldAllow {
				t.Logf("  ✅ PASS - Burst capacity correct")
			} else {
				t.Logf("  ❌ FAIL - Burst capacity incorrect")
			}
		})
	}
}

func TestTokenBucketRefill(t *testing.T) {
	t.Log("\n=== Testing Token Refill Behavior ===")

	limiter := NewTokenBucket(
		3,               // capacity
		3,               // refill rate
		100*time.Millisecond, // refill period (fast for testing)
	)

	ctx := context.Background()

	t.Logf("Configuration:")
	t.Logf("  Capacity:    3 tokens")
	t.Logf("  Refill:      3 tokens per 100ms")

	// Exhaust tokens
	t.Log("\nStep 1: Exhaust all tokens")
	for i := 0; i < 3; i++ {
		err := limiter.Wait(ctx)
		assert.NoError(t, err)
		t.Logf("  Request %d: ✅ Consumed token", i+1)
	}

	metrics := limiter.GetMetrics()
	t.Logf("  Tokens remaining: %d", metrics.CurrentTokens)
	assert.Equal(t, 0, metrics.CurrentTokens, "All tokens should be consumed")

	// Try one more - should wait for refill
	t.Log("\nStep 2: Request when empty (should wait ~100ms)")
	start := time.Now()
	err := limiter.Wait(ctx)
	waitTime := time.Since(start)
	assert.NoError(t, err)

	t.Logf("  Request 4: ✅ Allowed after %v wait", waitTime)
	t.Logf("  Expected wait: ~100ms")

	// Allow some tolerance for timing
	assert.Greater(t, waitTime, 90*time.Millisecond, "Should wait for refill")
	assert.Less(t, waitTime, 150*time.Millisecond, "Should not wait too long")

	if waitTime > 90*time.Millisecond && waitTime < 150*time.Millisecond {
		t.Logf("  ✅ PASS - Refill timing correct")
	} else {
		t.Logf("  ❌ FAIL - Refill timing incorrect")
	}
}

func TestTokenBucketConcurrency(t *testing.T) {
	t.Log("\n=== Testing Concurrent Access ===")

	limiter := NewTokenBucket(
		10,              // capacity
		10,              // refill rate
		1*time.Second,   // refill period
	)

	t.Logf("Configuration:")
	t.Logf("  Capacity:    10 tokens")
	t.Logf("  Concurrency: 20 goroutines")

	// Run concurrent requests
	results := make(chan bool, 20)

	t.Log("\nLaunching 20 concurrent requests...")
	for i := 0; i < 20; i++ {
		go func(id int) {
			allowed := limiter.Allow()
			results <- allowed
		}(i)
	}

	// Collect results
	allowed := 0
	denied := 0
	for i := 0; i < 20; i++ {
		if <-results {
			allowed++
		} else {
			denied++
		}
	}

	t.Logf("\nResults:")
	t.Logf("  Allowed: %d requests", allowed)
	t.Logf("  Denied:  %d requests", denied)
	t.Logf("  Total:   %d requests", allowed+denied)

	assert.Equal(t, 10, allowed, "Should allow exactly capacity amount")
	assert.Equal(t, 10, denied, "Should deny requests over capacity")

	if allowed == 10 && denied == 10 {
		t.Logf("  ✅ PASS - Thread-safe under concurrent load")
	} else {
		t.Logf("  ❌ FAIL - Concurrency issue detected")
	}
}

func TestTokenBucketMetrics(t *testing.T) {
	t.Log("\n=== Testing Metrics Collection ===")

	limiter := NewTokenBucket(
		5,               // capacity
		5,               // refill rate
		1*time.Second,   // refill period
	)

	t.Logf("Configuration:")
	t.Logf("  Capacity: 5 tokens")

	// Make some requests
	t.Log("\nMaking 8 requests:")
	for i := 0; i < 8; i++ {
		limiter.Allow()
	}

	metrics := limiter.GetMetrics()

	t.Logf("\nMetrics Report:")
	t.Logf("  Total Requests:  %d", metrics.TotalRequests)
	t.Logf("  Denied Requests: %d", metrics.DeniedRequests)
	t.Logf("  Current Tokens:  %d", metrics.CurrentTokens)
	t.Logf("  Capacity:        %d", metrics.Capacity)
	t.Logf("  Refill Rate:     %d tokens/second", metrics.RefillRate)

	assert.Equal(t, int64(8), metrics.TotalRequests, "Should track all requests")
	assert.Equal(t, int64(3), metrics.DeniedRequests, "Should track denied requests")

	deniedPercent := float64(metrics.DeniedRequests) / float64(metrics.TotalRequests) * 100
	t.Logf("  Denial Rate:     %.1f%%", deniedPercent)

	if metrics.TotalRequests == 8 && metrics.DeniedRequests == 3 {
		t.Logf("\n  ✅ PASS - Metrics correctly tracked")
	} else {
		t.Logf("\n  ❌ FAIL - Metrics tracking error")
	}
}

func TestTokenBucketContextCancellation(t *testing.T) {
	t.Log("\n=== Testing Context Cancellation ===")

	limiter := NewTokenBucket(
		1,               // capacity
		1,               // refill rate
		1*time.Second,   // refill period
	)

	// Exhaust the token
	limiter.Allow()

	t.Log("Step 1: Token exhausted")
	t.Log("Step 2: Creating context with 50ms timeout")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := limiter.Wait(ctx)
	duration := time.Since(start)

	t.Logf("Step 3: Wait returned after %v", duration)

	assert.Error(t, err, "Should return error on context cancellation")
	assert.Less(t, duration, 100*time.Millisecond, "Should cancel quickly")

	if err != nil && duration < 100*time.Millisecond {
		t.Logf("  ✅ PASS - Context cancellation handled correctly")
		t.Logf("  Error: %v", err)
	} else {
		t.Logf("  ❌ FAIL - Context cancellation not working")
	}
}

func BenchmarkTokenBucketAllow(b *testing.B) {
	limiter := NewTokenBucket(
		1000,            // Large capacity
		1000,            // High refill rate
		1*time.Second,
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

func BenchmarkTokenBucketWait(b *testing.B) {
	limiter := NewTokenBucket(
		1000,            // Large capacity
		1000,            // High refill rate
		1*time.Second,
	)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = limiter.Wait(ctx)
	}
}
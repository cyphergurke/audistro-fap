package api

import (
	"testing"
	"time"
)

func TestEndpointRateLimiterCleansUpIdleKeys(t *testing.T) {
	limiter := newEndpointRateLimiter(2, 2)
	if limiter == nil {
		t.Fatal("expected limiter")
	}

	now := time.Unix(1_700_000_000, 0)
	limiter.now = func() time.Time { return now }
	limiter.bucketTTL = 2 * time.Second
	limiter.cleanupInterval = 1 * time.Second
	limiter.maxKeys = 10

	if !limiter.Allow("device:a") {
		t.Fatal("expected first key to pass")
	}
	if got := len(limiter.buckets); got != 1 {
		t.Fatalf("expected 1 bucket, got %d", got)
	}

	now = now.Add(3 * time.Second)
	if !limiter.Allow("device:b") {
		t.Fatal("expected second key to pass after cleanup")
	}
	if got := len(limiter.buckets); got != 1 {
		t.Fatalf("expected stale bucket cleanup, got %d buckets", got)
	}
	if _, ok := limiter.buckets["device:a"]; ok {
		t.Fatal("expected stale key to be evicted")
	}
}

func TestEndpointRateLimiterMaxKeysCapsCardinality(t *testing.T) {
	limiter := newEndpointRateLimiter(1, 1)
	if limiter == nil {
		t.Fatal("expected limiter")
	}

	now := time.Unix(1_700_000_000, 0)
	limiter.now = func() time.Time { return now }
	limiter.bucketTTL = 10 * time.Minute
	limiter.cleanupInterval = 10 * time.Minute
	limiter.maxKeys = 2

	if !limiter.Allow("device:1") {
		t.Fatal("expected device:1 allowed")
	}
	if !limiter.Allow("device:2") {
		t.Fatal("expected device:2 allowed")
	}
	if limiter.Allow("device:3") {
		t.Fatal("expected device:3 to be denied due max key cap")
	}
	if got := len(limiter.buckets); got != 2 {
		t.Fatalf("expected key cap to stay at 2, got %d", got)
	}

	now = now.Add(11 * time.Minute)
	if !limiter.Allow("device:3") {
		t.Fatal("expected device:3 allowed after stale eviction")
	}
}

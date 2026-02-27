package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultChallengeRateRPS   = 2.0
	defaultChallengeRateBurst = 4
	defaultTokenRateRPS       = 2.0
	defaultTokenRateBurst     = 4
	defaultKeyRateRPS         = 5.0
	defaultKeyRateBurst       = 10
	defaultLimiterBucketTTL   = 10 * time.Minute
	defaultLimiterCleanupIntv = 1 * time.Minute
	defaultLimiterMaxKeys     = 20_000
)

type endpointRateLimiter struct {
	mu              sync.Mutex
	rps             float64
	burst           float64
	now             func() time.Time
	buckets         map[string]rateBucket
	bucketTTL       time.Duration
	cleanupInterval time.Duration
	maxKeys         int
	nextCleanupAt   time.Time
}

type rateBucket struct {
	tokens float64
	last   time.Time
}

func newEndpointRateLimiter(rps float64, burst int) *endpointRateLimiter {
	if rps <= 0 || burst <= 0 {
		return nil
	}
	return &endpointRateLimiter{
		rps:             rps,
		burst:           float64(burst),
		now:             time.Now,
		buckets:         make(map[string]rateBucket, 256),
		bucketTTL:       defaultLimiterBucketTTL,
		cleanupInterval: defaultLimiterCleanupIntv,
		maxKeys:         defaultLimiterMaxKeys,
	}
}

func (l *endpointRateLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	if strings.TrimSpace(key) == "" {
		key = "anonymous"
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.nextCleanupAt.After(now) {
		l.cleanupLocked(now)
		l.nextCleanupAt = now.Add(l.cleanupInterval)
	}

	bucket, ok := l.buckets[key]
	if !ok {
		if l.maxKeys > 0 && len(l.buckets) >= l.maxKeys {
			// Hard-cap cardinality to prevent unbounded memory growth.
			l.cleanupLocked(now)
			if len(l.buckets) >= l.maxKeys {
				return false
			}
		}
		// First request consumes one token immediately.
		l.buckets[key] = rateBucket{
			tokens: l.burst - 1,
			last:   now,
		}
		return true
	}

	elapsedSeconds := now.Sub(bucket.last).Seconds()
	if elapsedSeconds > 0 {
		bucket.tokens += elapsedSeconds * l.rps
		if bucket.tokens > l.burst {
			bucket.tokens = l.burst
		}
	}
	bucket.last = now

	if bucket.tokens < 1 {
		l.buckets[key] = bucket
		return false
	}
	bucket.tokens -= 1
	l.buckets[key] = bucket
	return true
}

func (l *endpointRateLimiter) cleanupLocked(now time.Time) {
	if l.bucketTTL <= 0 {
		return
	}
	for key, bucket := range l.buckets {
		idle := now.Sub(bucket.last)
		if idle < 0 {
			continue
		}
		if idle > l.bucketTTL {
			delete(l.buckets, key)
		}
	}
}

func rateLimitClientKey(r *http.Request) string {
	deviceID := strings.TrimSpace(readDeviceCookie(r))
	if deviceID != "" {
		return "device:" + deviceID
	}
	ip := extractClientIP(r)
	if ip != "" {
		return "ip:" + ip
	}
	return "anonymous"
}

func extractClientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		first := strings.TrimSpace(strings.Split(xff, ",")[0])
		if first != "" {
			return first
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (a *API) withRateLimit(limiter *endpointRateLimiter, next http.HandlerFunc) http.HandlerFunc {
	if limiter == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(rateLimitClientKey(r)) {
			writeError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next(w, r)
	}
}

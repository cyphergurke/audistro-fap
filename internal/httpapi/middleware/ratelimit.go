package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/fap/internal/security"
)

type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
}

type ChallengeRateLimiter struct {
	mu sync.Mutex

	now func() time.Time

	rateIP  float64
	burstIP float64
	ip      map[string]*tokenBucket

	rateSubject  float64
	burstSubject float64
	subject      map[string]*tokenBucket
}

func NewChallengeRateLimiter() *ChallengeRateLimiter {
	return &ChallengeRateLimiter{
		now:          time.Now,
		rateIP:       security.ChallengeRatePerIPPerSecond,
		burstIP:      security.ChallengeBurstPerIP,
		ip:           make(map[string]*tokenBucket),
		rateSubject:  security.ChallengeRatePerSubjectPerSecond,
		burstSubject: security.ChallengeBurstPerSubject,
		subject:      make(map[string]*tokenBucket),
	}
}

func (l *ChallengeRateLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := clientIP(r)
		subject := challengeSubject(r)

		now := l.now()
		if !l.consume(l.ip, clientIP, l.rateIP, l.burstIP, now) {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if !l.consume(l.subject, subject, l.rateSubject, l.burstSubject, now) {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (l *ChallengeRateLimiter) consume(store map[string]*tokenBucket, key string, rate float64, burst float64, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := store[key]
	if !ok {
		b = &tokenBucket{
			tokens:     burst,
			lastRefill: now,
		}
		store[key] = b
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * rate
		if b.tokens > burst {
			b.tokens = burst
		}
		b.lastRefill = now
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func challengeSubject(r *http.Request) string {
	if r.Body == nil {
		return ""
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var payload struct {
		Subject string `json:"subject"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload.Subject
}

func clientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		first := strings.TrimSpace(parts[0])
		if first != "" {
			return first
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

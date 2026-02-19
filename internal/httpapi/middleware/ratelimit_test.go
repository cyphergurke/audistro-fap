package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChallengeRateLimitTriggers(t *testing.T) {
	rl := NewChallengeRateLimiter()
	rl.rateIP = 1
	rl.burstIP = 1
	rl.rateSubject = 1
	rl.burstSubject = 1
	rl.now = func() time.Time {
		return time.Unix(1_700_000_000, 0)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.Wrap(next)

	req1 := httptest.NewRequest(http.MethodPost, "/fap/challenge", strings.NewReader(`{"subject":"alice"}`))
	req1.RemoteAddr = "10.0.0.1:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected first request 200, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/fap/challenge", strings.NewReader(`{"subject":"alice"}`))
	req2.RemoteAddr = "10.0.0.1:12346"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request 429, got %d", rec2.Code)
	}
}

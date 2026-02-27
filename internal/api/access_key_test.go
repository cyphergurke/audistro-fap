package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fap/internal/hlskey"
	devtoken "fap/internal/token"
)

func TestAccessEndpointNotImplementedWhenDevModeDisabled(t *testing.T) {
	issuer, err := devtoken.NewIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	api := NewWithOptions(nil, Options{
		DevMode:           false,
		AccessTokenIssuer: issuer,
		HLSKeyDerive: func(assetID string) [16]byte {
			return hlskey.DevAES128Key([]byte("0123456789abcdef0123456789abcdef"), assetID)
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/access/asset-dev-1", nil)
	api.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	var out errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Error != "dev_mode_disabled" {
		t.Fatalf("expected dev_mode_disabled, got %q", out.Error)
	}
}

func TestHLSKeyEndpointWithValidToken(t *testing.T) {
	issuer, err := devtoken.NewIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, bytes.NewReader(bytes.Repeat([]byte{0x42}, 16)))
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	token, _, err := issuer.Issue("asset-dev-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	api := NewWithOptions(nil, Options{
		DevMode:           true,
		AccessTokenIssuer: issuer,
		HLSKeyDerive: func(assetID string) [16]byte {
			return hlskey.DevAES128Key([]byte("0123456789abcdef0123456789abcdef"), assetID)
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/hls/asset-dev-1/key", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	api.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("unexpected content-type: %s", got)
	}
	if rec.Body.Len() != 16 {
		t.Fatalf("expected 16-byte key, got %d", rec.Body.Len())
	}
}

func TestHLSKeyEndpointMissingToken(t *testing.T) {
	issuer, err := devtoken.NewIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	api := NewWithOptions(nil, Options{
		DevMode:           true,
		AccessTokenIssuer: issuer,
		HLSKeyDerive: func(assetID string) [16]byte {
			return hlskey.DevAES128Key([]byte("0123456789abcdef0123456789abcdef"), assetID)
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/hls/asset-dev-1/key", nil)
	api.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	var out errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Error != "unauthorized" {
		t.Fatalf("expected unauthorized, got %q", out.Error)
	}
}

type fakeIssuer struct{}

func (fakeIssuer) Issue(_ string, _ time.Time) (string, int64, error) {
	return "", 0, errors.New("not implemented")
}

func (fakeIssuer) Validate(_ string, _ string, _ time.Time) error {
	return errors.New("bad token")
}

func TestHLSKeyEndpointInvalidToken(t *testing.T) {
	api := NewWithOptions(nil, Options{
		DevMode:           true,
		AccessTokenIssuer: fakeIssuer{},
		HLSKeyDerive: func(assetID string) [16]byte {
			return hlskey.DevAES128Key([]byte("0123456789abcdef0123456789abcdef"), assetID)
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/hls/asset-dev-1/key", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	api.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	var out errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Error != "unauthorized" {
		t.Fatalf("expected unauthorized, got %q", out.Error)
	}
}

func TestHLSCORSMiddlewareAllowedOrigin(t *testing.T) {
	issuer, err := devtoken.NewIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, bytes.NewReader(bytes.Repeat([]byte{0x43}, 16)))
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	token, _, err := issuer.Issue("asset-dev-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	api := NewWithOptions(nil, Options{
		DevMode:           true,
		AccessTokenIssuer: issuer,
		HLSKeyDerive: func(assetID string) [16]byte {
			return hlskey.DevAES128Key([]byte("0123456789abcdef0123456789abcdef"), assetID)
		},
	})
	router := NewHLSCORSMiddleware([]string{"https://player.example"}, false)(api.Router())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/hls/asset-dev-1/key", nil)
	req.Header.Set("Origin", "https://player.example")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://player.example" {
		t.Fatalf("unexpected allow origin: %q", got)
	}
}

func TestHLSCORSMiddlewarePreflight(t *testing.T) {
	router := NewHLSCORSMiddleware([]string{"https://player.example"}, true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler should not be called for preflight")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/hls/asset-dev-1/key", nil)
	req.Header.Set("Origin", "https://player.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://player.example" {
		t.Fatalf("unexpected allow origin: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("unexpected allow credentials: %q", got)
	}
}

func TestHLSCORSMiddlewarePreflightDisallowed(t *testing.T) {
	router := NewHLSCORSMiddleware([]string{"https://player.example"}, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler should not be called for disallowed preflight")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/hls/asset-dev-1/key", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

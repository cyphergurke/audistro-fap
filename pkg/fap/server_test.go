package fap

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewServerAndHealthz(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "token_secret")
	if err := os.WriteFile(secretPath, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg := Config{
		HTTPAddr:             ":0",
		DBPath:               filepath.Join(t.TempDir(), "fap.sqlite"),
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MasterKeyHex:         "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		WebhookSecret:        "secret",
		TokenSecretPath:      secretPath,
		AdminToken:           "admin-secret",
		InternalAllowedCIDRs: "127.0.0.1/32",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		MaxAccessAmountMSat:  50_000_000,
		AccessMinutesPerPay:  10,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = srv.Close() }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

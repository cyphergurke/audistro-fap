package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestNewAppComponents(t *testing.T) {
	cfg := Config{
		DBPath:               filepath.Join(t.TempDir(), "fap.sqlite"),
		MasterKey:            bytes.Repeat([]byte{0x11}, 32),
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WebhookSecret:        "secret",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		MaxAccessAmountMSat:  50_000_000,
		TokenSecret:          []byte("0123456789abcdef0123456789abcdef"),
	}
	components, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer components.Repository.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	components.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

package config

import (
	"strings"
	"testing"
)

func TestLoadMissingRequiredEnvReturnsError(t *testing.T) {
	t.Setenv("FAP_ISSUER_PRIVKEY_HEX", "abc123")
	t.Setenv("FAP_DB_PATH", "/tmp/fap.db")
	t.Setenv("FAP_LNBITS_BASE_URL", "http://localhost:3007")
	t.Setenv("FAP_LNBITS_INVOICE_API_KEY", "invoice-key")
	t.Setenv("FAP_LNBITS_READONLY_API_KEY", "")
	t.Setenv("FAP_WEBHOOK_SECRET", "secret")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "FAP_LNBITS_READONLY_API_KEY") {
		t.Fatalf("expected missing env error for FAP_LNBITS_READONLY_API_KEY, got: %v", err)
	}
}

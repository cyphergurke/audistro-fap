package fap

import (
	"path/filepath"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "token_secret")
	cfg := Config{
		HTTPAddr:             ":8080",
		DBPath:               "./tmp.db",
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MasterKeyHex:         "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		WebhookSecret:        "secret",
		TokenSecretPath:      secretPath,
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		MaxAccessAmountMSat:  50_000_000,
		AccessMinutesPerPay:  10,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if _, err := cfg.MasterKey(); err != nil {
		t.Fatalf("MasterKey: %v", err)
	}
}

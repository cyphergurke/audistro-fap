package fap

import (
	"os"
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
		AdminToken:           "admin-secret",
		InternalAllowedCIDRs: "127.0.0.1/32,172.16.0.0/12",
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

func TestLoadFromEnvDefaultsOpenAPIValidationToEnabled(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "token_secret")
	if err := os.WriteFile(secretPath, []byte("0123456789abcdef"), 0o600); err != nil {
		t.Fatalf("write token secret: %v", err)
	}
	t.Setenv("FAP_DB_PATH", "./tmp.db")
	t.Setenv("FAP_ISSUER_PRIVKEY_HEX", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FAP_MASTER_KEY_HEX", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	t.Setenv("FAP_WEBHOOK_SECRET", "secret")
	t.Setenv("FAP_TOKEN_SECRET_PATH", secretPath)
	t.Setenv("FAP_ADMIN_TOKEN", "admin-secret")
	t.Setenv("FAP_INTERNAL_ALLOWED_CIDRS", "127.0.0.1/32")
	t.Setenv("FAP_DISABLE_OPENAPI_VALIDATION", "")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.DisableOpenAPIValidation {
		t.Fatal("expected openapi validation to be enabled by default")
	}
}

func TestLoadFromEnvProdRequiresExplicitInternalCIDRs(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "token_secret")
	if err := os.WriteFile(secretPath, []byte("0123456789abcdef"), 0o600); err != nil {
		t.Fatalf("write token secret: %v", err)
	}
	t.Setenv("FAP_DB_PATH", "./tmp.db")
	t.Setenv("FAP_ISSUER_PRIVKEY_HEX", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FAP_MASTER_KEY_HEX", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	t.Setenv("FAP_WEBHOOK_SECRET", "secret")
	t.Setenv("FAP_TOKEN_SECRET_PATH", secretPath)
	t.Setenv("FAP_ADMIN_TOKEN", "admin-secret")
	t.Setenv("FAP_DEV_MODE", "false")
	t.Setenv("FAP_INTERNAL_ALLOWED_CIDRS", "")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "FAP_INTERNAL_ALLOWED_CIDRS is required when FAP_DEV_MODE=false" {
		t.Fatalf("expected explicit internal cidr error, got %v", err)
	}
}

func TestLoadFromEnvDevAllowsImplicitInternalCIDRs(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "token_secret")
	if err := os.WriteFile(secretPath, []byte("0123456789abcdef"), 0o600); err != nil {
		t.Fatalf("write token secret: %v", err)
	}
	t.Setenv("FAP_DB_PATH", "./tmp.db")
	t.Setenv("FAP_ISSUER_PRIVKEY_HEX", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FAP_MASTER_KEY_HEX", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	t.Setenv("FAP_WEBHOOK_SECRET", "secret")
	t.Setenv("FAP_TOKEN_SECRET_PATH", secretPath)
	t.Setenv("FAP_ADMIN_TOKEN", "admin-secret")
	t.Setenv("FAP_DEV_MODE", "true")
	_ = os.Unsetenv("FAP_INTERNAL_ALLOWED_CIDRS")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.InternalAllowedCIDRs == "" {
		t.Fatalf("expected dev fallback internal cidrs")
	}
}

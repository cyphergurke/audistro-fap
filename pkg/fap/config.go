package fap

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr                         string
	DBPath                           string
	IssuerPrivKeyHex                 string
	MasterKeyHex                     string
	WebhookSecret                    string
	TokenTTLSeconds                  int64
	InvoiceExpirySeconds             int64
	MaxAccessAmountMSat              int64
	AccessMinutesPerPay              int64
	WebhookEventRetentionSeconds     int64
	WebhookEventPruneIntervalSeconds int64
	DevMode                          bool
	ExposeBolt11InList               bool
	DeviceCookieSecure               bool
	TokenSecretPath                  string
	EnableCORS                       bool
	CORSAllowedOrigins               []string
	CORSAllowCredentials             bool
	AdminToken                       string
	InternalAllowedCIDRs             string
}

func LoadFromEnv() (Config, error) {
	cfg := Config{
		HTTPAddr:                         envOrDefault("FAP_HTTP_ADDR", ":8080"),
		TokenTTLSeconds:                  900,
		InvoiceExpirySeconds:             900,
		MaxAccessAmountMSat:              50_000_000,
		AccessMinutesPerPay:              10,
		WebhookEventRetentionSeconds:     604800,
		WebhookEventPruneIntervalSeconds: 300,
	}

	var err error
	cfg.DBPath, err = requiredEnv("FAP_DB_PATH")
	if err != nil {
		return Config{}, err
	}
	cfg.IssuerPrivKeyHex, err = requiredEnv("FAP_ISSUER_PRIVKEY_HEX")
	if err != nil {
		return Config{}, err
	}
	cfg.MasterKeyHex, err = requiredEnv("FAP_MASTER_KEY_HEX")
	if err != nil {
		return Config{}, err
	}
	cfg.WebhookSecret, err = requiredEnv("FAP_WEBHOOK_SECRET")
	if err != nil {
		return Config{}, err
	}
	cfg.TokenSecretPath, err = requiredEnv("FAP_TOKEN_SECRET_PATH")
	if err != nil {
		return Config{}, err
	}
	cfg.AdminToken, err = requiredEnv("FAP_ADMIN_TOKEN")
	if err != nil {
		return Config{}, err
	}

	cfg.TokenTTLSeconds, err = envInt64("FAP_TOKEN_TTL_SECONDS", cfg.TokenTTLSeconds)
	if err != nil {
		return Config{}, err
	}
	cfg.InvoiceExpirySeconds, err = envInt64("FAP_INVOICE_EXPIRY_SECONDS", cfg.InvoiceExpirySeconds)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxAccessAmountMSat, err = envInt64("FAP_MAX_ACCESS_AMOUNT_MSAT", cfg.MaxAccessAmountMSat)
	if err != nil {
		return Config{}, err
	}
	cfg.AccessMinutesPerPay, err = envInt64("FAP_ACCESS_MINUTES_PER_PAYMENT", cfg.AccessMinutesPerPay)
	if err != nil {
		return Config{}, err
	}
	cfg.WebhookEventRetentionSeconds, err = envInt64("FAP_WEBHOOK_EVENT_RETENTION_SECONDS", cfg.WebhookEventRetentionSeconds)
	if err != nil {
		return Config{}, err
	}
	cfg.WebhookEventPruneIntervalSeconds, err = envInt64("FAP_WEBHOOK_EVENT_PRUNE_INTERVAL_SECONDS", cfg.WebhookEventPruneIntervalSeconds)
	if err != nil {
		return Config{}, err
	}
	cfg.DevMode, err = envBool("FAP_DEV_MODE", false)
	if err != nil {
		return Config{}, err
	}
	cfg.ExposeBolt11InList, err = envBool("FAP_EXPOSE_BOLT11_IN_LIST", false)
	if err != nil {
		return Config{}, err
	}
	cfg.DeviceCookieSecure, err = envBool("FAP_DEVICE_COOKIE_SECURE", false)
	if err != nil {
		return Config{}, err
	}
	cfg.EnableCORS, err = envBool("FAP_ENABLE_CORS", false)
	if err != nil {
		return Config{}, err
	}
	cfg.CORSAllowCredentials, err = envBool("FAP_CORS_ALLOW_CREDENTIALS", false)
	if err != nil {
		return Config{}, err
	}
	cfg.CORSAllowedOrigins = envCSV("FAP_CORS_ALLOWED_ORIGINS")
	cfg.InternalAllowedCIDRs = envOrDefault("FAP_INTERNAL_ALLOWED_CIDRS", "127.0.0.1/32,172.16.0.0/12")

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return fmt.Errorf("FAP_HTTP_ADDR is required")
	}
	if strings.TrimSpace(c.DBPath) == "" {
		return fmt.Errorf("FAP_DB_PATH is required")
	}
	if strings.TrimSpace(c.IssuerPrivKeyHex) == "" {
		return fmt.Errorf("FAP_ISSUER_PRIVKEY_HEX is required")
	}
	if strings.TrimSpace(c.MasterKeyHex) == "" {
		return fmt.Errorf("FAP_MASTER_KEY_HEX is required")
	}
	if strings.TrimSpace(c.WebhookSecret) == "" {
		return fmt.Errorf("FAP_WEBHOOK_SECRET is required")
	}
	if strings.TrimSpace(c.TokenSecretPath) == "" {
		return fmt.Errorf("FAP_TOKEN_SECRET_PATH is required")
	}
	if strings.TrimSpace(c.AdminToken) == "" {
		return fmt.Errorf("FAP_ADMIN_TOKEN is required")
	}
	if c.TokenTTLSeconds <= 0 {
		return fmt.Errorf("FAP_TOKEN_TTL_SECONDS must be > 0")
	}
	if c.InvoiceExpirySeconds <= 0 {
		return fmt.Errorf("FAP_INVOICE_EXPIRY_SECONDS must be > 0")
	}
	if c.MaxAccessAmountMSat <= 0 {
		return fmt.Errorf("FAP_MAX_ACCESS_AMOUNT_MSAT must be > 0")
	}
	if c.AccessMinutesPerPay <= 0 {
		return fmt.Errorf("FAP_ACCESS_MINUTES_PER_PAYMENT must be > 0")
	}
	if c.WebhookEventRetentionSeconds < 0 {
		return fmt.Errorf("FAP_WEBHOOK_EVENT_RETENTION_SECONDS must be >= 0")
	}
	if c.WebhookEventPruneIntervalSeconds < 0 {
		return fmt.Errorf("FAP_WEBHOOK_EVENT_PRUNE_INTERVAL_SECONDS must be >= 0")
	}
	issuerRaw, err := hex.DecodeString(c.IssuerPrivKeyHex)
	if err != nil {
		return fmt.Errorf("FAP_ISSUER_PRIVKEY_HEX invalid hex: %w", err)
	}
	if len(issuerRaw) != 32 {
		return fmt.Errorf("FAP_ISSUER_PRIVKEY_HEX must decode to 32 bytes")
	}
	masterRaw, err := hex.DecodeString(c.MasterKeyHex)
	if err != nil {
		return fmt.Errorf("FAP_MASTER_KEY_HEX invalid hex: %w", err)
	}
	if len(masterRaw) != 32 {
		return fmt.Errorf("FAP_MASTER_KEY_HEX must decode to 32 bytes")
	}
	if err := validateCIDRList(c.InternalAllowedCIDRs); err != nil {
		return fmt.Errorf("FAP_INTERNAL_ALLOWED_CIDRS invalid: %w", err)
	}
	return nil
}

func (c Config) MasterKey() ([]byte, error) {
	raw, err := hex.DecodeString(c.MasterKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode master key: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes")
	}
	return raw, nil
}

func requiredEnv(key string) (string, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("missing required env: %s", key)
	}
	return v, nil
}

func envOrDefault(key string, fallback string) string {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func envInt64(key string, fallback int64) (int64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer env %s: %w", key, err)
	}
	return n, nil
}

func envBool(key string, fallback bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid boolean env %s: %w", key, err)
	}
	return b, nil
}

func envCSV(key string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func validateCIDRList(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	for _, part := range strings.Split(trimmed, ",") {
		cidr := strings.TrimSpace(part)
		if cidr == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return err
		}
	}
	return nil
}

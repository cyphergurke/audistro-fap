package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr             string
	IssuerPrivKeyHex     string
	DBPath               string
	LNBitsBaseURL        string
	LNBitsInvoiceAPIKey  string
	LNBitsReadonlyAPIKey string
	WebhookSecret        string
	TokenTTLSeconds      int
	InvoiceExpirySeconds int
	PriceMSATDefault     int
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:             getEnvDefault("FAP_HTTP_ADDR", ":8080"),
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		PriceMSATDefault:     100000,
	}

	var err error

	cfg.IssuerPrivKeyHex, err = getRequiredEnv("FAP_ISSUER_PRIVKEY_HEX")
	if err != nil {
		return Config{}, err
	}
	cfg.DBPath, err = getRequiredEnv("FAP_DB_PATH")
	if err != nil {
		return Config{}, err
	}
	cfg.LNBitsBaseURL, err = getRequiredEnv("FAP_LNBITS_BASE_URL")
	if err != nil {
		return Config{}, err
	}
	cfg.LNBitsInvoiceAPIKey, err = getRequiredEnv("FAP_LNBITS_INVOICE_API_KEY")
	if err != nil {
		return Config{}, err
	}
	cfg.LNBitsReadonlyAPIKey, err = getRequiredEnv("FAP_LNBITS_READONLY_API_KEY")
	if err != nil {
		return Config{}, err
	}
	cfg.WebhookSecret, err = getRequiredEnv("FAP_WEBHOOK_SECRET")
	if err != nil {
		return Config{}, err
	}

	cfg.TokenTTLSeconds, err = getEnvInt("FAP_TOKEN_TTL_SECONDS", cfg.TokenTTLSeconds)
	if err != nil {
		return Config{}, err
	}
	cfg.InvoiceExpirySeconds, err = getEnvInt("FAP_INVOICE_EXPIRY_SECONDS", cfg.InvoiceExpirySeconds)
	if err != nil {
		return Config{}, err
	}
	cfg.PriceMSATDefault, err = getEnvInt("FAP_PRICE_MSAT_DEFAULT", cfg.PriceMSATDefault)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func getRequiredEnv(key string) (string, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", fmt.Errorf("missing required env: %s", key)
	}
	return v, nil
}

func getEnvDefault(key string, def string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	return v
}

func getEnvInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid integer env %s: %w", key, err)
	}
	return parsed, nil
}

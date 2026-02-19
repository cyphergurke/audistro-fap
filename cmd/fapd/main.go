package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/yourorg/fap/pkg/fapkit"
	faphttp "github.com/yourorg/fap/pkg/fapkit/http"
)

func main() {
	cfg, migrateOnly, err := loadConfigFromEnv()
	if err != nil {
		fmt.Println("config error:", err)
		os.Exit(1)
	}

	if err := migrateDB(cfg.DBPath); err != nil {
		fmt.Println("migration error:", err)
		os.Exit(1)
	}
	if migrateOnly {
		fmt.Println("migrations completed")
		return
	}

	store, closeStore, err := fapkit.NewSQLiteStore(cfg.DBPath)
	if err != nil {
		fmt.Println("store error:", err)
		os.Exit(1)
	}
	defer func() { _ = closeStore() }()

	payments, err := fapkit.NewLNBitsPayments(cfg.LNBitsBaseURL, cfg.LNBitsInvoiceAPIKey, cfg.LNBitsReadOnlyAPIKey)
	if err != nil {
		fmt.Println("payments error:", err)
		os.Exit(1)
	}

	svc, err := fapkit.NewService(cfg, fapkit.Dependencies{
		Store:    store,
		Payments: payments,
		Clock:    fapkit.SystemClock(),
	})
	if err != nil {
		fmt.Println("service error:", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	faphttp.RegisterRoutes(mux, svc, faphttp.HTTPOptions{WebhookSecret: cfg.WebhookSecret})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	fmt.Println("fapd listening on", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Println("server error:", err)
		os.Exit(1)
	}
}

func loadConfigFromEnv() (fapkit.Config, bool, error) {
	issuerPrivKeyHex, err := requiredEnv("FAP_ISSUER_PRIVKEY_HEX")
	if err != nil {
		return fapkit.Config{}, false, err
	}
	dbPath, err := requiredEnv("FAP_DB_PATH")
	if err != nil {
		return fapkit.Config{}, false, err
	}
	lnbitsBaseURL, err := requiredEnv("FAP_LNBITS_BASE_URL")
	if err != nil {
		return fapkit.Config{}, false, err
	}
	lnbitsInvoiceAPIKey, err := requiredEnv("FAP_LNBITS_INVOICE_API_KEY")
	if err != nil {
		return fapkit.Config{}, false, err
	}
	lnbitsReadOnlyAPIKey, err := requiredEnv("FAP_LNBITS_READONLY_API_KEY")
	if err != nil {
		return fapkit.Config{}, false, err
	}
	webhookSecret, err := requiredEnv("FAP_WEBHOOK_SECRET")
	if err != nil {
		return fapkit.Config{}, false, err
	}

	tokenTTLSeconds, err := envInt64("FAP_TOKEN_TTL_SECONDS", 600)
	if err != nil {
		return fapkit.Config{}, false, err
	}
	invoiceExpirySeconds, err := envInt64("FAP_INVOICE_EXPIRY_SECONDS", 900)
	if err != nil {
		return fapkit.Config{}, false, err
	}
	priceMsatDefault, err := envInt64("FAP_PRICE_MSAT_DEFAULT", 100000)
	if err != nil {
		return fapkit.Config{}, false, err
	}

	cfg := fapkit.Config{
		HTTPAddr:             envOrDefault("FAP_HTTP_ADDR", ":8080"),
		IssuerPrivKeyHex:     issuerPrivKeyHex,
		DBPath:               dbPath,
		LNBitsBaseURL:        lnbitsBaseURL,
		LNBitsInvoiceAPIKey:  lnbitsInvoiceAPIKey,
		LNBitsReadOnlyAPIKey: lnbitsReadOnlyAPIKey,
		WebhookSecret:        webhookSecret,
		TokenTTLSeconds:      tokenTTLSeconds,
		InvoiceExpirySeconds: invoiceExpirySeconds,
		PriceMsatDefault:     priceMsatDefault,
	}

	if err := cfg.Validate(); err != nil {
		return fapkit.Config{}, false, err
	}

	migrateOnly := false
	for _, arg := range os.Args[1:] {
		if arg == "-migrate" {
			migrateOnly = true
			break
		}
	}
	return cfg, migrateOnly, nil
}

func envOrDefault(key string, def string) string {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return def
	}
	return val
}

func envInt64(key string, def int64) (int64, error) {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return def, nil
	}
	parsed, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer env %s: %w", key, err)
	}
	return parsed, nil
}

func requiredEnv(key string) (string, error) {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return "", fmt.Errorf("missing required env: %s", key)
	}
	return val, nil
}

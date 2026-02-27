package app

import (
	"context"
	"net/http"
	"time"

	"fap/internal/api"
	"fap/internal/crypto/secretbox"
	"fap/internal/hlskey"
	"fap/internal/pay"
	"fap/internal/pay/merchantlnbits"
	"fap/internal/service"
	"fap/internal/store"
	"fap/internal/token"
)

type Config struct {
	DBPath                           string
	MasterKey                        []byte
	IssuerPrivKeyHex                 string
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
	TokenSecret                      []byte
	EnableCORS                       bool
	CORSAllowedOrigins               []string
	CORSAllowCredentials             bool
}

type Components struct {
	Repository *store.SQLiteRepository
	Service    *service.FAPService
	Router     http.Handler
}

func New(ctx context.Context, cfg Config) (*Components, error) {
	repo, err := store.OpenSQLite(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	factory := pay.NewCachedFactory(cfg.MasterKey, repo, secretbox.Decrypt, func(payee store.Payee, invoiceKey string, readKey string) pay.PaymentAdapter {
		return merchantlnbits.New(payee.LNBitsBaseURL, invoiceKey, readKey)
	})
	svc, err := service.New(repo, factory, service.Config{
		IssuerPrivKeyHex:                 cfg.IssuerPrivKeyHex,
		TokenTTLSeconds:                  cfg.TokenTTLSeconds,
		InvoiceExpirySeconds:             cfg.InvoiceExpirySeconds,
		MaxAccessAmountMSat:              cfg.MaxAccessAmountMSat,
		AccessMinutesPerPay:              cfg.AccessMinutesPerPay,
		WebhookEventRetentionSeconds:     cfg.WebhookEventRetentionSeconds,
		WebhookEventPruneIntervalSeconds: cfg.WebhookEventPruneIntervalSeconds,
		DevMode:                          cfg.DevMode,
	})
	if err != nil {
		_ = repo.Close()
		return nil, err
	}
	issuer, err := token.NewIssuer(cfg.TokenSecret, time.Duration(cfg.TokenTTLSeconds)*time.Second, nil)
	if err != nil {
		_ = repo.Close()
		return nil, err
	}
	apiHandler := api.NewWithOptions(svc, api.Options{
		WebhookSecret:      cfg.WebhookSecret,
		DevMode:            cfg.DevMode,
		ExposeBolt11InList: cfg.ExposeBolt11InList,
		DeviceCookieSecure: cfg.DeviceCookieSecure,
		AccessTokenIssuer:  issuer,
		HLSKeyDerive: func(assetID string) [16]byte {
			return hlskey.DevAES128Key(cfg.MasterKey, assetID)
		},
	})
	baseRouter := apiHandler.Router()
	if cfg.EnableCORS {
		baseRouter = api.NewHLSCORSMiddleware(cfg.CORSAllowedOrigins, cfg.CORSAllowCredentials)(baseRouter)
	}
	inject := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx2 := api.WithMasterKey(r.Context(), cfg.MasterKey)
		ctx2 = api.WithEncryptor(ctx2, secretbox.Encrypt)
		baseRouter.ServeHTTP(w, r.WithContext(ctx2))
	})

	return &Components{
		Repository: repo,
		Service:    svc,
		Router:     inject,
	}, nil
}

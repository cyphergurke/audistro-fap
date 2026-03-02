package fap

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"audistro-fap/internal/app"
)

type Server struct {
	cfg      Config
	router   http.Handler
	verifier TokenVerifier
	closeFn  func() error
}

func NewServer(cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	tokenSecret, err := loadTokenSecret(cfg.TokenSecretPath)
	if err != nil {
		return nil, err
	}
	masterKey, err := cfg.MasterKey()
	if err != nil {
		return nil, err
	}
	components, err := app.New(context.Background(), app.Config{
		DBPath:                           cfg.DBPath,
		MasterKey:                        masterKey,
		IssuerPrivKeyHex:                 cfg.IssuerPrivKeyHex,
		WebhookSecret:                    cfg.WebhookSecret,
		TokenTTLSeconds:                  cfg.TokenTTLSeconds,
		InvoiceExpirySeconds:             cfg.InvoiceExpirySeconds,
		MaxAccessAmountMSat:              cfg.MaxAccessAmountMSat,
		AccessMinutesPerPay:              cfg.AccessMinutesPerPay,
		WebhookEventRetentionSeconds:     cfg.WebhookEventRetentionSeconds,
		WebhookEventPruneIntervalSeconds: cfg.WebhookEventPruneIntervalSeconds,
		DevMode:                          cfg.DevMode,
		ExposeBolt11InList:               cfg.ExposeBolt11InList,
		DeviceCookieSecure:               cfg.DeviceCookieSecure,
		TokenSecret:                      tokenSecret,
		EnableCORS:                       cfg.EnableCORS,
		CORSAllowedOrigins:               cfg.CORSAllowedOrigins,
		CORSAllowCredentials:             cfg.CORSAllowCredentials,
		AdminToken:                       cfg.AdminToken,
		InternalAllowedCIDRs:             cfg.InternalAllowedCIDRs,
		DisableOpenAPIValidation:         cfg.DisableOpenAPIValidation,
	})
	if err != nil {
		return nil, err
	}
	closeFn := func() error {
		if components.Repository != nil {
			return components.Repository.Close()
		}
		return nil
	}
	return &Server{
		cfg:      cfg,
		router:   components.Router,
		verifier: NewSchnorrVerifier(components.Service.IssuerPubKeyHex()),
		closeFn:  closeFn,
	}, nil
}

func (s *Server) Router() http.Handler { return s.router }

func (s *Server) Verifier() TokenVerifier { return s.verifier }

func (s *Server) HTTPServer() *http.Server {
	handler := withAccessLog(s.router)
	return &http.Server{
		Addr:              s.cfg.HTTPAddr,
		Handler:           http.TimeoutHandler(handler, 15*time.Second, "request timeout"),
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func (s *Server) Close() error {
	if s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

func (s *Server) ListenAndServe() error {
	server := s.HTTPServer()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func loadTokenSecret(path string) ([]byte, error) {
	secret, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read token secret: %w", err)
	}
	secret = bytes.TrimRight(secret, "\r\n")
	if len(secret) < 16 {
		return nil, fmt.Errorf("token secret must be at least 16 bytes")
	}
	return secret, nil
}

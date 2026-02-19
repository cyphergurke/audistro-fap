package httpapi

import (
	"net/http"
	"time"

	"github.com/yourorg/fap/internal/config"
	"github.com/yourorg/fap/internal/httpapi/handlers"
	"github.com/yourorg/fap/internal/security"
)

func NewServer(cfg config.Config, svc handlers.FAPService, hlsKeys handlers.HLSKeyRepository, issuerPubKeyHex string) *http.Server {
	api := handlers.NewAPIWithHLS(svc, hlsKeys, cfg.WebhookSecret, issuerPubKeyHex, int64(cfg.PriceMSATDefault))
	handler := http.TimeoutHandler(api.Routes(), security.HandlerTimeout, "request timeout")

	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

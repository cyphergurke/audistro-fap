package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	fapmw "github.com/yourorg/fap/internal/fap/httpmw"
	"github.com/yourorg/fap/internal/fap/service"
	httpmw "github.com/yourorg/fap/internal/httpapi/middleware"
	"github.com/yourorg/fap/internal/security"
	"github.com/yourorg/fap/internal/store/repo"
)

type FAPService interface {
	CreateChallenge(ctx context.Context, assetID string, subject string, rail repo.PaymentRail, amount int64, amountUnit repo.AmountUnit, asset string) (service.ChallengeResult, error)
	HandleWebhook(ctx context.Context, rail repo.PaymentRail, providerRef string, now int64) error
	MintToken(ctx context.Context, intentID string, now int64) (token string, expiresAt int64, resourceID string, rail repo.PaymentRail, err error)
}

type HLSKeyRepository interface {
	GetKey(ctx context.Context, assetID string) ([]byte, error)
}

type API struct {
	svc           FAPService
	hlsKeys       HLSKeyRepository
	hlsMW         *fapmw.Middleware
	webhookSecret string
	defaultPrice  int64
	nowUnix       func() int64
}

func NewAPI(svc FAPService, webhookSecret string, defaultPriceMSat int64) *API {
	return NewAPIWithHLS(svc, nil, webhookSecret, "", defaultPriceMSat)
}

func NewAPIWithHLS(svc FAPService, hlsKeys HLSKeyRepository, webhookSecret string, issuerPubKeyHex string, defaultPriceMSat int64) *API {
	var mw *fapmw.Middleware
	if issuerPubKeyHex != "" {
		mw = fapmw.New(issuerPubKeyHex)
	}
	if defaultPriceMSat <= 0 {
		defaultPriceMSat = security.DefaultPriceMSat
	}

	return &API{
		svc:           svc,
		hlsKeys:       hlsKeys,
		hlsMW:         mw,
		webhookSecret: webhookSecret,
		defaultPrice:  defaultPriceMSat,
		nowUnix:       func() int64 { return time.Now().Unix() },
	}
}

func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", allowMethod(http.MethodGet, Healthz))
	mux.HandleFunc("/metrics", allowMethod(http.MethodGet, Metrics))
	mux.HandleFunc("/openapi.yaml", allowMethod(http.MethodGet, OpenAPI))
	mux.HandleFunc("/docs", allowMethod(http.MethodGet, Docs))
	challengeRL := httpmw.NewChallengeRateLimiter()
	mux.Handle("/fap/challenge", challengeRL.Wrap(http.HandlerFunc(allowMethod(http.MethodPost, a.Challenge))))
	mux.HandleFunc("/fap/token", allowMethod(http.MethodPost, a.Token))
	mux.HandleFunc("/fap/webhook/lnbits", allowMethod(http.MethodPost, a.Webhook))
	mux.HandleFunc("/fap/webhook/", allowMethod(http.MethodPost, a.Webhook))

	hlsHandler := http.Handler(http.HandlerFunc(a.HLSKey))
	if a.hlsMW != nil {
		hlsHandler = a.hlsMW.Wrap(hlsHandler)
	}
	mux.Handle("/hls/", hlsHandler)

	return httpmw.RequestID(mux)
}

func allowMethod(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func (a *API) serviceUnavailable(w http.ResponseWriter) bool {
	if a.svc != nil {
		return false
	}
	writeError(w, http.StatusInternalServerError, "service unavailable")
	return true
}

func mapTokenError(err error) int {
	if errors.Is(err, service.ErrIntentNotSettled) {
		return http.StatusConflict
	}
	if errors.Is(err, service.ErrIntentExpired) {
		return http.StatusConflict
	}
	if errors.Is(err, service.ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, service.ErrUnsupportedRail) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

type errorResponse struct {
	Error string `json:"error"`
}

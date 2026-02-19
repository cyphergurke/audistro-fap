package mw

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/yourorg/fap/pkg/fapkit"
)

type MiddlewareOptions struct {
	ChallengeEndpoint string
}

type AssetIDExtractor func(r *http.Request) (string, bool)

type tokenVerifier interface {
	VerifyTokenForMiddleware(tokenStr string, nowUnix int64) (fapkit.VerifiedToken, error)
}

type paymentRequiredResponse struct {
	Error              string `json:"error"`
	ChallengeEndpoint  string `json:"challenge_endpoint"`
	AssetID            string `json:"asset_id"`
	RequiredResourceID string `json:"required_resource_id"`
}

func NewResourceMiddleware(svc fapkit.Service, extract AssetIDExtractor, entitlement string, opts MiddlewareOptions) func(http.Handler) http.Handler {
	challengeEndpoint := opts.ChallengeEndpoint
	if challengeEndpoint == "" {
		challengeEndpoint = "/fap/challenge"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assetID, ok := extract(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			requiredResourceID := "hls:key:" + assetID
			authz := r.Header.Get("Authorization")
			if authz == "" {
				writeJSON(w, http.StatusPaymentRequired, paymentRequiredResponse{
					Error:              "payment_required",
					ChallengeEndpoint:  challengeEndpoint,
					AssetID:            assetID,
					RequiredResourceID: requiredResourceID,
				})
				return
			}

			tokenStr, ok := parseBearer(authz)
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			verifier, ok := svc.(tokenVerifier)
			if !ok {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			verified, err := verifier.VerifyTokenForMiddleware(tokenStr, time.Now().Unix())
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if verified.ResourceID != requiredResourceID {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if !hasEntitlement(verified.Entitlements, entitlement) {
				w.WriteHeader(http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func NewHLSKeyMiddleware(svc fapkit.Service, opts MiddlewareOptions) func(next http.Handler) http.Handler {
	return NewResourceMiddleware(svc, defaultHLSExtractor, "hls:key", opts)
}

func defaultHLSExtractor(r *http.Request) (string, bool) {
	if r.Method != http.MethodGet {
		return "", false
	}
	path := r.URL.Path
	if !strings.HasPrefix(path, "/hls/") || !strings.HasSuffix(path, "/key") {
		return "", false
	}
	trimmed := strings.TrimPrefix(path, "/hls/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "key" {
		return "", false
	}
	return parts[0], true
}

func parseBearer(authz string) (string, bool) {
	if !strings.HasPrefix(authz, "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(authz, "Bearer ")
	return token, token != ""
}

func hasEntitlement(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v paymentRequiredResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

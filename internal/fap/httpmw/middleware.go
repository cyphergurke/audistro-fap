package httpmw

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/yourorg/fap/internal/fap/token"
	"github.com/yourorg/fap/internal/obs"
)

const challengeEndpoint = "/fap/challenge"

type Middleware struct {
	expectedIssuerPubKeyHex string
	nowUnix                 func() int64
}

func New(expectedIssuerPubKeyHex string) *Middleware {
	return &Middleware{
		expectedIssuerPubKeyHex: expectedIssuerPubKeyHex,
		nowUnix:                 unixNow,
	}
}

func (m *Middleware) SetNowUnix(now func() int64) {
	if now == nil {
		m.nowUnix = unixNow
		return
	}
	m.nowUnix = now
}

func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assetID, ok := extractAssetID(r.Method, r.URL.Path)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		requiredResourceID := "hls:key:" + assetID

		authz := r.Header.Get("Authorization")
		if authz == "" {
			obs.ObserveAuth("payment_required")
			writeJSON(w, http.StatusPaymentRequired, paymentRequiredResponse{
				Error:              "payment_required",
				ChallengeEndpoint:  challengeEndpoint,
				AssetID:            assetID,
				RequiredResourceID: requiredResourceID,
			})
			return
		}

		tokenStr, ok := bearerToken(authz)
		if !ok {
			obs.ObserveAuth("invalid_token")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		payload, err := token.VerifyToken(tokenStr, m.expectedIssuerPubKeyHex, m.nowUnix())
		if err != nil {
			obs.ObserveAuth("invalid_token")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if payload.ResourceID != requiredResourceID {
			obs.ObserveAuth("forbidden_resource")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if !hasEntitlement(payload.Entitlements, token.Entitlement("hls:key")) {
			obs.ObserveAuth("forbidden_entitlement")
			w.WriteHeader(http.StatusForbidden)
			return
		}

		obs.ObserveAuth("ok")
		next.ServeHTTP(w, r)
	})
}

type paymentRequiredResponse struct {
	Error              string `json:"error"`
	ChallengeEndpoint  string `json:"challenge_endpoint"`
	AssetID            string `json:"asset_id"`
	RequiredResourceID string `json:"required_resource_id"`
}

func writeJSON(w http.ResponseWriter, status int, v paymentRequiredResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func extractAssetID(method string, path string) (string, bool) {
	if method != http.MethodGet {
		return "", false
	}

	if !strings.HasPrefix(path, "/hls/") || !strings.HasSuffix(path, "/key") {
		return "", false
	}

	trimmed := strings.TrimPrefix(path, "/hls/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		return "", false
	}
	if parts[1] != "key" || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func bearerToken(authz string) (string, bool) {
	if !strings.HasPrefix(authz, "Bearer ") {
		return "", false
	}
	tok := strings.TrimPrefix(authz, "Bearer ")
	if tok == "" {
		return "", false
	}
	return tok, true
}

func hasEntitlement(items []token.Entitlement, want token.Entitlement) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func unixNow() int64 {
	return time.Now().Unix()
}

package fap

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type AssetIDExtractor func(r *http.Request) (string, bool)

func NewHLSKeyExtractor() AssetIDExtractor {
	return func(r *http.Request) (string, bool) {
		if r.Method != http.MethodGet {
			return "", false
		}
		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")
		if len(parts) != 3 {
			return "", false
		}
		if parts[0] != "hls" || parts[2] != "key" || parts[1] == "" {
			return "", false
		}
		return parts[1], true
	}
}

type gateResponse struct {
	Error             string `json:"error"`
	ChallengeEndpoint string `json:"challenge_endpoint"`
	ResourceID        string `json:"required_resource_id"`
}

func NewResourceGateMiddleware(verifier TokenVerifier, extract AssetIDExtractor, entitlement string) func(http.Handler) http.Handler {
	_ = entitlement
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assetID, ok := extract(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			requiredRID := "hls:key:" + assetID
			authz := r.Header.Get("Authorization")
			if authz == "" {
				writeJSON(w, http.StatusPaymentRequired, gateResponse{
					Error:             "payment_required",
					ChallengeEndpoint: "/v1/fap/challenge",
					ResourceID:        requiredRID,
				})
				return
			}
			token, ok := parseBearer(authz)
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			claims, err := verifier.Verify(token, time.Now().Unix())
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if claims.ResourceID != requiredRID {
				w.WriteHeader(http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func parseBearer(authz string) (string, bool) {
	if !strings.HasPrefix(authz, "Bearer ") {
		return "", false
	}
	v := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	if v == "" {
		return "", false
	}
	return v, true
}

func writeJSON[T any](w http.ResponseWriter, status int, body T) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

package http

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yourorg/fap/internal/obs"
	"github.com/yourorg/fap/pkg/fapkit"
)

type HTTPOptions struct {
	WebhookSecret string
}

type errorResponse struct {
	Error string `json:"error"`
}

type challengeRequest struct {
	AssetID    string `json:"asset_id"`
	Subject    string `json:"subject"`
	AmountMSat int64  `json:"amount_msat"`
}

type challengeResponse struct {
	IntentID    string `json:"intent_id"`
	Offer       string `json:"offer"`
	ProviderRef string `json:"provider_ref"`
	ExpiresAt   int64  `json:"expires_at"`
}

type tokenRequest struct {
	IntentID string `json:"intent_id"`
}

type tokenResponse struct {
	Token      string `json:"token"`
	ExpiresAt  int64  `json:"expires_at"`
	ResourceID string `json:"resource_id"`
}

type webhookRequest struct {
	ProviderRef string `json:"provider_ref"`
	PaymentHash string `json:"payment_hash"`
}

//go:embed openapi/openapi.yaml
var openAPISpec []byte

const scalarHTML = `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>FAP API Docs</title>
  </head>
  <body>
    <script
      id="api-reference"
      data-url="/openapi.yaml"
      data-configuration='{"theme":"purple","layout":"modern"}'></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`

func RegisterRoutes(mux *http.ServeMux, svc fapkit.Service, opts HTTPOptions) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthzResponse{OK: true})
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		obs.Register()
		promhttp.Handler().ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openAPISpec)
	})
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(scalarHTML))
	})

	mux.HandleFunc("POST /fap/challenge", func(w http.ResponseWriter, r *http.Request) {
		var req challengeRequest
		if err := decodeStrict(r.Body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		ch, err := svc.CreateChallenge(r.Context(), req.AssetID, req.Subject, req.AmountMSat)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, challengeResponse{
			IntentID:    ch.IntentID,
			Offer:       ch.Offer,
			ProviderRef: ch.ProviderRef,
			ExpiresAt:   ch.ExpiresAt,
		})
	})

	mux.HandleFunc("POST /fap/token", func(w http.ResponseWriter, r *http.Request) {
		var req tokenRequest
		if err := decodeStrict(r.Body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		out, err := svc.MintToken(r.Context(), req.IntentID, 0)
		if err != nil {
			if err == fapkit.ErrIntentNotSettled || err == fapkit.ErrIntentExpired {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			if err == fapkit.ErrNotFound {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tokenResponse{
			Token:      out.Token,
			ExpiresAt:  out.ExpiresAt,
			ResourceID: out.ResourceID,
		})
	})

	mux.HandleFunc("POST /fap/webhook/lightning", func(w http.ResponseWriter, r *http.Request) {
		handleWebhook(w, r, svc, opts)
	})
	mux.HandleFunc("POST /fap/webhook/lnbits", func(w http.ResponseWriter, r *http.Request) {
		handleWebhook(w, r, svc, opts)
	})
}

func handleWebhook(w http.ResponseWriter, r *http.Request, svc fapkit.Service, opts HTTPOptions) {
	if r.Header.Get("X-FAP-Webhook-Secret") != opts.WebhookSecret {
		writeError(w, http.StatusUnauthorized, "invalid webhook secret")
		return
	}

	var req webhookRequest
	if err := decodeStrict(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	providerRef := req.ProviderRef
	if providerRef == "" {
		providerRef = req.PaymentHash
	}
	if providerRef == "" {
		writeError(w, http.StatusBadRequest, "missing provider_ref")
		return
	}

	if err := svc.HandleWebhook(r.Context(), providerRef, 0); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeStrict[T any](body io.Reader, out *T) error {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func writeJSON[T any](w http.ResponseWriter, status int, v T) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type healthzResponse struct {
	OK bool `json:"ok"`
}

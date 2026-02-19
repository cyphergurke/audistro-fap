package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	httpmwreq "github.com/yourorg/fap/internal/httpapi/middleware"
	"github.com/yourorg/fap/internal/obs"
	"github.com/yourorg/fap/internal/store/repo"
)

type webhookRequest struct {
	ProviderRef string `json:"provider_ref"`
	PaymentHash string `json:"payment_hash"`
}

func (a *API) Webhook(w http.ResponseWriter, r *http.Request) {
	requestID := httpmwreq.FromContext(r.Context())
	if a.serviceUnavailable(w) {
		obs.ObserveWebhook("error")
		obs.DefaultLogger().ErrorHandler("webhook", "error", requestID, "service unavailable")
		return
	}
	if r.Header.Get("X-FAP-Webhook-Secret") != a.webhookSecret {
		obs.ObserveWebhook("unauthorized")
		obs.DefaultLogger().InfoHandler("webhook", "unauthorized", requestID)
		writeError(w, http.StatusUnauthorized, "invalid webhook secret")
		return
	}

	rail, ok := parseWebhookRail(r.URL.Path)
	if !ok {
		obs.ObserveWebhook("bad_request")
		obs.DefaultLogger().InfoHandler("webhook", "bad_request", requestID)
		writeError(w, http.StatusBadRequest, "invalid rail")
		return
	}

	var req webhookRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		obs.ObserveWebhook("bad_request")
		obs.DefaultLogger().InfoHandler("webhook", "bad_request", requestID)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := ensureNoTrailingJSON(dec); err != nil {
		obs.ObserveWebhook("bad_request")
		obs.DefaultLogger().InfoHandler("webhook", "bad_request", requestID)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	providerRef := req.ProviderRef
	if providerRef == "" {
		providerRef = req.PaymentHash
	}
	if providerRef == "" {
		obs.ObserveWebhook("bad_request")
		obs.DefaultLogger().InfoHandler("webhook", "bad_request", requestID)
		writeError(w, http.StatusBadRequest, "missing provider_ref")
		return
	}

	if err := a.svc.HandleWebhook(r.Context(), rail, providerRef, a.nowUnix()); err != nil {
		obs.ObserveWebhook("error")
		obs.DefaultLogger().ErrorHandler("webhook", "error", requestID, err.Error())
		writeError(w, mapTokenError(err), err.Error())
		return
	}

	obs.ObserveWebhook("ok")
	obs.DefaultLogger().InfoHandler("webhook", "ok", requestID)
	w.WriteHeader(http.StatusNoContent)
}

func parseWebhookRail(path string) (repo.PaymentRail, bool) {
	if path == "/fap/webhook/lnbits" {
		return repo.PaymentRailLightning, true
	}

	const prefix = "/fap/webhook/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}

	rail := strings.TrimPrefix(path, prefix)
	if rail == "" || strings.Contains(rail, "/") {
		return "", false
	}

	if rail == "lnbits" {
		return repo.PaymentRailLightning, true
	}
	return repo.PaymentRail(rail), true
}

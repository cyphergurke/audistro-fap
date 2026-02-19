package handlers

import (
	"encoding/json"
	"net/http"

	httpmwreq "github.com/yourorg/fap/internal/httpapi/middleware"
	"github.com/yourorg/fap/internal/obs"
)

type tokenRequest struct {
	IntentID string `json:"intent_id"`
}

type tokenResponse struct {
	Token      string `json:"token"`
	ExpiresAt  int64  `json:"expires_at"`
	ResourceID string `json:"resource_id"`
	Rail       string `json:"rail"`
}

func (a *API) Token(w http.ResponseWriter, r *http.Request) {
	requestID := httpmwreq.FromContext(r.Context())
	if a.serviceUnavailable(w) {
		obs.ObserveToken("error")
		obs.DefaultLogger().ErrorHandler("token", "error", requestID, "service unavailable")
		return
	}

	var req tokenRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		obs.ObserveToken("bad_request")
		obs.DefaultLogger().InfoHandler("token", "bad_request", requestID)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := ensureNoTrailingJSON(dec); err != nil {
		obs.ObserveToken("bad_request")
		obs.DefaultLogger().InfoHandler("token", "bad_request", requestID)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tokenValue, expiresAt, resourceID, rail, err := a.svc.MintToken(r.Context(), req.IntentID, a.nowUnix())
	if err != nil {
		status := mapTokenError(err)
		result := "error"
		if status == http.StatusConflict {
			result = "conflict"
		} else if status == http.StatusNotFound {
			result = "not_found"
		}
		obs.ObserveToken(result)
		obs.DefaultLogger().ErrorHandler("token", result, requestID, err.Error())
		writeError(w, status, err.Error())
		return
	}

	obs.ObserveToken("ok")
	obs.DefaultLogger().InfoHandler("token", "ok", requestID)
	writeJSON(w, http.StatusOK, tokenResponse{
		Token:      tokenValue,
		ExpiresAt:  expiresAt,
		ResourceID: resourceID,
		Rail:       string(rail),
	})
}

package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/yourorg/fap/internal/fap/service"
	httpmwreq "github.com/yourorg/fap/internal/httpapi/middleware"
	"github.com/yourorg/fap/internal/obs"
	"github.com/yourorg/fap/internal/store/repo"
)

type challengeRequest struct {
	AssetID    string `json:"asset_id"`
	Subject    string `json:"subject"`
	Rail       string `json:"rail"`
	Amount     int64  `json:"amount"`
	AmountUnit string `json:"amount_unit"`
	Asset      string `json:"asset"`
}

type challengeResponse struct {
	IntentID    string `json:"intent_id"`
	Rail        string `json:"rail"`
	Offer       string `json:"offer"`
	ProviderRef string `json:"provider_ref"`
	ExpiresAt   int64  `json:"expires_at"`
}

func (a *API) Challenge(w http.ResponseWriter, r *http.Request) {
	requestID := httpmwreq.FromContext(r.Context())
	if a.serviceUnavailable(w) {
		obs.ObserveChallenge("error")
		obs.DefaultLogger().ErrorHandler("challenge", "error", requestID, "service unavailable")
		return
	}

	var req challengeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		obs.ObserveChallenge("bad_request")
		obs.DefaultLogger().InfoHandler("challenge", "bad_request", requestID)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := ensureNoTrailingJSON(dec); err != nil {
		obs.ObserveChallenge("bad_request")
		obs.DefaultLogger().InfoHandler("challenge", "bad_request", requestID)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	rail := req.Rail
	if rail == "" {
		rail = string(repo.PaymentRailLightning)
	}
	amount := req.Amount
	if amount == 0 {
		amount = a.defaultPrice
	}
	amountUnit := req.AmountUnit
	if amountUnit == "" {
		amountUnit = string(repo.AmountUnitMsat)
	}
	asset := req.Asset
	if asset == "" {
		asset = "BTC"
	}

	result, err := a.svc.CreateChallenge(
		r.Context(),
		req.AssetID,
		req.Subject,
		repo.PaymentRail(rail),
		amount,
		repo.AmountUnit(amountUnit),
		asset,
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidAmount) || errors.Is(err, service.ErrInvalidAssetID) || errors.Is(err, service.ErrInvalidSubject) {
			obs.ObserveChallenge("bad_request")
			obs.DefaultLogger().InfoHandler("challenge", "bad_request", requestID)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, service.ErrUnsupportedRail) {
			obs.ObserveChallenge("bad_request")
			obs.DefaultLogger().InfoHandler("challenge", "bad_request", requestID)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		obs.ObserveChallenge("error")
		obs.DefaultLogger().ErrorHandler("challenge", "error", requestID, err.Error())
		writeError(w, http.StatusInternalServerError, "failed to create challenge")
		return
	}

	obs.ObserveChallenge("ok")
	obs.DefaultLogger().InfoHandler("challenge", "ok", requestID)
	writeJSON(w, http.StatusOK, challengeResponse{
		IntentID:    result.IntentID,
		Rail:        string(result.Rail),
		Offer:       result.Offer,
		ProviderRef: result.ProviderRef,
		ExpiresAt:   result.ExpiresAt,
	})
}

func ensureNoTrailingJSON(dec *json.Decoder) error {
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}

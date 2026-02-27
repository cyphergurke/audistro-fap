package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	faptoken "audistro-fap/internal/fap/token"
	"audistro-fap/internal/service"
)

type API struct {
	svc                *service.FAPService
	webhookSecret      string
	devMode            bool
	exposeBolt11InList bool
	deviceCookieSecure bool
	accessTokenIssuer  AccessTokenIssuer
	hlsKeyDerive       HLSKeyDeriveFunc
	challengeLimiter   *endpointRateLimiter
	tokenLimiter       *endpointRateLimiter
	keyLimiter         *endpointRateLimiter
}

func New(svc *service.FAPService, webhookSecret string) *API {
	return &API{
		svc:           svc,
		webhookSecret: webhookSecret,
	}
}

type AccessTokenIssuer interface {
	Issue(assetID string, now time.Time) (token string, expiresAt int64, err error)
	Validate(token string, assetID string, now time.Time) error
}

type HLSKeyDeriveFunc func(assetID string) [16]byte

type Options struct {
	WebhookSecret      string
	DevMode            bool
	ExposeBolt11InList bool
	DeviceCookieSecure bool
	AccessTokenIssuer  AccessTokenIssuer
	HLSKeyDerive       HLSKeyDeriveFunc
	ChallengeRateRPS   float64
	ChallengeRateBurst int
	TokenRateRPS       float64
	TokenRateBurst     int
	KeyRateRPS         float64
	KeyRateBurst       int
}

func NewWithOptions(svc *service.FAPService, opts Options) *API {
	challengeRPS := opts.ChallengeRateRPS
	if challengeRPS <= 0 {
		challengeRPS = defaultChallengeRateRPS
	}
	challengeBurst := opts.ChallengeRateBurst
	if challengeBurst <= 0 {
		challengeBurst = defaultChallengeRateBurst
	}

	tokenRPS := opts.TokenRateRPS
	if tokenRPS <= 0 {
		tokenRPS = defaultTokenRateRPS
	}
	tokenBurst := opts.TokenRateBurst
	if tokenBurst <= 0 {
		tokenBurst = defaultTokenRateBurst
	}

	keyRPS := opts.KeyRateRPS
	if keyRPS <= 0 {
		keyRPS = defaultKeyRateRPS
	}
	keyBurst := opts.KeyRateBurst
	if keyBurst <= 0 {
		keyBurst = defaultKeyRateBurst
	}

	return &API{
		svc:                svc,
		webhookSecret:      opts.WebhookSecret,
		devMode:            opts.DevMode,
		exposeBolt11InList: opts.ExposeBolt11InList,
		deviceCookieSecure: opts.DeviceCookieSecure,
		accessTokenIssuer:  opts.AccessTokenIssuer,
		hlsKeyDerive:       opts.HLSKeyDerive,
		challengeLimiter:   newEndpointRateLimiter(challengeRPS, challengeBurst),
		tokenLimiter:       newEndpointRateLimiter(tokenRPS, tokenBurst),
		keyLimiter:         newEndpointRateLimiter(keyRPS, keyBurst),
	}
}

func (a *API) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /openapi.yaml", openAPI)
	mux.HandleFunc("GET /docs", docs)
	mux.HandleFunc("POST /v1/payees", a.createPayee)
	mux.HandleFunc("POST /v1/assets", a.createAsset)
	mux.HandleFunc("POST /v1/boost", a.createBoost)
	mux.HandleFunc("GET /v1/boost", a.listBoosts)
	mux.HandleFunc("GET /v1/boost/{boostId}", a.getBoost)
	mux.HandleFunc("POST /v1/boost/{boostId}/mark_paid", a.markBoostPaid)
	mux.HandleFunc("GET /v1/ledger", a.listLedger)
	mux.HandleFunc("POST /v1/fap/challenge", a.withRateLimit(a.challengeLimiter, a.challenge))
	mux.HandleFunc("POST /v1/device/bootstrap", a.deviceBootstrap)
	mux.HandleFunc("POST /v1/fap/webhook/lnbits", a.webhook)
	mux.HandleFunc("POST /v1/fap/token", a.withRateLimit(a.tokenLimiter, a.token))
	mux.HandleFunc("POST /v1/access/{assetId}", a.access)
	mux.HandleFunc("GET /v1/access/grants", a.listAccessGrants)
	mux.HandleFunc("GET /hls/{assetId}/key", a.withRateLimit(a.keyLimiter, a.hlsKey))
	return mux
}

type errorResponse struct {
	Error string `json:"error"`
}

type payeeRequest struct {
	DisplayName               string `json:"display_name"`
	LNBitsBaseURL             string `json:"lnbits_base_url"`
	LNBitsInvoiceKey          string `json:"lnbits_invoice_key,omitempty"`
	LNBitsReadKey             string `json:"lnbits_read_key,omitempty"`
	LNBitsInvoiceKeyLegacyEnv string `json:"FAP_LNBITS_INVOICE_API_KEY,omitempty"`
	LNBitsReadKeyLegacyEnv    string `json:"FAP_LNBITS_READONLY_API_KEY,omitempty"`
}

type payeeResponse struct {
	PayeeID       string `json:"payee_id"`
	DisplayName   string `json:"display_name"`
	Rail          string `json:"rail"`
	Mode          string `json:"mode"`
	LNBitsBaseURL string `json:"lnbits_base_url"`
}

type assetRequest struct {
	AssetID   string `json:"asset_id"`
	PayeeID   string `json:"payee_id"`
	Title     string `json:"title"`
	PriceMSat int64  `json:"price_msat"`
}

type assetResponse struct {
	AssetID    string `json:"asset_id"`
	PayeeID    string `json:"payee_id"`
	Title      string `json:"title"`
	PriceMSat  int64  `json:"price_msat"`
	ResourceID string `json:"resource_id"`
}

type challengeRequest struct {
	AssetID        string `json:"asset_id"`
	PayeeID        string `json:"payee_id,omitempty"`
	AmountMSat     int64  `json:"amount_msat,omitempty"`
	Memo           string `json:"memo,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Subject        string `json:"subject,omitempty"`
}

type challengeResponse struct {
	ChallengeID string `json:"challenge_id"`
	IntentID    string `json:"intent_id"`
	DeviceID    string `json:"device_id,omitempty"`
	AssetID     string `json:"asset_id,omitempty"`
	PayeeID     string `json:"payee_id,omitempty"`
	Status      string `json:"status,omitempty"`
	Bolt11      string `json:"bolt11"`
	PaymentHash string `json:"payment_hash"`
	CheckingID  string `json:"checking_id,omitempty"`
	ExpiresAt   int64  `json:"expires_at"`
	AmountMSat  int64  `json:"amount_msat"`
	ResourceID  string `json:"resource_id"`
}

type deviceBootstrapResponse struct {
	DeviceID string `json:"device_id"`
}

type accessGrantResponse struct {
	GrantID          string `json:"grant_id"`
	AssetID          string `json:"asset_id"`
	Scope            string `json:"scope"`
	MinutesPurchased int64  `json:"minutes_purchased"`
	ValidFrom        *int64 `json:"valid_from"`
	ValidUntil       *int64 `json:"valid_until"`
	Status           string `json:"status"`
	ChallengeID      string `json:"challenge_id"`
	AmountMSat       int64  `json:"amount_msat"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
}

type accessGrantsResponse struct {
	DeviceID string                `json:"device_id"`
	Items    []accessGrantResponse `json:"items"`
}

type webhookRequest struct {
	EventID     string `json:"event_id"`
	PaymentHash string `json:"payment_hash"`
	CheckingID  string `json:"checking_id"`
	Paid        *bool  `json:"paid"`
	Pending     *bool  `json:"pending"`
	Status      string `json:"status"`
	PaidAt      *int64 `json:"paid_at"`
	Time        *int64 `json:"time"`
}

type tokenRequest struct {
	ChallengeID string `json:"challenge_id,omitempty"`
	IntentID    string `json:"intent_id,omitempty"`
	Subject     string `json:"subject,omitempty"`
}

type tokenResponse struct {
	Token      string `json:"token"`
	ExpiresAt  int64  `json:"expires_at"`
	ResourceID string `json:"resource_id"`
}

type accessResponse struct {
	AssetID     string `json:"asset_id"`
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"`
}

type boostRequest struct {
	AssetID        string `json:"asset_id"`
	PayeeID        string `json:"payee_id"`
	AmountMSat     int64  `json:"amount_msat"`
	Memo           string `json:"memo,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type boostResponse struct {
	BoostID    string `json:"boost_id"`
	AssetID    string `json:"asset_id"`
	PayeeID    string `json:"payee_id"`
	AmountMSat int64  `json:"amount_msat"`
	Bolt11     string `json:"bolt11"`
	ExpiresAt  int64  `json:"expires_at"`
	Status     string `json:"status"`
}

type boostListItemResponse struct {
	BoostID    string `json:"boost_id"`
	AssetID    string `json:"asset_id"`
	PayeeID    string `json:"payee_id"`
	AmountMSat int64  `json:"amount_msat"`
	Status     string `json:"status"`
	Bolt11     string `json:"bolt11,omitempty"`
	ExpiresAt  int64  `json:"expires_at"`
	CreatedAt  int64  `json:"created_at"`
	PaidAt     *int64 `json:"paid_at"`
}

type boostListResponse struct {
	Items      []boostListItemResponse `json:"items"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

type boostStatusResponse struct {
	BoostID    string `json:"boost_id"`
	Status     string `json:"status"`
	PaidAt     *int64 `json:"paid_at"`
	ExpiresAt  int64  `json:"expires_at"`
	AmountMSat int64  `json:"amount_msat"`
}

type ledgerEntryResponse struct {
	EntryID     string `json:"entry_id"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	AssetID     string `json:"asset_id,omitempty"`
	PayeeID     string `json:"payee_id"`
	AmountMSat  int64  `json:"amount_msat"`
	Currency    string `json:"currency"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	PaidAt      *int64 `json:"paid_at"`
	ReferenceID string `json:"reference_id,omitempty"`
}

type ledgerListResponse struct {
	DeviceID   string                `json:"device_id"`
	Items      []ledgerEntryResponse `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

type healthzResponse struct {
	OK bool `json:"ok"`
}

var assetIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

const deviceCookieName = "fap_device_id"

func (a *API) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthzResponse{OK: true})
}

func (a *API) deviceBootstrap(w http.ResponseWriter, r *http.Request) {
	existing := readDeviceCookie(r)
	device, err := a.svc.BootstrapDevice(r.Context(), existing, time.Now().Unix())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeDeviceCookie(w, device.DeviceID, a.deviceCookieSecure)
	writeJSON(w, http.StatusOK, deviceBootstrapResponse{DeviceID: device.DeviceID})
}

func (a *API) listAccessGrants(w http.ResponseWriter, r *http.Request) {
	deviceID := readDeviceCookie(r)
	if strings.TrimSpace(deviceID) == "" {
		writeError(w, http.StatusUnauthorized, "device_required")
		return
	}
	assetID := strings.TrimSpace(r.URL.Query().Get("asset_id"))
	if assetID != "" && !assetIDPattern.MatchString(assetID) {
		writeError(w, http.StatusBadRequest, "invalid asset_id")
		return
	}
	items, err := a.svc.ListAccessGrantsForDevice(r.Context(), deviceID, assetID, time.Now().Unix())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]accessGrantResponse, 0, len(items))
	for _, item := range items {
		out = append(out, accessGrantResponse{
			GrantID:          item.GrantID,
			AssetID:          item.AssetID,
			Scope:            item.Scope,
			MinutesPurchased: item.MinutesPurchased,
			ValidFrom:        item.ValidFrom,
			ValidUntil:       item.ValidUntil,
			Status:           item.Status,
			ChallengeID:      item.ChallengeID,
			AmountMSat:       item.AmountMSat,
			CreatedAt:        item.CreatedAt,
			UpdatedAt:        item.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, accessGrantsResponse{
		DeviceID: deviceID,
		Items:    out,
	})
}

func (a *API) createPayee(w http.ResponseWriter, r *http.Request) {
	var req payeeRequest
	if err := decodeStrict(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	encryptor := GetEncryptor(ctx)
	masterKey := GetMasterKey(ctx)
	if encryptor == nil || len(masterKey) == 0 {
		writeError(w, http.StatusInternalServerError, "server encryption setup missing")
		return
	}
	created, err := a.svc.CreatePayee(ctx, service.CreatePayeeRequest{
		DisplayName:      req.DisplayName,
		LNBitsBaseURL:    req.LNBitsBaseURL,
		LNBitsInvoiceKey: firstNonEmpty(req.LNBitsInvoiceKey, req.LNBitsInvoiceKeyLegacyEnv),
		LNBitsReadKey:    firstNonEmpty(req.LNBitsReadKey, req.LNBitsReadKeyLegacyEnv),
	}, encryptor, masterKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payeeResponse{
		PayeeID:       created.PayeeID,
		DisplayName:   created.DisplayName,
		Rail:          created.Rail,
		Mode:          created.Mode,
		LNBitsBaseURL: created.LNBitsBaseURL,
	})
}

func (a *API) createAsset(w http.ResponseWriter, r *http.Request) {
	var req assetRequest
	if err := decodeStrict(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	created, err := a.svc.CreateAsset(r.Context(), service.CreateAssetRequest{
		AssetID:   req.AssetID,
		PayeeID:   req.PayeeID,
		Title:     req.Title,
		PriceMSat: req.PriceMSat,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, assetResponse{
		AssetID:    created.AssetID,
		PayeeID:    created.PayeeID,
		Title:      created.Title,
		PriceMSat:  created.PriceMSat,
		ResourceID: created.ResourceID,
	})
}

func (a *API) challenge(w http.ResponseWriter, r *http.Request) {
	var req challengeRequest
	if err := decodeStrict(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	device, err := a.svc.BootstrapDevice(r.Context(), readDeviceCookie(r), time.Now().Unix())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeDeviceCookie(w, device.DeviceID, a.deviceCookieSecure)

	ch, err := a.svc.CreateChallenge(r.Context(), service.CreateChallengeRequest{
		DeviceID:       device.DeviceID,
		AssetID:        req.AssetID,
		PayeeID:        req.PayeeID,
		AmountMSat:     req.AmountMSat,
		Memo:           req.Memo,
		IdempotencyKey: req.IdempotencyKey,
		Subject:        req.Subject,
	})
	if err != nil {
		if errors.Is(err, service.ErrDeviceMismatch) {
			writeError(w, http.StatusForbidden, "device_mismatch")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, challengeResponse{
		ChallengeID: ch.ChallengeID,
		IntentID:    ch.IntentID,
		DeviceID:    ch.DeviceID,
		AssetID:     ch.AssetID,
		PayeeID:     ch.PayeeID,
		Status:      ch.Status,
		Bolt11:      ch.Bolt11,
		PaymentHash: ch.PaymentHash,
		CheckingID:  ch.CheckingID,
		ExpiresAt:   ch.ExpiresAt,
		AmountMSat:  ch.AmountMSat,
		ResourceID:  ch.ResourceID,
	})
}

func (a *API) webhook(w http.ResponseWriter, r *http.Request) {
	if !a.hasValidWebhookSecret(r) {
		writeError(w, http.StatusUnauthorized, "invalid webhook secret")
		return
	}

	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	event, err := parseLNBitsWebhookEvent(rawBody)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = a.svc.HandleLNBitsWebhook(r.Context(), event, time.Now().Unix())
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) token(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := decodeStrict(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	challengeID := strings.TrimSpace(req.ChallengeID)
	if challengeID == "" {
		challengeID = strings.TrimSpace(req.IntentID)
	}
	subject := strings.TrimSpace(req.Subject)
	if cookieValue := readDeviceCookie(r); cookieValue != "" {
		subject = cookieValue
	}
	minted, err := a.svc.MintToken(r.Context(), challengeID, subject, time.Now().Unix())
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotSettled):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrIntentExpired):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrDeviceRequired):
			writeError(w, http.StatusUnauthorized, "device_required")
		case errors.Is(err, service.ErrDeviceMismatch):
			writeError(w, http.StatusForbidden, "device_mismatch")
		case errors.Is(err, service.ErrSubjectMismatch):
			writeError(w, http.StatusForbidden, err.Error())
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse(minted))
}

func (a *API) access(w http.ResponseWriter, r *http.Request) {
	assetID := r.PathValue("assetId")
	if !assetIDPattern.MatchString(assetID) {
		writeError(w, http.StatusBadRequest, "invalid_asset_id")
		return
	}
	if !a.devMode {
		writeError(w, http.StatusForbidden, "dev_mode_disabled")
		return
	}
	if a.accessTokenIssuer == nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	token, expiresAt, err := a.accessTokenIssuer.Issue(assetID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, accessResponse{
		AssetID:     assetID,
		AccessToken: token,
		ExpiresAt:   expiresAt,
	})
}

func (a *API) createBoost(w http.ResponseWriter, r *http.Request) {
	var req boostRequest
	if err := decodeStrict(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	device, err := a.svc.BootstrapDevice(r.Context(), readDeviceCookie(r), time.Now().Unix())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeDeviceCookie(w, device.DeviceID, a.deviceCookieSecure)

	created, err := a.svc.CreateBoost(r.Context(), service.CreateBoostRequest{
		DeviceID:       device.DeviceID,
		AssetID:        req.AssetID,
		PayeeID:        req.PayeeID,
		AmountMSat:     req.AmountMSat,
		Memo:           req.Memo,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrValidation):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrDeviceRequired):
			writeError(w, http.StatusUnauthorized, "device_required")
		case errors.Is(err, service.ErrDeviceMismatch):
			writeError(w, http.StatusForbidden, "device_mismatch")
		case errors.Is(err, service.ErrNotFound):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	writeJSON(w, http.StatusOK, boostResponse{
		BoostID:    created.BoostID,
		AssetID:    created.AssetID,
		PayeeID:    created.PayeeID,
		AmountMSat: created.AmountMSat,
		Bolt11:     created.Bolt11,
		ExpiresAt:  created.ExpiresAt,
		Status:     created.Status,
	})
}

func (a *API) listLedger(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(readDeviceCookie(r))
	if deviceID == "" {
		writeError(w, http.StatusUnauthorized, "device_required")
		return
	}

	query := r.URL.Query()
	limit := 0
	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	result, err := a.svc.ListLedgerEntriesForDevice(r.Context(), service.ListLedgerEntriesRequest{
		DeviceID: deviceID,
		Kind:     strings.TrimSpace(query.Get("kind")),
		Status:   strings.TrimSpace(query.Get("status")),
		AssetID:  strings.TrimSpace(query.Get("asset_id")),
		Limit:    limit,
		Cursor:   strings.TrimSpace(query.Get("cursor")),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrValidation):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error")
		}
		return
	}

	items := make([]ledgerEntryResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, ledgerEntryResponse{
			EntryID:     item.EntryID,
			Kind:        item.Kind,
			Status:      item.Status,
			AssetID:     item.AssetID,
			PayeeID:     item.PayeeID,
			AmountMSat:  item.AmountMSat,
			Currency:    item.Currency,
			CreatedAt:   item.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
			PaidAt:      item.PaidAt,
			ReferenceID: item.ReferenceID,
		})
	}
	writeJSON(w, http.StatusOK, ledgerListResponse{
		DeviceID:   deviceID,
		Items:      items,
		NextCursor: result.NextCursor,
	})
}

func (a *API) listBoosts(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := 0
	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	result, err := a.svc.ListBoosts(r.Context(), service.ListBoostsRequest{
		AssetID: strings.TrimSpace(query.Get("asset_id")),
		PayeeID: strings.TrimSpace(query.Get("payee_id")),
		Status:  strings.TrimSpace(query.Get("status")),
		Limit:   limit,
		Cursor:  strings.TrimSpace(query.Get("cursor")),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrValidation):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error")
		}
		return
	}

	items := make([]boostListItemResponse, 0, len(result.Items))
	for _, item := range result.Items {
		value := boostListItemResponse{
			BoostID:    item.BoostID,
			AssetID:    item.AssetID,
			PayeeID:    item.PayeeID,
			AmountMSat: item.AmountMSat,
			Status:     item.Status,
			ExpiresAt:  item.ExpiresAt,
			CreatedAt:  item.CreatedAt,
			PaidAt:     item.PaidAt,
		}
		if a.exposeBolt11InList {
			value.Bolt11 = item.Bolt11
		}
		items = append(items, value)
	}

	writeJSON(w, http.StatusOK, boostListResponse{
		Items:      items,
		NextCursor: result.NextCursor,
	})
}

func (a *API) getBoost(w http.ResponseWriter, r *http.Request) {
	boostID := strings.TrimSpace(r.PathValue("boostId"))
	boost, err := a.svc.GetBoost(r.Context(), boostID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrValidation):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrNotFound):
			writeError(w, http.StatusNotFound, "boost_not_found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	writeJSON(w, http.StatusOK, boostStatusResponse{
		BoostID:    boost.BoostID,
		Status:     boost.Status,
		PaidAt:     boost.PaidAt,
		ExpiresAt:  boost.ExpiresAt,
		AmountMSat: boost.AmountMSat,
	})
}

func (a *API) markBoostPaid(w http.ResponseWriter, r *http.Request) {
	if !a.devMode {
		writeError(w, http.StatusForbidden, "dev_mode_disabled")
		return
	}
	boostID := strings.TrimSpace(r.PathValue("boostId"))
	boost, err := a.svc.MarkBoostPaid(r.Context(), boostID, time.Now().Unix())
	if err != nil {
		switch {
		case errors.Is(err, service.ErrValidation):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrNotFound):
			writeError(w, http.StatusNotFound, "boost_not_found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	writeJSON(w, http.StatusOK, boostStatusResponse{
		BoostID:    boost.BoostID,
		Status:     boost.Status,
		PaidAt:     boost.PaidAt,
		ExpiresAt:  boost.ExpiresAt,
		AmountMSat: boost.AmountMSat,
	})
}

func (a *API) hlsKey(w http.ResponseWriter, r *http.Request) {
	assetID := r.PathValue("assetId")
	if !assetIDPattern.MatchString(assetID) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	token, ok := extractAccessToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	now := time.Now().UTC()
	authz, valid := a.authorizeTokenForAsset(token, assetID, now)
	if !valid {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if authz.kind == "signed" {
		deviceID := strings.TrimSpace(readDeviceCookie(r))
		if deviceID == "" {
			writeError(w, http.StatusUnauthorized, "device_required")
			return
		}
		if authz.payload == nil || strings.TrimSpace(authz.payload.Subject) == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if strings.TrimSpace(authz.payload.Subject) != deviceID {
			writeError(w, http.StatusForbidden, "device_mismatch")
			return
		}
		if err := a.svc.AuthorizeKeyAccess(r.Context(), deviceID, assetID, now.Unix()); err != nil {
			switch {
			case errors.Is(err, service.ErrPaymentRequired):
				writeError(w, http.StatusForbidden, "payment_required")
			case errors.Is(err, service.ErrGrantExpired):
				writeError(w, http.StatusForbidden, "grant_expired")
			case errors.Is(err, service.ErrValidation):
				writeError(w, http.StatusBadRequest, err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "internal_error")
			}
			return
		}
	}
	if a.hlsKeyDerive == nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	keyBytes := a.hlsKeyDerive(assetID)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(keyBytes[:])
}

type tokenAuthorization struct {
	kind    string
	payload *faptoken.AccessTokenPayload
}

func (a *API) authorizeTokenForAsset(token string, assetID string, now time.Time) (tokenAuthorization, bool) {
	if a.accessTokenIssuer != nil {
		if err := a.accessTokenIssuer.Validate(token, assetID, now); err == nil {
			return tokenAuthorization{kind: "dev"}, true
		}
	}

	if a.svc == nil {
		return tokenAuthorization{}, false
	}
	payload, err := faptoken.VerifyToken(token, a.svc.IssuerPubKeyHex(), now.Unix())
	if err != nil {
		return tokenAuthorization{}, false
	}
	expectedResource := "hls:key:" + assetID
	if payload.ResourceID != expectedResource {
		return tokenAuthorization{}, false
	}
	for _, entitlement := range payload.Entitlements {
		if string(entitlement) == "hls:key" {
			return tokenAuthorization{kind: "signed", payload: &payload}, true
		}
	}
	return tokenAuthorization{}, false
}

func extractAccessToken(r *http.Request) (string, bool) {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if authz != "" {
		if len(authz) < 8 || !strings.EqualFold(authz[:7], "Bearer ") {
			return "", false
		}
		token := strings.TrimSpace(authz[7:])
		if token == "" {
			return "", false
		}
		return token, true
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		return "", false
	}
	return token, true
}

func readDeviceCookie(r *http.Request) string {
	cookie, err := r.Cookie(deviceCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func writeDeviceCookie(w http.ResponseWriter, deviceID string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     deviceCookieName,
		Value:    deviceID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func (a *API) hasValidWebhookSecret(r *http.Request) bool {
	primary := strings.TrimSpace(r.Header.Get("X-FAP-Webhook-Secret"))
	secondary := strings.TrimSpace(r.Header.Get("X-Webhook-Secret"))
	return primary == a.webhookSecret || secondary == a.webhookSecret
}

func parseLNBitsWebhookEvent(rawBody []byte) (service.LNBitsWebhookEvent, error) {
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return service.LNBitsWebhookEvent{}, err
	}

	candidates := []map[string]any{payload}
	if nested, ok := payload["data"].(map[string]any); ok {
		candidates = append(candidates, nested)
	}
	if nested, ok := payload["payment"].(map[string]any); ok {
		candidates = append(candidates, nested)
	}

	event := service.LNBitsWebhookEvent{Paid: false}
	for _, candidate := range candidates {
		if event.PaymentHash == "" {
			event.PaymentHash = firstString(candidate, "payment_hash", "paymentHash", "hash")
		}
		if event.CheckingID == "" {
			event.CheckingID = firstString(candidate, "checking_id", "checkingId")
		}
		if event.EventID == "" {
			event.EventID = firstString(candidate, "event_id", "eventId", "id")
		}
		if paidValue, ok := firstBool(candidate, "paid"); ok {
			event.Paid = paidValue
		}
		if pendingValue, ok := firstBool(candidate, "pending"); ok {
			event.Paid = !pendingValue
		}
		status := strings.ToLower(strings.TrimSpace(firstString(candidate, "status")))
		switch status {
		case "paid", "settled", "complete", "completed":
			event.Paid = true
		case "pending", "unpaid", "failed", "expired":
			event.Paid = false
		}

		if event.PaidAt == nil {
			if ts, ok := firstInt64(candidate, "paid_at", "time", "settled_at"); ok && ts > 0 {
				event.PaidAt = &ts
			}
		}
		if event.EventTime == nil {
			if ts, ok := firstInt64(candidate, "time", "timestamp", "created_at", "settled_at"); ok && ts > 0 {
				event.EventTime = &ts
			}
		}
		if event.AmountMSat <= 0 {
			if amountMSat, ok := firstInt64(candidate, "amount_msat", "msat"); ok && amountMSat > 0 {
				event.AmountMSat = amountMSat
			} else if amountSat, ok := firstInt64(candidate, "amount_sat", "sat"); ok && amountSat > 0 {
				event.AmountMSat = amountSat * 1000
			} else if amount, ok := firstInt64(candidate, "amount"); ok && amount > 0 {
				event.AmountMSat = amount
			}
		}
	}

	event.PaymentHash = strings.TrimSpace(event.PaymentHash)
	event.CheckingID = strings.TrimSpace(event.CheckingID)
	event.EventID = strings.TrimSpace(event.EventID)
	if event.PaymentHash == "" && event.CheckingID == "" {
		return service.LNBitsWebhookEvent{}, errors.New("payment_hash or checking_id is required")
	}
	return event, nil
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if raw, ok := values[key]; ok {
			if value, ok := raw.(string); ok {
				if trimmed := strings.TrimSpace(value); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

func firstBool(values map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if raw, ok := values[key]; ok {
			if value, ok := raw.(bool); ok {
				return value, true
			}
		}
	}
	return false, false
}

func firstInt64(values map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		if raw, ok := values[key]; ok {
			switch value := raw.(type) {
			case float64:
				return int64(value), true
			case int64:
				return value, true
			case json.Number:
				parsed, err := value.Int64()
				if err == nil {
					return parsed, true
				}
			case string:
				parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
				if err == nil {
					return parsed, true
				}
			}
		}
	}
	return 0, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

func writeJSON[T any](w http.ResponseWriter, status int, body T) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

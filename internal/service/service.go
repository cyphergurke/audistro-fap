package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	itoken "audistro-fap/internal/fap/token"
	"audistro-fap/internal/pay"
	"audistro-fap/internal/store"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

var (
	ErrValidation      = errors.New("validation")
	ErrNotFound        = errors.New("not found")
	ErrNotSettled      = errors.New("payment not settled")
	ErrIntentExpired   = errors.New("intent expired")
	ErrSubjectMismatch = errors.New("subject mismatch")
	ErrDeviceMismatch  = errors.New("device mismatch")
	ErrDeviceRequired  = errors.New("device required")
	ErrPaymentRequired = errors.New("payment required")
	ErrGrantExpired    = errors.New("grant expired")
)

var (
	boostAssetIDPattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
	accessEntityIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
)

const (
	maxBoostAmountMSat                 int64 = 50_000_000
	defaultMaxAccessMSat               int64 = 50_000_000
	defaultAccessMinutes               int64 = 10
	defaultWebhookRetentionSeconds     int64 = 7 * 24 * 60 * 60
	defaultWebhookPruneIntervalSeconds int64 = 5 * 60
	defaultBoostListLimit                    = 20
	maxBoostListLimit                        = 100
	defaultLedgerSummaryWindowDays           = 30
)

const (
	grantScopeHLSKey = "hls_key"
)

var allowedBoostStatuses = map[string]struct{}{
	"pending": {},
	"paid":    {},
	"expired": {},
	"failed":  {},
}

var allowedLedgerKinds = map[string]struct{}{
	"access": {},
	"boost":  {},
}

var allowedLedgerSummaryWindows = map[int]struct{}{
	7:  {},
	30: {},
}

var allowedLedgerStatuses = map[string]struct{}{
	"pending":  {},
	"paid":     {},
	"expired":  {},
	"failed":   {},
	"refunded": {},
}

type Config struct {
	IssuerPrivKeyHex                 string
	TokenTTLSeconds                  int64
	InvoiceExpirySeconds             int64
	MaxAccessAmountMSat              int64
	AccessMinutesPerPay              int64
	WebhookEventRetentionSeconds     int64
	WebhookEventPruneIntervalSeconds int64
	DevMode                          bool
}

type CreatePayeeRequest struct {
	DisplayName      string
	LNBitsBaseURL    string
	LNBitsInvoiceKey string
	LNBitsReadKey    string
}

type CreateAssetRequest struct {
	AssetID   string
	PayeeID   string
	Title     string
	PriceMSat int64
}

type Payee struct {
	PayeeID       string
	DisplayName   string
	Rail          string
	Mode          string
	LNBitsBaseURL string
	CreatedAt     int64
	UpdatedAt     int64
}

type Asset struct {
	AssetID    string
	PayeeID    string
	Title      string
	PriceMSat  int64
	ResourceID string
	CreatedAt  int64
	UpdatedAt  int64
}

type Challenge struct {
	ChallengeID string
	IntentID    string
	DeviceID    string
	AssetID     string
	PayeeID     string
	Bolt11      string
	PaymentHash string
	CheckingID  string
	ExpiresAt   int64
	AmountMSat  int64
	ResourceID  string
	Status      string
	PaidAt      *int64
}

type CreateChallengeRequest struct {
	DeviceID       string
	AssetID        string
	PayeeID        string
	AmountMSat     int64
	Memo           string
	IdempotencyKey string
	Subject        string
}

type AccessToken struct {
	Token      string
	ExpiresAt  int64
	ResourceID string
}

type Device struct {
	DeviceID   string
	CreatedAt  int64
	LastSeenAt int64
}

type AccessGrant struct {
	GrantID          string
	DeviceID         string
	AssetID          string
	Scope            string
	MinutesPurchased int64
	ValidFrom        *int64
	ValidUntil       *int64
	Status           string
	ChallengeID      string
	AmountMSat       int64
	CreatedAt        int64
	UpdatedAt        int64
}

type CreateBoostRequest struct {
	DeviceID       string
	AssetID        string
	PayeeID        string
	AmountMSat     int64
	Memo           string
	IdempotencyKey string
}

type Boost struct {
	BoostID              string
	DeviceID             string
	AssetID              string
	PayeeID              string
	AmountMSat           int64
	Bolt11               string
	LNBitsPaymentHash    string
	LNBitsCheckingID     string
	LNBitsWebhookEventID string
	Status               string
	ExpiresAt            int64
	PaidAt               *int64
	CreatedAt            int64
	UpdatedAt            int64
	IdempotencyKey       string
}

type LedgerEntry struct {
	EntryID     string
	DeviceID    string
	Kind        string
	Status      string
	AssetID     string
	PayeeID     string
	AmountMSat  int64
	Currency    string
	RelatedID   string
	ReferenceID string
	Memo        string
	CreatedAt   int64
	UpdatedAt   int64
	PaidAt      *int64
}

type ListLedgerEntriesRequest struct {
	DeviceID string
	Kind     string
	Status   string
	AssetID  string
	Limit    int
	Cursor   string
}

type LedgerListResult struct {
	Items      []LedgerEntry
	NextCursor string
}

type GetLedgerSummaryRequest struct {
	DeviceID   string
	WindowDays int
	Kind       string
	Limit      int
}

type GetLedgerReportRequest struct {
	DeviceID string
	Month    string
}

type LedgerSummaryTotals struct {
	PaidMSatAccess int64
	PaidMSatBoost  int64
	PaidMSatTotal  int64
}

type LedgerSummaryAsset struct {
	AssetID    string
	AmountMSat int64
}

type LedgerSummaryPayee struct {
	PayeeID    string
	AmountMSat int64
}

type LedgerSummaryCounts struct {
	PaidEntries    int64
	PendingEntries int64
}

type LedgerSummaryResult struct {
	DeviceID   string
	WindowDays int
	FromUnix   int64
	ToUnix     int64
	Totals     LedgerSummaryTotals
	TopAssets  []LedgerSummaryAsset
	TopPayees  []LedgerSummaryPayee
	Counts     LedgerSummaryCounts
}

type LedgerReportResult struct {
	DeviceID    string
	PeriodStart int64
	PeriodEnd   int64
	Totals      LedgerSummaryTotals
	ByPayee     []LedgerSummaryPayee
	ByAsset     []LedgerSummaryAsset
	ComputedAt  int64
}

type LNBitsWebhookEvent struct {
	PaymentHash string
	CheckingID  string
	EventID     string
	Paid        bool
	PaidAt      *int64
	AmountMSat  int64
	EventTime   *int64
}

type ListBoostsRequest struct {
	AssetID string
	PayeeID string
	Status  string
	Limit   int
	Cursor  string
}

type BoostListResult struct {
	Items      []Boost
	NextCursor string
}

type EncryptFunc func(masterKey []byte, plaintext []byte) ([]byte, error)

type FAPService struct {
	store              store.Repository
	adapters           pay.PayeeAdapterFactory
	cfg                Config
	issuerPub          string
	signToken          func(payload itoken.AccessTokenPayload) (string, error)
	nowUnix            func() int64
	webhookPruneMu     sync.Mutex
	nextWebhookPruneAt int64
}

func New(storeRepo store.Repository, adapters pay.PayeeAdapterFactory, cfg Config) (*FAPService, error) {
	if storeRepo == nil {
		return nil, fmt.Errorf("store is required")
	}
	if adapters == nil {
		return nil, fmt.Errorf("adapter factory is required")
	}
	if cfg.TokenTTLSeconds <= 0 {
		return nil, fmt.Errorf("token ttl must be > 0")
	}
	if cfg.InvoiceExpirySeconds <= 0 {
		return nil, fmt.Errorf("invoice expiry must be > 0")
	}
	if cfg.MaxAccessAmountMSat <= 0 {
		cfg.MaxAccessAmountMSat = defaultMaxAccessMSat
	}
	if cfg.AccessMinutesPerPay <= 0 {
		cfg.AccessMinutesPerPay = defaultAccessMinutes
	}
	if cfg.WebhookEventRetentionSeconds <= 0 {
		cfg.WebhookEventRetentionSeconds = defaultWebhookRetentionSeconds
	}
	if cfg.WebhookEventPruneIntervalSeconds <= 0 {
		cfg.WebhookEventPruneIntervalSeconds = defaultWebhookPruneIntervalSeconds
	}
	privRaw, err := hex.DecodeString(cfg.IssuerPrivKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode issuer private key: %w", err)
	}
	if len(privRaw) != 32 {
		return nil, fmt.Errorf("issuer private key must be 32 bytes")
	}
	priv, _ := btcec.PrivKeyFromBytes(privRaw)
	issuerPub := hex.EncodeToString(schnorr.SerializePubKey(priv.PubKey()))

	return &FAPService{
		store:     storeRepo,
		adapters:  adapters,
		cfg:       cfg,
		issuerPub: issuerPub,
		signToken: func(payload itoken.AccessTokenPayload) (string, error) {
			return itoken.SignToken(payload, priv)
		},
		nowUnix: func() int64 { return time.Now().Unix() },
	}, nil
}

func (s *FAPService) IssuerPubKeyHex() string { return s.issuerPub }

func (s *FAPService) CreatePayee(ctx context.Context, req CreatePayeeRequest, encrypt EncryptFunc, masterKey []byte) (Payee, error) {
	if strings.TrimSpace(req.DisplayName) == "" {
		return Payee{}, fmt.Errorf("%w: display_name is required", ErrValidation)
	}
	if strings.TrimSpace(req.LNBitsBaseURL) == "" || strings.TrimSpace(req.LNBitsInvoiceKey) == "" || strings.TrimSpace(req.LNBitsReadKey) == "" {
		return Payee{}, fmt.Errorf("%w: lnbits config is required", ErrValidation)
	}

	encInvoice, err := encrypt(masterKey, []byte(req.LNBitsInvoiceKey))
	if err != nil {
		return Payee{}, fmt.Errorf("encrypt invoice key: %w", err)
	}
	encRead, err := encrypt(masterKey, []byte(req.LNBitsReadKey))
	if err != nil {
		return Payee{}, fmt.Errorf("encrypt read key: %w", err)
	}

	now := s.nowUnix()
	id, err := randomID()
	if err != nil {
		return Payee{}, err
	}
	p := store.Payee{
		PayeeID:             id,
		DisplayName:         req.DisplayName,
		Rail:                "lightning",
		Mode:                "lnbits",
		LNBitsBaseURL:       req.LNBitsBaseURL,
		LNBitsInvoiceKeyEnc: encInvoice,
		LNBitsReadKeyEnc:    encRead,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.store.CreatePayee(ctx, p); err != nil {
		return Payee{}, err
	}
	return Payee{
		PayeeID: p.PayeeID, DisplayName: p.DisplayName,
		Rail: p.Rail, Mode: p.Mode, LNBitsBaseURL: p.LNBitsBaseURL,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}, nil
}

func (s *FAPService) CreateAsset(ctx context.Context, req CreateAssetRequest) (Asset, error) {
	if strings.TrimSpace(req.AssetID) == "" || strings.TrimSpace(req.PayeeID) == "" || strings.TrimSpace(req.Title) == "" {
		return Asset{}, fmt.Errorf("%w: asset_id, payee_id, title are required", ErrValidation)
	}
	if req.PriceMSat <= 0 {
		return Asset{}, fmt.Errorf("%w: price_msat must be > 0", ErrValidation)
	}
	if _, err := s.store.GetByID(ctx, req.PayeeID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Asset{}, fmt.Errorf("%w: payee", ErrNotFound)
		}
		return Asset{}, err
	}
	now := s.nowUnix()
	a := store.Asset{
		AssetID: req.AssetID, PayeeID: req.PayeeID, Title: req.Title,
		PriceMSat: req.PriceMSat, ResourceID: "hls:key:" + req.AssetID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateAsset(ctx, a); err != nil {
		return Asset{}, err
	}
	return Asset{
		AssetID: a.AssetID, PayeeID: a.PayeeID, Title: a.Title, PriceMSat: a.PriceMSat,
		ResourceID: a.ResourceID, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}, nil
}

func (s *FAPService) BootstrapDevice(ctx context.Context, existingDeviceID string, nowUnix int64) (Device, error) {
	if nowUnix <= 0 {
		nowUnix = s.nowUnix()
	}
	deviceID := strings.TrimSpace(existingDeviceID)
	if deviceID != "" {
		if !accessEntityIDPattern.MatchString(deviceID) {
			return Device{}, fmt.Errorf("%w: device_id is invalid", ErrValidation)
		}
		current, err := s.store.GetDeviceByID(ctx, deviceID)
		if err == nil {
			if touchErr := s.store.TouchDevice(ctx, deviceID, nowUnix); touchErr != nil && !errors.Is(touchErr, store.ErrNotFound) {
				return Device{}, touchErr
			}
			current.LastSeenAt = nowUnix
			return Device{
				DeviceID:   current.DeviceID,
				CreatedAt:  current.CreatedAt,
				LastSeenAt: current.LastSeenAt,
			}, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return Device{}, err
		}
	}

	newID, err := randomID()
	if err != nil {
		return Device{}, err
	}
	row := store.Device{
		DeviceID:   newID,
		CreatedAt:  nowUnix,
		LastSeenAt: nowUnix,
	}
	if err := s.store.CreateDevice(ctx, row); err != nil {
		return Device{}, err
	}
	return Device{
		DeviceID:   row.DeviceID,
		CreatedAt:  row.CreatedAt,
		LastSeenAt: row.LastSeenAt,
	}, nil
}

func (s *FAPService) ListAccessGrantsForDevice(ctx context.Context, deviceID string, assetID string, nowUnix int64) ([]AccessGrant, error) {
	deviceID = strings.TrimSpace(deviceID)
	if !accessEntityIDPattern.MatchString(deviceID) {
		return nil, fmt.Errorf("%w: device_id is invalid", ErrValidation)
	}
	assetID = strings.TrimSpace(assetID)
	if assetID != "" && !accessEntityIDPattern.MatchString(assetID) {
		return nil, fmt.Errorf("%w: asset_id is invalid", ErrValidation)
	}
	if nowUnix <= 0 {
		nowUnix = s.nowUnix()
	}

	rows, err := s.store.ListAccessGrantsByDevice(ctx, deviceID, assetID)
	if err != nil {
		return nil, err
	}

	items := make([]AccessGrant, 0, len(rows))
	for _, row := range rows {
		if row.Status == "active" && row.ValidUntil != nil && nowUnix > *row.ValidUntil {
			if updateErr := s.store.UpdateAccessGrantStatus(ctx, row.GrantID, "expired", nowUnix); updateErr == nil {
				row.Status = "expired"
				row.UpdatedAt = nowUnix
			}
		}
		items = append(items, toAccessGrant(row))
	}
	return items, nil
}

func (s *FAPService) AuthorizeKeyAccess(ctx context.Context, deviceID string, assetID string, nowUnix int64) error {
	deviceID = strings.TrimSpace(deviceID)
	assetID = strings.TrimSpace(assetID)
	if !accessEntityIDPattern.MatchString(deviceID) {
		return fmt.Errorf("%w: device_id is invalid", ErrValidation)
	}
	if !accessEntityIDPattern.MatchString(assetID) {
		return fmt.Errorf("%w: asset_id is invalid", ErrValidation)
	}
	if nowUnix <= 0 {
		nowUnix = s.nowUnix()
	}

	grant, err := s.store.GetLatestAccessGrantByDeviceAsset(ctx, deviceID, assetID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrPaymentRequired
		}
		return err
	}
	if grant.Status == "revoked" {
		return ErrPaymentRequired
	}
	if grant.Status == "expired" {
		return ErrGrantExpired
	}
	if grant.Status != "active" {
		return ErrPaymentRequired
	}

	if grant.ValidUntil == nil {
		if grant.MinutesPurchased <= 0 {
			return fmt.Errorf("%w: grant minutes_purchased must be > 0", ErrValidation)
		}
		validFrom := nowUnix
		validUntil := nowUnix + (grant.MinutesPurchased * 60)
		if err := s.store.ActivateAccessGrant(ctx, grant.GrantID, validFrom, validUntil, nowUnix); err != nil {
			return err
		}
		grant.ValidFrom = &validFrom
		grant.ValidUntil = &validUntil
	}

	if grant.ValidUntil != nil && nowUnix > *grant.ValidUntil {
		_ = s.store.UpdateAccessGrantStatus(ctx, grant.GrantID, "expired", nowUnix)
		return ErrGrantExpired
	}
	return nil
}

func (s *FAPService) CreateChallenge(ctx context.Context, req CreateChallengeRequest) (Challenge, error) {
	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID != "" && !accessEntityIDPattern.MatchString(deviceID) {
		return Challenge{}, fmt.Errorf("%w: device_id is invalid", ErrValidation)
	}

	assetID := strings.TrimSpace(req.AssetID)
	if !accessEntityIDPattern.MatchString(assetID) {
		return Challenge{}, fmt.Errorf("%w: asset_id is invalid", ErrValidation)
	}

	payeeID := strings.TrimSpace(req.PayeeID)
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	memo := strings.TrimSpace(req.Memo)
	isCatalogMode := payeeID != "" || req.AmountMSat > 0 || idempotencyKey != "" || memo != ""
	if !isCatalogMode {
		subject := strings.TrimSpace(req.Subject)
		if subject == "" {
			return Challenge{}, fmt.Errorf("%w: subject is required for legacy challenge mode", ErrValidation)
		}
		return s.createLegacyChallenge(ctx, assetID)
	}

	if !accessEntityIDPattern.MatchString(payeeID) {
		return Challenge{}, fmt.Errorf("%w: payee_id is invalid", ErrValidation)
	}
	if req.AmountMSat <= 0 || req.AmountMSat > s.cfg.MaxAccessAmountMSat {
		return Challenge{}, fmt.Errorf("%w: amount_msat must be 1..%d", ErrValidation, s.cfg.MaxAccessAmountMSat)
	}
	if idempotencyKey != "" && len(idempotencyKey) > 128 {
		return Challenge{}, fmt.Errorf("%w: idempotency_key must be <= 128 chars", ErrValidation)
	}
	if len(memo) > 256 {
		return Challenge{}, fmt.Errorf("%w: memo must be <= 256 chars", ErrValidation)
	}

	if idempotencyKey != "" {
		existing, err := s.store.GetChallengeByIdempotencyKey(ctx, idempotencyKey)
		if err == nil {
			if deviceID != "" && existing.DeviceID != "" && existing.DeviceID != deviceID {
				return Challenge{}, ErrDeviceMismatch
			}
			if ledgerErr := s.ensureLedgerEntryForChallenge(ctx, existing); ledgerErr != nil {
				return Challenge{}, ledgerErr
			}
			return toChallenge(existing), nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return Challenge{}, err
		}
	}

	adapter, err := s.adapters.ForPayee(ctx, payeeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Challenge{}, fmt.Errorf("%w: payee", ErrNotFound)
		}
		return Challenge{}, err
	}
	if memo == "" {
		memo = "FAP access asset:" + assetID
	}
	bolt11, paymentHash, checkingID, expiresAt, err := adapter.CreateInvoice(ctx, req.AmountMSat, memo, s.cfg.InvoiceExpirySeconds)
	if err != nil {
		return Challenge{}, err
	}
	if strings.TrimSpace(bolt11) == "" || strings.TrimSpace(paymentHash) == "" {
		return Challenge{}, fmt.Errorf("lnbits invoice response missing required fields")
	}

	challengeID, err := randomID()
	if err != nil {
		return Challenge{}, err
	}
	now := s.nowUnix()
	checkingID = strings.TrimSpace(checkingID)
	if checkingID == "" {
		checkingID = strings.TrimSpace(paymentHash)
	}
	var idempotencyKeyPtr *string
	if idempotencyKey != "" {
		idempotencyKeyPtr = &idempotencyKey
	}
	challenge := store.AccessChallenge{
		ChallengeID:       challengeID,
		DeviceID:          deviceID,
		AssetID:           assetID,
		PayeeID:           payeeID,
		AmountMSat:        req.AmountMSat,
		Memo:              memo,
		Status:            "pending",
		Bolt11:            bolt11,
		LNBitsCheckingID:  checkingID,
		LNBitsPaymentHash: strings.TrimSpace(paymentHash),
		ExpiresAt:         expiresAt,
		CreatedAt:         now,
		UpdatedAt:         now,
		IdempotencyKey:    idempotencyKeyPtr,
	}
	if err := s.store.CreateChallenge(ctx, challenge); err != nil {
		if idempotencyKey != "" && isUniqueConstraint(err) {
			if existing, getErr := s.store.GetChallengeByIdempotencyKey(ctx, idempotencyKey); getErr == nil {
				if ledgerErr := s.ensureLedgerEntryForChallenge(ctx, existing); ledgerErr != nil {
					return Challenge{}, ledgerErr
				}
				return toChallenge(existing), nil
			}
		}
		return Challenge{}, err
	}
	if err := s.ensureLedgerEntryForChallenge(ctx, challenge); err != nil {
		return Challenge{}, err
	}
	return toChallenge(challenge), nil
}

func (s *FAPService) createLegacyChallenge(ctx context.Context, assetID string) (Challenge, error) {
	asset, err := s.store.GetAssetByID(ctx, assetID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Challenge{}, fmt.Errorf("%w: asset", ErrNotFound)
		}
		return Challenge{}, err
	}
	adapter, err := s.adapters.ForPayee(ctx, asset.PayeeID)
	if err != nil {
		return Challenge{}, err
	}
	memo := "FAP asset:" + asset.AssetID
	bolt11, paymentHash, checkingID, expiresAt, err := adapter.CreateInvoice(ctx, asset.PriceMSat, memo, s.cfg.InvoiceExpirySeconds)
	if err != nil {
		return Challenge{}, err
	}
	intentID, err := randomID()
	if err != nil {
		return Challenge{}, err
	}
	intent := store.PaymentIntent{
		IntentID: intentID, AssetID: asset.AssetID, PayeeID: asset.PayeeID,
		AmountMSat: asset.PriceMSat, Bolt11: bolt11, PaymentHash: paymentHash,
		Status: "pending", InvoiceExpiresAt: expiresAt, CreatedAt: s.nowUnix(),
	}
	if err := s.store.CreateIntent(ctx, intent); err != nil {
		return Challenge{}, err
	}
	return Challenge{
		ChallengeID: intent.IntentID,
		IntentID:    intent.IntentID,
		AssetID:     asset.AssetID,
		PayeeID:     asset.PayeeID,
		Bolt11:      bolt11,
		PaymentHash: paymentHash,
		CheckingID:  strings.TrimSpace(checkingID),
		ExpiresAt:   expiresAt,
		AmountMSat:  asset.PriceMSat,
		ResourceID:  asset.ResourceID,
		Status:      "pending",
	}, nil
}

func (s *FAPService) CreateBoost(ctx context.Context, req CreateBoostRequest) (Boost, error) {
	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" {
		return Boost{}, ErrDeviceRequired
	}
	if !accessEntityIDPattern.MatchString(deviceID) {
		return Boost{}, fmt.Errorf("%w: device_id is invalid", ErrValidation)
	}

	assetID := strings.TrimSpace(req.AssetID)
	if !boostAssetIDPattern.MatchString(assetID) {
		return Boost{}, fmt.Errorf("%w: asset_id is invalid", ErrValidation)
	}
	payeeID := strings.TrimSpace(req.PayeeID)
	if payeeID == "" {
		return Boost{}, fmt.Errorf("%w: payee_id is required", ErrValidation)
	}
	if req.AmountMSat <= 0 || req.AmountMSat > maxBoostAmountMSat {
		return Boost{}, fmt.Errorf("%w: amount_msat must be 1..%d", ErrValidation, maxBoostAmountMSat)
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		return Boost{}, fmt.Errorf("%w: idempotency_key is required (1..128 chars)", ErrValidation)
	}
	if len(strings.TrimSpace(req.Memo)) > 256 {
		return Boost{}, fmt.Errorf("%w: memo must be <= 256 chars", ErrValidation)
	}

	existing, err := s.store.GetBoostByIdempotencyKey(ctx, idempotencyKey)
	if err == nil {
		if strings.TrimSpace(existing.DeviceID) != "" && strings.TrimSpace(existing.DeviceID) != deviceID {
			return Boost{}, ErrDeviceMismatch
		}
		if ledgerErr := s.ensureLedgerEntryForBoost(ctx, existing); ledgerErr != nil {
			return Boost{}, ledgerErr
		}
		return toBoost(existing), nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Boost{}, err
	}

	now := s.nowUnix()
	boostID, err := randomID()
	if err != nil {
		return Boost{}, err
	}
	expiresAt := now + s.cfg.InvoiceExpirySeconds
	bolt11 := buildFakeBolt11(req.AmountMSat, boostID)
	lnbitsPaymentHash := ""
	lnbitsCheckingID := ""
	if !s.cfg.DevMode {
		adapter, err := s.adapters.ForPayee(ctx, payeeID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return Boost{}, fmt.Errorf("%w: payee", ErrNotFound)
			}
			return Boost{}, err
		}
		memo := strings.TrimSpace(req.Memo)
		if memo == "" {
			memo = "Boost asset:" + assetID
		}
		var checkingID string
		bolt11, lnbitsPaymentHash, checkingID, expiresAt, err = adapter.CreateInvoice(ctx, req.AmountMSat, memo, s.cfg.InvoiceExpirySeconds)
		if err != nil {
			return Boost{}, err
		}
		if strings.TrimSpace(bolt11) == "" || strings.TrimSpace(lnbitsPaymentHash) == "" {
			return Boost{}, fmt.Errorf("lnbits invoice response missing required fields")
		}
		lnbitsCheckingID = strings.TrimSpace(checkingID)
		if lnbitsCheckingID == "" {
			lnbitsCheckingID = lnbitsPaymentHash
		}
	}
	boost := store.Boost{
		BoostID:              boostID,
		DeviceID:             deviceID,
		AssetID:              assetID,
		PayeeID:              payeeID,
		AmountMSat:           req.AmountMSat,
		Bolt11:               bolt11,
		LNBitsPaymentHash:    lnbitsPaymentHash,
		LNBitsCheckingID:     lnbitsCheckingID,
		LNBitsWebhookEventID: "",
		Status:               "pending",
		ExpiresAt:            expiresAt,
		PaidAt:               nil,
		CreatedAt:            now,
		UpdatedAt:            now,
		IdempotencyKey:       idempotencyKey,
	}
	if err := s.store.CreateBoost(ctx, boost); err != nil {
		if isUniqueConstraint(err) {
			existing, getErr := s.store.GetBoostByIdempotencyKey(ctx, idempotencyKey)
			if getErr == nil {
				if strings.TrimSpace(existing.DeviceID) != "" && strings.TrimSpace(existing.DeviceID) != deviceID {
					return Boost{}, ErrDeviceMismatch
				}
				if ledgerErr := s.ensureLedgerEntryForBoost(ctx, existing); ledgerErr != nil {
					return Boost{}, ledgerErr
				}
				return toBoost(existing), nil
			}
		}
		return Boost{}, err
	}
	if err := s.ensureLedgerEntryForBoost(ctx, boost); err != nil {
		return Boost{}, err
	}
	return toBoost(boost), nil
}

func (s *FAPService) GetBoost(ctx context.Context, boostID string) (Boost, error) {
	boostID = strings.TrimSpace(boostID)
	if boostID == "" || len(boostID) > 128 {
		return Boost{}, fmt.Errorf("%w: boost_id is invalid", ErrValidation)
	}
	value, err := s.store.GetBoostByID(ctx, boostID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Boost{}, ErrNotFound
		}
		return Boost{}, err
	}
	if value.Status == "pending" && s.nowUnix() > value.ExpiresAt {
		now := s.nowUnix()
		if updateErr := s.store.UpdateBoostStatus(ctx, value.BoostID, "expired", nil, now); updateErr == nil {
			_ = s.updateLedgerStatus(ctx, "boost", value.BoostID, "expired", nil, value.LNBitsCheckingID, now)
			value.Status = "expired"
			value.PaidAt = nil
			value.UpdatedAt = now
		}
	}
	return toBoost(value), nil
}

func (s *FAPService) ListBoosts(ctx context.Context, req ListBoostsRequest) (BoostListResult, error) {
	assetID := strings.TrimSpace(req.AssetID)
	if assetID != "" && !boostAssetIDPattern.MatchString(assetID) {
		return BoostListResult{}, fmt.Errorf("%w: asset_id is invalid", ErrValidation)
	}

	payeeID := strings.TrimSpace(req.PayeeID)
	if len(payeeID) > 128 {
		return BoostListResult{}, fmt.Errorf("%w: payee_id is invalid", ErrValidation)
	}

	status := strings.TrimSpace(req.Status)
	if status != "" {
		if _, ok := allowedBoostStatuses[status]; !ok {
			return BoostListResult{}, fmt.Errorf("%w: status is invalid", ErrValidation)
		}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultBoostListLimit
	}
	if limit > maxBoostListLimit {
		limit = maxBoostListLimit
	}

	cursor, err := parseBoostCursor(req.Cursor)
	if err != nil {
		return BoostListResult{}, fmt.Errorf("%w: cursor is invalid", ErrValidation)
	}

	values, err := s.store.ListBoosts(ctx, store.ListBoostsParams{
		AssetID: assetID,
		PayeeID: payeeID,
		Status:  status,
		Limit:   limit + 1,
		Cursor:  cursor,
	})
	if err != nil {
		return BoostListResult{}, err
	}

	now := s.nowUnix()
	for index := range values {
		if values[index].Status == "pending" && now > values[index].ExpiresAt {
			if updateErr := s.store.UpdateBoostStatus(ctx, values[index].BoostID, "expired", nil, now); updateErr == nil {
				_ = s.updateLedgerStatus(ctx, "boost", values[index].BoostID, "expired", nil, values[index].LNBitsCheckingID, now)
				values[index].Status = "expired"
				values[index].PaidAt = nil
				values[index].UpdatedAt = now
			}
		}
	}

	nextCursor := ""
	if len(values) > limit {
		cutoff := values[limit-1]
		nextCursor = encodeBoostCursor(cutoff.CreatedAt, cutoff.BoostID)
		values = values[:limit]
	}

	items := make([]Boost, 0, len(values))
	for _, value := range values {
		items = append(items, toBoost(value))
	}
	return BoostListResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (s *FAPService) ListLedgerEntriesForDevice(ctx context.Context, req ListLedgerEntriesRequest) (LedgerListResult, error) {
	deviceID := strings.TrimSpace(req.DeviceID)
	if !accessEntityIDPattern.MatchString(deviceID) {
		return LedgerListResult{}, fmt.Errorf("%w: device_id is invalid", ErrValidation)
	}

	kind := strings.TrimSpace(req.Kind)
	if kind != "" {
		if _, ok := allowedLedgerKinds[kind]; !ok {
			return LedgerListResult{}, fmt.Errorf("%w: kind is invalid", ErrValidation)
		}
	}
	status := strings.TrimSpace(req.Status)
	if status != "" {
		if _, ok := allowedLedgerStatuses[status]; !ok {
			return LedgerListResult{}, fmt.Errorf("%w: status is invalid", ErrValidation)
		}
	}
	assetID := strings.TrimSpace(req.AssetID)
	if assetID != "" && !boostAssetIDPattern.MatchString(assetID) {
		return LedgerListResult{}, fmt.Errorf("%w: asset_id is invalid", ErrValidation)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultBoostListLimit
	}
	if limit > maxBoostListLimit {
		limit = maxBoostListLimit
	}

	cursor, err := parseLedgerCursor(req.Cursor)
	if err != nil {
		return LedgerListResult{}, fmt.Errorf("%w: cursor is invalid", ErrValidation)
	}

	values, err := s.store.ListLedgerEntriesForDevice(ctx, store.ListLedgerEntriesParams{
		DeviceID: deviceID,
		Kind:     kind,
		Status:   status,
		AssetID:  assetID,
		Limit:    limit + 1,
		Cursor:   cursor,
	})
	if err != nil {
		return LedgerListResult{}, err
	}

	nextCursor := ""
	if len(values) > limit {
		cutoff := values[limit-1]
		nextCursor = encodeLedgerCursor(cutoff.CreatedAt, cutoff.EntryID)
		values = values[:limit]
	}

	items := make([]LedgerEntry, 0, len(values))
	for _, value := range values {
		items = append(items, toLedgerEntry(value))
	}
	return LedgerListResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (s *FAPService) GetLedgerSummaryForDevice(ctx context.Context, req GetLedgerSummaryRequest) (LedgerSummaryResult, error) {
	deviceID := strings.TrimSpace(req.DeviceID)
	if !accessEntityIDPattern.MatchString(deviceID) {
		return LedgerSummaryResult{}, fmt.Errorf("%w: device_id is invalid", ErrValidation)
	}

	windowDays := req.WindowDays
	if windowDays == 0 {
		windowDays = defaultLedgerSummaryWindowDays
	}
	if _, ok := allowedLedgerSummaryWindows[windowDays]; !ok {
		return LedgerSummaryResult{}, fmt.Errorf("%w: window_days is invalid", ErrValidation)
	}

	kind := strings.TrimSpace(req.Kind)
	if kind != "" {
		if _, ok := allowedLedgerKinds[kind]; !ok {
			return LedgerSummaryResult{}, fmt.Errorf("%w: kind is invalid", ErrValidation)
		}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultBoostListLimit
	}
	if limit > maxBoostListLimit {
		limit = maxBoostListLimit
	}

	toUnix := s.nowUnix()
	fromUnix := toUnix - int64(windowDays*24*60*60)

	values, err := s.store.GetLedgerSummaryForDevice(ctx, store.LedgerSummaryParams{
		DeviceID: deviceID,
		FromUnix: fromUnix,
		ToUnix:   toUnix,
		Kind:     kind,
		Limit:    limit,
	})
	if err != nil {
		return LedgerSummaryResult{}, err
	}

	topAssets := make([]LedgerSummaryAsset, 0, len(values.TopAssets))
	for _, item := range values.TopAssets {
		topAssets = append(topAssets, LedgerSummaryAsset{
			AssetID:    item.AssetID,
			AmountMSat: item.AmountMSat,
		})
	}

	topPayees := make([]LedgerSummaryPayee, 0, len(values.TopPayees))
	for _, item := range values.TopPayees {
		topPayees = append(topPayees, LedgerSummaryPayee{
			PayeeID:    item.PayeeID,
			AmountMSat: item.AmountMSat,
		})
	}

	return LedgerSummaryResult{
		DeviceID:   deviceID,
		WindowDays: windowDays,
		FromUnix:   fromUnix,
		ToUnix:     toUnix,
		Totals: LedgerSummaryTotals{
			PaidMSatAccess: values.Totals.PaidMSatAccess,
			PaidMSatBoost:  values.Totals.PaidMSatBoost,
			PaidMSatTotal:  values.Totals.PaidMSatTotal,
		},
		TopAssets: topAssets,
		TopPayees: topPayees,
		Counts: LedgerSummaryCounts{
			PaidEntries:    values.Counts.PaidEntries,
			PendingEntries: values.Counts.PendingEntries,
		},
	}, nil
}

func (s *FAPService) GetLedgerReportForDevice(ctx context.Context, req GetLedgerReportRequest) (LedgerReportResult, error) {
	deviceID := strings.TrimSpace(req.DeviceID)
	if !accessEntityIDPattern.MatchString(deviceID) {
		return LedgerReportResult{}, fmt.Errorf("%w: device_id is invalid", ErrValidation)
	}

	periodStart, periodEnd, err := ledgerReportPeriodBounds(req.Month, s.nowUnix())
	if err != nil {
		return LedgerReportResult{}, fmt.Errorf("%w: month is invalid", ErrValidation)
	}

	report, err := s.store.GetLedgerReportByDevicePeriod(ctx, deviceID, periodStart, periodEnd)
	reportMissing := errors.Is(err, store.ErrNotFound)
	if err != nil && !reportMissing {
		return LedgerReportResult{}, err
	}

	maxPaidAt, err := s.store.GetLedgerReportMaxPaidAt(ctx, deviceID, periodStart, periodEnd)
	if err != nil {
		return LedgerReportResult{}, err
	}

	needsRecompute := reportMissing
	if !reportMissing {
		if report.Status != "computed" {
			needsRecompute = true
		}
		if maxPaidAt != nil && *maxPaidAt > report.UpdatedAt {
			needsRecompute = true
		}
	}

	if needsRecompute {
		if report.ReportID != "" {
			if markErr := s.store.UpdateLedgerReportStatus(ctx, report.ReportID, "stale"); markErr != nil {
				return LedgerReportResult{}, markErr
			}
		}

		computed, computeErr := s.store.ComputeLedgerReportForDevice(ctx, deviceID, periodStart, periodEnd)
		if computeErr != nil {
			return LedgerReportResult{}, computeErr
		}

		nowUnix := s.nowUnix()
		computed.ReportID = ledgerReportID(deviceID, periodStart, periodEnd)
		computed.DeviceID = deviceID
		computed.PeriodStart = periodStart
		computed.PeriodEnd = periodEnd
		computed.Status = "computed"
		computed.CreatedAt = nowUnix
		computed.UpdatedAt = nowUnix
		if report.ReportID != "" && report.CreatedAt > 0 {
			computed.CreatedAt = report.CreatedAt
		}

		if err := s.store.UpsertLedgerReport(ctx, computed); err != nil {
			return LedgerReportResult{}, err
		}
		report = computed
	}

	byPayee := make([]LedgerSummaryPayee, 0, len(report.ByPayee))
	for _, item := range report.ByPayee {
		byPayee = append(byPayee, LedgerSummaryPayee{
			PayeeID:    item.Key,
			AmountMSat: item.AmountMSat,
		})
	}

	byAsset := make([]LedgerSummaryAsset, 0, len(report.ByAsset))
	for _, item := range report.ByAsset {
		byAsset = append(byAsset, LedgerSummaryAsset{
			AssetID:    item.Key,
			AmountMSat: item.AmountMSat,
		})
	}

	return LedgerReportResult{
		DeviceID:    deviceID,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Totals: LedgerSummaryTotals{
			PaidMSatAccess: report.TotalsPaidMSatAccess,
			PaidMSatBoost:  report.TotalsPaidMSatBoost,
			PaidMSatTotal:  report.TotalsPaidMSatTotal,
		},
		ByPayee:    byPayee,
		ByAsset:    byAsset,
		ComputedAt: report.UpdatedAt,
	}, nil
}

func ledgerReportPeriodBounds(month string, nowUnix int64) (int64, int64, error) {
	trimmed := strings.TrimSpace(month)
	var start time.Time
	if trimmed == "" {
		now := time.Unix(nowUnix, 0).UTC()
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	} else {
		parsed, err := time.Parse("2006-01", trimmed)
		if err != nil {
			return 0, 0, err
		}
		start = time.Date(parsed.Year(), parsed.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	end := start.AddDate(0, 1, 0)
	return start.Unix(), end.Unix(), nil
}

func ledgerReportID(deviceID string, periodStart int64, periodEnd int64) string {
	return fmt.Sprintf("lr_%s_%d_%d", deviceID, periodStart, periodEnd)
}

func (s *FAPService) MarkBoostPaid(ctx context.Context, boostID string, nowUnix int64) (Boost, error) {
	boost, err := s.GetBoost(ctx, boostID)
	if err != nil {
		return Boost{}, err
	}
	if boost.Status == "paid" {
		return boost, nil
	}
	if boost.Status != "pending" {
		return Boost{}, fmt.Errorf("%w: boost status cannot transition to paid", ErrValidation)
	}
	if nowUnix <= 0 {
		nowUnix = s.nowUnix()
	}
	if err := s.ensureLedgerEntryForBoost(ctx, store.Boost{
		BoostID:           boost.BoostID,
		DeviceID:          boost.DeviceID,
		AssetID:           boost.AssetID,
		PayeeID:           boost.PayeeID,
		AmountMSat:        boost.AmountMSat,
		LNBitsPaymentHash: boost.LNBitsPaymentHash,
		LNBitsCheckingID:  boost.LNBitsCheckingID,
		Status:            boost.Status,
		CreatedAt:         boost.CreatedAt,
		UpdatedAt:         boost.UpdatedAt,
		PaidAt:            boost.PaidAt,
	}); err != nil {
		return Boost{}, err
	}
	if err := s.store.UpdateBoostStatus(ctx, boost.BoostID, "paid", &nowUnix, nowUnix); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Boost{}, ErrNotFound
		}
		return Boost{}, err
	}
	if err := s.updateLedgerStatus(ctx, "boost", boost.BoostID, "paid", &nowUnix, boost.LNBitsCheckingID, nowUnix); err != nil {
		return Boost{}, err
	}
	updated, err := s.store.GetBoostByID(ctx, boost.BoostID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Boost{}, ErrNotFound
		}
		return Boost{}, err
	}
	return toBoost(updated), nil
}

func (s *FAPService) HandleWebhook(ctx context.Context, paymentHash string, nowUnix int64) error {
	if strings.TrimSpace(paymentHash) == "" {
		return fmt.Errorf("%w: payment_hash is required", ErrValidation)
	}
	intent, err := s.store.GetIntentByPaymentHash(ctx, paymentHash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	adapter, err := s.adapters.ForPayee(ctx, intent.PayeeID)
	if err != nil {
		return err
	}
	settled, settledAt, err := adapter.IsSettled(ctx, paymentHash)
	if err != nil {
		return err
	}
	if !settled {
		return nil
	}
	if nowUnix == 0 {
		nowUnix = s.nowUnix()
	}
	if settledAt != nil {
		nowUnix = *settledAt
	}
	return s.store.MarkIntentSettled(ctx, intent.IntentID, nowUnix)
}

func (s *FAPService) HandleLNBitsWebhook(ctx context.Context, event LNBitsWebhookEvent, nowUnix int64) error {
	paymentHash := strings.TrimSpace(event.PaymentHash)
	checkingID := strings.TrimSpace(event.CheckingID)
	eventID := strings.TrimSpace(event.EventID)
	if paymentHash == "" && checkingID == "" {
		return fmt.Errorf("%w: payment_hash or checking_id is required", ErrValidation)
	}

	if nowUnix <= 0 {
		nowUnix = s.nowUnix()
	}
	if event.EventTime != nil && *event.EventTime > 0 {
		nowUnix = *event.EventTime
	}
	if event.PaidAt != nil && *event.PaidAt > 0 {
		nowUnix = *event.PaidAt
	}
	s.maybePruneWebhookEvents(ctx, nowUnix)

	eventKey := buildWebhookEventKey(eventID, checkingID, paymentHash, event.AmountMSat, nowUnix)
	if eventKey != "" {
		created, err := s.store.RecordWebhookEvent(ctx, store.WebhookEvent{
			EventKey:   eventKey,
			ReceivedAt: nowUnix,
		})
		if err != nil {
			return err
		}
		if !created {
			return nil
		}
	}

	if eventID != "" {
		if _, err := s.store.GetBoostByLNBitsWebhookEventID(ctx, eventID); err == nil {
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}

	boost, err := s.findBoostByLNBitsRefs(ctx, checkingID, paymentHash)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err == nil && event.Paid {
		updated, markErr := s.MarkBoostPaid(ctx, boost.BoostID, nowUnix)
		if markErr != nil {
			if errors.Is(markErr, ErrValidation) {
				return nil
			}
			return markErr
		}
		if eventID != "" && updated.LNBitsWebhookEventID != eventID {
			if setErr := s.store.UpdateBoostLNBitsWebhookEventID(ctx, boost.BoostID, eventID, nowUnix); setErr != nil {
				return setErr
			}
		}
	}

	challenge, challengeErr := s.findChallengeByLNBitsRefs(ctx, checkingID, paymentHash)
	if challengeErr != nil && !errors.Is(challengeErr, store.ErrNotFound) {
		return challengeErr
	}
	if challengeErr == nil && event.Paid {
		if markErr := s.markAccessChallengePaid(ctx, challenge.ChallengeID, nowUnix); markErr != nil && !errors.Is(markErr, ErrValidation) {
			return markErr
		}
	}

	if paymentHash != "" {
		_ = s.HandleWebhook(ctx, paymentHash, nowUnix)
		_ = s.handleChallengeByPaymentHash(ctx, paymentHash, nowUnix)
	}
	return nil
}

func (s *FAPService) maybePruneWebhookEvents(ctx context.Context, nowUnix int64) {
	if s.cfg.WebhookEventRetentionSeconds <= 0 || s.cfg.WebhookEventPruneIntervalSeconds <= 0 {
		return
	}
	cutoff := nowUnix - s.cfg.WebhookEventRetentionSeconds
	if cutoff <= 0 {
		return
	}

	shouldRun := false
	s.webhookPruneMu.Lock()
	if nowUnix >= s.nextWebhookPruneAt {
		s.nextWebhookPruneAt = nowUnix + s.cfg.WebhookEventPruneIntervalSeconds
		shouldRun = true
	}
	s.webhookPruneMu.Unlock()
	if !shouldRun {
		return
	}

	_, _ = s.store.PruneWebhookEvents(ctx, cutoff)
}

func buildWebhookEventKey(eventID string, checkingID string, paymentHash string, amountMSat int64, timestampUnix int64) string {
	if trimmed := strings.TrimSpace(eventID); trimmed != "" {
		return "id:" + trimmed
	}
	checking := strings.TrimSpace(checkingID)
	payment := strings.TrimSpace(paymentHash)
	if checking == "" && payment == "" {
		return ""
	}
	if timestampUnix <= 0 {
		timestampUnix = time.Now().Unix()
	}
	const dedupeWindowSeconds = int64(300)
	window := timestampUnix / dedupeWindowSeconds
	return "tuple:c:" + checking +
		"|p:" + payment +
		"|a:" + strconv.FormatInt(amountMSat, 10) +
		"|w:" + strconv.FormatInt(window, 10)
}

func (s *FAPService) findBoostByLNBitsRefs(ctx context.Context, checkingID string, paymentHash string) (store.Boost, error) {
	if checkingID != "" {
		boost, err := s.store.GetBoostByLNBitsCheckingID(ctx, checkingID)
		if err == nil {
			return boost, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return store.Boost{}, err
		}
	}
	if paymentHash != "" {
		return s.store.GetBoostByLNBitsPaymentHash(ctx, paymentHash)
	}
	return store.Boost{}, store.ErrNotFound
}

func (s *FAPService) findChallengeByLNBitsRefs(ctx context.Context, checkingID string, paymentHash string) (store.AccessChallenge, error) {
	if checkingID != "" {
		challenge, err := s.store.GetChallengeByLNBitsCheckingID(ctx, checkingID)
		if err == nil {
			return challenge, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return store.AccessChallenge{}, err
		}
	}
	if paymentHash != "" {
		return s.store.GetChallengeByLNBitsPaymentHash(ctx, paymentHash)
	}
	return store.AccessChallenge{}, store.ErrNotFound
}

func (s *FAPService) handleChallengeByPaymentHash(ctx context.Context, paymentHash string, nowUnix int64) error {
	challenge, err := s.store.GetChallengeByLNBitsPaymentHash(ctx, paymentHash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	adapter, err := s.adapters.ForPayee(ctx, challenge.PayeeID)
	if err != nil {
		return err
	}
	settled, settledAt, err := adapter.IsSettled(ctx, paymentHash)
	if err != nil {
		return err
	}
	if !settled {
		return nil
	}
	if nowUnix <= 0 {
		nowUnix = s.nowUnix()
	}
	if settledAt != nil && *settledAt > 0 {
		nowUnix = *settledAt
	}
	return s.markAccessChallengePaid(ctx, challenge.ChallengeID, nowUnix)
}

func (s *FAPService) trySettlePendingChallenge(ctx context.Context, challenge store.AccessChallenge, nowUnix int64) (store.AccessChallenge, bool) {
	if challenge.Status != "pending" {
		return challenge, false
	}
	checkingID := strings.TrimSpace(challenge.LNBitsCheckingID)
	paymentHash := strings.TrimSpace(challenge.LNBitsPaymentHash)
	if checkingID == "" && paymentHash == "" {
		return challenge, false
	}
	adapter, err := s.adapters.ForPayee(ctx, challenge.PayeeID)
	if err != nil {
		return challenge, false
	}
	paymentRef := checkingID
	if paymentRef == "" {
		paymentRef = paymentHash
	}
	settled, settledAt, err := adapter.IsSettled(ctx, paymentRef)
	if err != nil || !settled {
		// Fallback for adapters that still expect payment_hash.
		if paymentRef != paymentHash && paymentHash != "" {
			settled, settledAt, err = adapter.IsSettled(ctx, paymentHash)
		}
		if err != nil || !settled {
			return challenge, false
		}
	}

	paidAt := nowUnix
	if paidAt <= 0 {
		paidAt = s.nowUnix()
	}
	if settledAt != nil && *settledAt > 0 {
		paidAt = *settledAt
	}
	if err := s.markAccessChallengePaid(ctx, challenge.ChallengeID, paidAt); err != nil {
		return challenge, false
	}
	updated, err := s.store.GetChallengeByID(ctx, challenge.ChallengeID)
	if err != nil {
		return challenge, false
	}
	return updated, true
}

func (s *FAPService) markAccessChallengePaid(ctx context.Context, challengeID string, paidAt int64) error {
	challenge, err := s.store.GetChallengeByID(ctx, challengeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if challenge.Status == "paid" {
		_ = s.ensureLedgerEntryForChallenge(ctx, challenge)
		_ = s.updateLedgerStatus(ctx, "access", challenge.ChallengeID, "paid", challenge.PaidAt, firstNonEmpty(challenge.LNBitsCheckingID, challenge.LNBitsPaymentHash), s.nowUnix())
		return s.createAccessGrantFromChallenge(ctx, challenge, s.nowUnix())
	}
	if challenge.Status != "pending" {
		return fmt.Errorf("%w: challenge status cannot transition to paid", ErrValidation)
	}
	if paidAt <= 0 {
		paidAt = s.nowUnix()
	}
	if err := s.store.UpdateChallengeStatus(ctx, challengeID, "paid", &paidAt, paidAt); err != nil {
		return err
	}
	_ = s.ensureLedgerEntryForChallenge(ctx, challenge)
	if err := s.updateLedgerStatus(ctx, "access", challenge.ChallengeID, "paid", &paidAt, firstNonEmpty(challenge.LNBitsCheckingID, challenge.LNBitsPaymentHash), paidAt); err != nil {
		return err
	}
	return s.createAccessGrantFromChallenge(ctx, challenge, paidAt)
}

func (s *FAPService) createAccessGrantFromChallenge(ctx context.Context, challenge store.AccessChallenge, nowUnix int64) error {
	deviceID := strings.TrimSpace(challenge.DeviceID)
	if deviceID == "" {
		return nil
	}
	if _, err := s.store.GetAccessGrantByChallengeID(ctx, challenge.ChallengeID); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	grantID, err := randomID()
	if err != nil {
		return err
	}
	grant := store.AccessGrant{
		GrantID:          grantID,
		DeviceID:         deviceID,
		AssetID:          challenge.AssetID,
		Scope:            grantScopeHLSKey,
		MinutesPurchased: s.cfg.AccessMinutesPerPay,
		ValidFrom:        nil,
		ValidUntil:       nil,
		Status:           "active",
		ChallengeID:      challenge.ChallengeID,
		AmountMSat:       challenge.AmountMSat,
		CreatedAt:        nowUnix,
		UpdatedAt:        nowUnix,
	}
	if err := s.store.CreateAccessGrant(ctx, grant); err != nil {
		if isUniqueConstraint(err) {
			if _, getErr := s.store.GetAccessGrantByChallengeID(ctx, challenge.ChallengeID); getErr == nil {
				return nil
			}
		}
		return err
	}
	return nil
}

func (s *FAPService) ensureLedgerEntryForChallenge(ctx context.Context, challenge store.AccessChallenge) error {
	deviceID := strings.TrimSpace(challenge.DeviceID)
	if deviceID == "" {
		return nil
	}
	currency := "msat"
	createdAt := challenge.CreatedAt
	if createdAt <= 0 {
		createdAt = s.nowUnix()
	}
	updatedAt := challenge.UpdatedAt
	if updatedAt <= 0 {
		updatedAt = createdAt
	}
	entryID := "ledger_access_" + challenge.ChallengeID
	return s.store.InsertLedgerEntryIfNotExists(ctx, store.LedgerEntry{
		EntryID:     entryID,
		DeviceID:    deviceID,
		Kind:        "access",
		Status:      challenge.Status,
		AssetID:     challenge.AssetID,
		PayeeID:     challenge.PayeeID,
		AmountMSat:  challenge.AmountMSat,
		Currency:    currency,
		RelatedID:   challenge.ChallengeID,
		ReferenceID: firstNonEmpty(challenge.LNBitsCheckingID, challenge.LNBitsPaymentHash),
		Memo:        challenge.Memo,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		PaidAt:      challenge.PaidAt,
	})
}

func (s *FAPService) ensureLedgerEntryForBoost(ctx context.Context, boost store.Boost) error {
	deviceID := strings.TrimSpace(boost.DeviceID)
	if deviceID == "" {
		return nil
	}
	entryID := "ledger_boost_" + boost.BoostID
	return s.store.InsertLedgerEntryIfNotExists(ctx, store.LedgerEntry{
		EntryID:     entryID,
		DeviceID:    deviceID,
		Kind:        "boost",
		Status:      boost.Status,
		AssetID:     boost.AssetID,
		PayeeID:     boost.PayeeID,
		AmountMSat:  boost.AmountMSat,
		Currency:    "msat",
		RelatedID:   boost.BoostID,
		ReferenceID: firstNonEmpty(boost.LNBitsCheckingID, boost.LNBitsPaymentHash),
		Memo:        "",
		CreatedAt:   boost.CreatedAt,
		UpdatedAt:   boost.UpdatedAt,
		PaidAt:      boost.PaidAt,
	})
}

func (s *FAPService) updateLedgerStatus(ctx context.Context, kind string, relatedID string, status string, paidAt *int64, referenceID string, updatedAt int64) error {
	if updatedAt <= 0 {
		updatedAt = s.nowUnix()
	}
	err := s.store.UpdateLedgerStatus(ctx, kind, relatedID, status, paidAt, referenceID, updatedAt)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

func (s *FAPService) MintToken(ctx context.Context, intentID string, subject string, nowUnix int64) (AccessToken, error) {
	intendedID := strings.TrimSpace(intentID)
	if intendedID == "" {
		return AccessToken{}, fmt.Errorf("%w: challenge_id is required", ErrValidation)
	}
	subject = strings.TrimSpace(subject)
	if nowUnix == 0 {
		nowUnix = s.nowUnix()
	}

	payeeID := ""
	assetID := ""
	resourceID := ""
	paymentHash := ""
	isChallengeMode := false

	challenge, challengeErr := s.store.GetChallengeByID(ctx, intendedID)
	if challengeErr == nil {
		isChallengeMode = true
		if challenge.Status == "pending" {
			if updated, ok := s.trySettlePendingChallenge(ctx, challenge, nowUnix); ok {
				challenge = updated
			}
		}
		if challenge.Status == "pending" && nowUnix > challenge.ExpiresAt {
			_ = s.store.UpdateChallengeStatus(ctx, challenge.ChallengeID, "expired", nil, nowUnix)
			_ = s.updateLedgerStatus(ctx, "access", challenge.ChallengeID, "expired", nil, firstNonEmpty(challenge.LNBitsCheckingID, challenge.LNBitsPaymentHash), nowUnix)
			return AccessToken{}, ErrNotSettled
		}
		if challenge.Status != "paid" {
			return AccessToken{}, ErrNotSettled
		}
		if challenge.DeviceID != "" {
			if subject == "" {
				return AccessToken{}, ErrDeviceRequired
			}
			if subject != challenge.DeviceID {
				return AccessToken{}, ErrDeviceMismatch
			}
		} else if subject == "" {
			subject = "anonymous"
		}
		payeeID = challenge.PayeeID
		assetID = challenge.AssetID
		resourceID = "hls:key:" + challenge.AssetID
		paymentHash = challenge.LNBitsPaymentHash
	} else {
		if !errors.Is(challengeErr, store.ErrNotFound) {
			return AccessToken{}, challengeErr
		}
		if subject == "" {
			subject = "anonymous"
		}
		intent, err := s.store.GetIntentByID(ctx, intendedID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return AccessToken{}, ErrNotFound
			}
			return AccessToken{}, err
		}
		if intent.Status != "settled" {
			return AccessToken{}, ErrNotSettled
		}
		if nowUnix > intent.InvoiceExpiresAt {
			return AccessToken{}, ErrIntentExpired
		}
		asset, err := s.store.GetAssetByID(ctx, intent.AssetID)
		if err != nil {
			return AccessToken{}, err
		}
		payeeID = intent.PayeeID
		assetID = intent.AssetID
		resourceID = asset.ResourceID
		paymentHash = intent.PaymentHash
	}

	existing, err := s.store.GetAccessTokenByIntentID(ctx, intendedID)
	existingFound := false
	if err == nil {
		existingFound = true
		if existing.Subject != subject {
			if isChallengeMode {
				return AccessToken{}, ErrDeviceMismatch
			}
			return AccessToken{}, ErrSubjectMismatch
		}
		if existing.ExpiresAt > nowUnix {
			return AccessToken{Token: existing.Token, ExpiresAt: existing.ExpiresAt, ResourceID: existing.ResourceID}, nil
		}
	}
	if !errors.Is(err, store.ErrNotFound) {
		return AccessToken{}, err
	}

	expiresAt := nowUnix + s.cfg.TokenTTLSeconds
	nonce, err := randomID()
	if err != nil {
		return AccessToken{}, err
	}
	payload := itoken.AccessTokenPayload{
		Version:         "1",
		IssuerPubKeyHex: s.issuerPub,
		Subject:         subject,
		ResourceID:      resourceID,
		Entitlements:    []itoken.Entitlement{"hls:key"},
		IssuedAt:        nowUnix,
		ExpiresAt:       expiresAt,
		PaymentHash:     paymentHash,
		Nonce:           nonce,
	}
	jwt, err := s.signToken(payload)
	if err != nil {
		return AccessToken{}, err
	}
	tokenID, err := randomID()
	if err != nil {
		return AccessToken{}, err
	}
	token := store.AccessToken{
		TokenID:    tokenID,
		IntentID:   intendedID,
		PayeeID:    payeeID,
		AssetID:    assetID,
		ResourceID: resourceID,
		Subject:    subject,
		Token:      jwt,
		ExpiresAt:  expiresAt,
		CreatedAt:  nowUnix,
	}
	if existingFound {
		if err := s.store.UpdateAccessTokenByIntentID(ctx, token); err == nil {
			return AccessToken{Token: token.Token, ExpiresAt: token.ExpiresAt, ResourceID: token.ResourceID}, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			if existing2, e := s.store.GetAccessTokenByIntentID(ctx, intendedID); e == nil {
				if existing2.Subject != subject {
					if isChallengeMode {
						return AccessToken{}, ErrDeviceMismatch
					}
					return AccessToken{}, ErrSubjectMismatch
				}
				if existing2.ExpiresAt > nowUnix {
					return AccessToken{Token: existing2.Token, ExpiresAt: existing2.ExpiresAt, ResourceID: existing2.ResourceID}, nil
				}
			}
			return AccessToken{}, err
		}
	}
	if err := s.store.CreateAccessToken(ctx, token); err != nil {
		if existing2, e := s.store.GetAccessTokenByIntentID(ctx, intendedID); e == nil {
			if existing2.Subject != subject {
				if isChallengeMode {
					return AccessToken{}, ErrDeviceMismatch
				}
				return AccessToken{}, ErrSubjectMismatch
			}
			return AccessToken{Token: existing2.Token, ExpiresAt: existing2.ExpiresAt, ResourceID: existing2.ResourceID}, nil
		}
		return AccessToken{}, err
	}
	return AccessToken{Token: token.Token, ExpiresAt: token.ExpiresAt, ResourceID: token.ResourceID}, nil
}

func randomID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func buildFakeBolt11(amountMSat int64, boostID string) string {
	sats := amountMSat / 1000
	if sats <= 0 {
		sats = 1
	}
	prefix := boostID
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	return fmt.Sprintf("lnbc%dn1%sdevboost", sats, prefix)
}

func toBoost(value store.Boost) Boost {
	return Boost{
		BoostID:              value.BoostID,
		DeviceID:             value.DeviceID,
		AssetID:              value.AssetID,
		PayeeID:              value.PayeeID,
		AmountMSat:           value.AmountMSat,
		Bolt11:               value.Bolt11,
		LNBitsPaymentHash:    value.LNBitsPaymentHash,
		LNBitsCheckingID:     value.LNBitsCheckingID,
		LNBitsWebhookEventID: value.LNBitsWebhookEventID,
		Status:               value.Status,
		ExpiresAt:            value.ExpiresAt,
		PaidAt:               value.PaidAt,
		CreatedAt:            value.CreatedAt,
		UpdatedAt:            value.UpdatedAt,
		IdempotencyKey:       value.IdempotencyKey,
	}
}

func toLedgerEntry(value store.LedgerEntry) LedgerEntry {
	return LedgerEntry{
		EntryID:     value.EntryID,
		DeviceID:    value.DeviceID,
		Kind:        value.Kind,
		Status:      value.Status,
		AssetID:     value.AssetID,
		PayeeID:     value.PayeeID,
		AmountMSat:  value.AmountMSat,
		Currency:    value.Currency,
		RelatedID:   value.RelatedID,
		ReferenceID: value.ReferenceID,
		Memo:        value.Memo,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
		PaidAt:      value.PaidAt,
	}
}

func toChallenge(value store.AccessChallenge) Challenge {
	return Challenge{
		ChallengeID: value.ChallengeID,
		IntentID:    value.ChallengeID,
		DeviceID:    value.DeviceID,
		AssetID:     value.AssetID,
		PayeeID:     value.PayeeID,
		Bolt11:      value.Bolt11,
		PaymentHash: value.LNBitsPaymentHash,
		CheckingID:  value.LNBitsCheckingID,
		ExpiresAt:   value.ExpiresAt,
		AmountMSat:  value.AmountMSat,
		ResourceID:  "hls:key:" + value.AssetID,
		Status:      value.Status,
		PaidAt:      value.PaidAt,
	}
}

func toAccessGrant(value store.AccessGrant) AccessGrant {
	return AccessGrant{
		GrantID:          value.GrantID,
		DeviceID:         value.DeviceID,
		AssetID:          value.AssetID,
		Scope:            value.Scope,
		MinutesPurchased: value.MinutesPurchased,
		ValidFrom:        value.ValidFrom,
		ValidUntil:       value.ValidUntil,
		Status:           value.Status,
		ChallengeID:      value.ChallengeID,
		AmountMSat:       value.AmountMSat,
		CreatedAt:        value.CreatedAt,
		UpdatedAt:        value.UpdatedAt,
	}
}

func isUniqueConstraint(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "constraint")
}

func parseBoostCursor(rawCursor string) (*store.BoostCursor, error) {
	cursor := strings.TrimSpace(rawCursor)
	if cursor == "" {
		return nil, nil
	}
	parts := strings.SplitN(cursor, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cursor format")
	}
	createdAt, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor created_at")
	}
	boostID := strings.TrimSpace(parts[1])
	if boostID == "" || len(boostID) > 128 {
		return nil, fmt.Errorf("invalid cursor boost_id")
	}
	return &store.BoostCursor{
		CreatedAt: createdAt,
		BoostID:   boostID,
	}, nil
}

func encodeBoostCursor(createdAt int64, boostID string) string {
	return fmt.Sprintf("%d:%s", createdAt, boostID)
}

func parseLedgerCursor(rawCursor string) (*store.LedgerCursor, error) {
	cursor := strings.TrimSpace(rawCursor)
	if cursor == "" {
		return nil, nil
	}
	parts := strings.SplitN(cursor, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cursor format")
	}
	createdAt, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor created_at")
	}
	entryID := strings.TrimSpace(parts[1])
	if entryID == "" || len(entryID) > 256 {
		return nil, fmt.Errorf("invalid cursor entry_id")
	}
	return &store.LedgerCursor{
		CreatedAt: createdAt,
		EntryID:   entryID,
	}, nil
}

func encodeLedgerCursor(createdAt int64, entryID string) string {
	return fmt.Sprintf("%d:%s", createdAt, entryID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

package store

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("not found")

type Payee struct {
	PayeeID             string
	DisplayName         string
	Rail                string
	Mode                string
	LNBitsBaseURL       string
	LNBitsInvoiceKeyEnc []byte
	LNBitsReadKeyEnc    []byte
	CreatedAt           int64
	UpdatedAt           int64
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

type PaymentIntent struct {
	IntentID         string
	AssetID          string
	PayeeID          string
	AmountMSat       int64
	Bolt11           string
	PaymentHash      string
	Status           string
	InvoiceExpiresAt int64
	SettledAt        *int64
	CreatedAt        int64
}

type AccessChallenge struct {
	ChallengeID       string
	DeviceID          string
	AssetID           string
	PayeeID           string
	AmountMSat        int64
	Memo              string
	Status            string
	Bolt11            string
	LNBitsCheckingID  string
	LNBitsPaymentHash string
	ExpiresAt         int64
	PaidAt            *int64
	CreatedAt         int64
	UpdatedAt         int64
	IdempotencyKey    *string
}

type AccessToken struct {
	TokenID    string
	IntentID   string
	PayeeID    string
	AssetID    string
	ResourceID string
	Subject    string
	Token      string
	ExpiresAt  int64
	CreatedAt  int64
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

type LedgerSummaryParams struct {
	DeviceID string
	FromUnix int64
	ToUnix   int64
	Kind     string
	Limit    int
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

type LedgerSummary struct {
	Totals    LedgerSummaryTotals
	TopAssets []LedgerSummaryAsset
	TopPayees []LedgerSummaryPayee
	Counts    LedgerSummaryCounts
}

type WebhookEvent struct {
	EventKey   string
	ReceivedAt int64
}

type BoostCursor struct {
	CreatedAt int64
	BoostID   string
}

type ListBoostsParams struct {
	AssetID string
	PayeeID string
	Status  string
	Limit   int
	Cursor  *BoostCursor
}

type LedgerCursor struct {
	CreatedAt int64
	EntryID   string
}

type ListLedgerEntriesParams struct {
	DeviceID string
	Kind     string
	Status   string
	AssetID  string
	Limit    int
	Cursor   *LedgerCursor
}

type PayeeRepository interface {
	CreatePayee(ctx context.Context, p Payee) error
	GetByID(ctx context.Context, payeeID string) (Payee, error)
}

type AssetRepository interface {
	CreateAsset(ctx context.Context, a Asset) error
	GetAssetByID(ctx context.Context, assetID string) (Asset, error)
}

type IntentRepository interface {
	CreateIntent(ctx context.Context, i PaymentIntent) error
	GetIntentByID(ctx context.Context, intentID string) (PaymentIntent, error)
	GetIntentByPaymentHash(ctx context.Context, paymentHash string) (PaymentIntent, error)
	MarkIntentSettled(ctx context.Context, intentID string, settledAt int64) error
}

type AccessTokenRepository interface {
	CreateAccessToken(ctx context.Context, t AccessToken) error
	GetAccessTokenByIntentID(ctx context.Context, intentID string) (AccessToken, error)
	UpdateAccessTokenByIntentID(ctx context.Context, t AccessToken) error
}

type ChallengeRepository interface {
	CreateChallenge(ctx context.Context, c AccessChallenge) error
	GetChallengeByID(ctx context.Context, challengeID string) (AccessChallenge, error)
	GetChallengeByIdempotencyKey(ctx context.Context, idempotencyKey string) (AccessChallenge, error)
	GetChallengeByLNBitsPaymentHash(ctx context.Context, paymentHash string) (AccessChallenge, error)
	GetChallengeByLNBitsCheckingID(ctx context.Context, checkingID string) (AccessChallenge, error)
	UpdateChallengeStatus(ctx context.Context, challengeID string, status string, paidAt *int64, updatedAt int64) error
}

type DeviceRepository interface {
	CreateDevice(ctx context.Context, d Device) error
	GetDeviceByID(ctx context.Context, deviceID string) (Device, error)
	TouchDevice(ctx context.Context, deviceID string, lastSeenAt int64) error
}

type AccessGrantRepository interface {
	CreateAccessGrant(ctx context.Context, g AccessGrant) error
	GetAccessGrantByChallengeID(ctx context.Context, challengeID string) (AccessGrant, error)
	GetLatestAccessGrantByDeviceAsset(ctx context.Context, deviceID string, assetID string) (AccessGrant, error)
	ActivateAccessGrant(ctx context.Context, grantID string, validFrom int64, validUntil int64, updatedAt int64) error
	UpdateAccessGrantStatus(ctx context.Context, grantID string, status string, updatedAt int64) error
	ListAccessGrantsByDevice(ctx context.Context, deviceID string, assetID string) ([]AccessGrant, error)
}

type BoostRepository interface {
	CreateBoost(ctx context.Context, b Boost) error
	GetBoostByID(ctx context.Context, boostID string) (Boost, error)
	GetBoostByIdempotencyKey(ctx context.Context, idempotencyKey string) (Boost, error)
	GetBoostByLNBitsPaymentHash(ctx context.Context, paymentHash string) (Boost, error)
	GetBoostByLNBitsCheckingID(ctx context.Context, checkingID string) (Boost, error)
	GetBoostByLNBitsWebhookEventID(ctx context.Context, eventID string) (Boost, error)
	UpdateBoostStatus(ctx context.Context, boostID string, status string, paidAt *int64, updatedAt int64) error
	UpdateBoostLNBitsWebhookEventID(ctx context.Context, boostID string, eventID string, updatedAt int64) error
	ListBoosts(ctx context.Context, params ListBoostsParams) ([]Boost, error)
}

type LedgerRepository interface {
	InsertLedgerEntryIfNotExists(ctx context.Context, entry LedgerEntry) error
	UpdateLedgerStatus(ctx context.Context, kind string, relatedID string, status string, paidAt *int64, referenceID string, updatedAt int64) error
	ListLedgerEntriesForDevice(ctx context.Context, params ListLedgerEntriesParams) ([]LedgerEntry, error)
	GetLedgerSummaryForDevice(ctx context.Context, params LedgerSummaryParams) (LedgerSummary, error)
}

type WebhookEventRepository interface {
	RecordWebhookEvent(ctx context.Context, event WebhookEvent) (bool, error)
	PruneWebhookEvents(ctx context.Context, olderThanUnix int64) (int64, error)
}

type Repository interface {
	PayeeRepository
	AssetRepository
	IntentRepository
	ChallengeRepository
	AccessTokenRepository
	DeviceRepository
	AccessGrantRepository
	BoostRepository
	LedgerRepository
	WebhookEventRepository
	Close() error
	DB() *sql.DB
}

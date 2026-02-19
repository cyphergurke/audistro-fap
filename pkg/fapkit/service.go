package fapkit

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	iservice "github.com/yourorg/fap/internal/fap/service"
	itoken "github.com/yourorg/fap/internal/fap/token"
	"github.com/yourorg/fap/internal/pay/lnbits"
	ipayment "github.com/yourorg/fap/internal/pay/payment"
	"github.com/yourorg/fap/internal/security"
	"github.com/yourorg/fap/internal/store/repo"
	"github.com/yourorg/fap/internal/store/sqlite"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrIntentNotSettled = errors.New("intent not settled")
	ErrIntentExpired    = errors.New("intent expired")
)

type PaymentRail string

const PaymentRailLightning PaymentRail = "lightning"

type Challenge struct {
	IntentID    string
	Offer       string
	ProviderRef string
	ExpiresAt   int64
}

type MintedToken struct {
	Token      string
	ExpiresAt  int64
	ResourceID string
}

type Service interface {
	CreateChallenge(ctx context.Context, assetID string, subject string, amountMsat int64) (Challenge, error)
	HandleWebhook(ctx context.Context, paymentHash string, nowUnix int64) error
	MintToken(ctx context.Context, intentID string, nowUnix int64) (MintedToken, error)
}

type Dependencies struct {
	Store    Store
	Payments Payments
	Clock    Clock
}

type Store interface {
	CreatePendingIntent(ctx context.Context, intent IntentRecord) error
	GetIntentByID(ctx context.Context, intentID string) (IntentRecord, error)
	GetIntentByProviderRef(ctx context.Context, rail PaymentRail, providerRef string) (IntentRecord, error)
	MarkIntentSettledByProviderRef(ctx context.Context, rail PaymentRail, providerRef string, settledAt int64) error
	UpsertToken(ctx context.Context, rec TokenRecord) error
	GetTokenByPaymentRefResource(ctx context.Context, paymentRef string, resourceID string) (TokenRecord, error)
}

type Payments interface {
	CreateOffer(ctx context.Context, amountMsat int64, memo string, expirySeconds int64) (offer string, providerRef string, expiresAt int64, err error)
	VerifySettlement(ctx context.Context, providerRef string) (settled bool, settledAt *int64, err error)
}

type Clock interface {
	NowUnix() int64
}

type IntentRecord struct {
	ID          string
	ResourceID  string
	AssetID     string
	Subject     string
	Status      string
	CreatedAt   int64
	ExpiresAt   int64
	SettledAt   *int64
	Rail        PaymentRail
	Amount      int64
	AmountUnit  string
	Asset       string
	ProviderRef string
	Offer       string
	AmountMSat  int64
	Bolt11      string
	PaymentHash string
}

type TokenRecord struct {
	ID         string
	PaymentRef string
	ResourceID string
	Token      string
	IssuedAt   int64
	ExpiresAt  int64
}

type systemClock struct{}

func (systemClock) NowUnix() int64 { return time.Now().Unix() }

func SystemClock() Clock { return systemClock{} }

func NewSQLiteStore(dbPath string) (Store, func() error, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, nil, fmt.Errorf("db path is required")
	}

	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		return nil, nil, err
	}
	if err := sqlite.Migrate(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	s := &sqliteStore{
		intents: sqlite.NewIntentsRepo(db),
		tokens:  sqlite.NewTokensRepo(db),
	}
	return s, db.Close, nil
}

func NewLNBitsPayments(baseURL, invoiceKey, readKey string) (Payments, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("lnbits base url is required")
	}
	if strings.TrimSpace(invoiceKey) == "" {
		return nil, fmt.Errorf("lnbits invoice api key is required")
	}
	if strings.TrimSpace(readKey) == "" {
		return nil, fmt.Errorf("lnbits read only api key is required")
	}
	return &lnbitsPayments{adapter: lnbits.NewAdapter(baseURL, invoiceKey, readKey)}, nil
}

func NewService(cfg Config, deps Dependencies) (Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if deps.Store == nil {
		return nil, fmt.Errorf("store dependency is required")
	}
	if deps.Payments == nil {
		return nil, fmt.Errorf("payments dependency is required")
	}
	if deps.Clock == nil {
		deps.Clock = SystemClock()
	}

	priv, err := parsePrivateKeyHex(cfg.IssuerPrivKeyHex)
	if err != nil {
		return nil, err
	}
	issuerPubKeyHex := hex.EncodeToString(schnorr.SerializePubKey(priv.PubKey()))

	sign := func(payload itoken.AccessTokenPayload) (string, error) {
		return itoken.SignToken(payload, priv)
	}
	verify := func(tokenStr string, expectedIssuerPubKeyHex string, nowUnix int64) (itoken.AccessTokenPayload, error) {
		return itoken.VerifyToken(tokenStr, expectedIssuerPubKeyHex, nowUnix)
	}

	internalSvc := iservice.New(
		&intentRepoAdapter{store: deps.Store},
		&tokenRepoAdapter{store: deps.Store},
		iservice.NewStaticAdapterRegistry(&paymentsAdapter{payments: deps.Payments}),
		sign,
		verify,
		iservice.ServiceConfig{
			IssuerPubKeyHex:      issuerPubKeyHex,
			TokenTTLSeconds:      cfg.TokenTTLSeconds,
			InvoiceExpirySeconds: cfg.InvoiceExpirySeconds,
			MinAmountMSat:        security.DefaultMinAmountMSat,
			MaxAmountMSat:        security.DefaultMaxAmountMSat,
		},
	)

	return &serviceImpl{
		internal:         internalSvc,
		verify:           verify,
		issuerPubKeyHex:  issuerPubKeyHex,
		defaultPriceMsat: cfg.PriceMsatDefault,
		clock:            deps.Clock,
	}, nil
}

type serviceImpl struct {
	internal         *iservice.Service
	verify           func(tokenStr string, expectedIssuerPubKeyHex string, nowUnix int64) (itoken.AccessTokenPayload, error)
	issuerPubKeyHex  string
	defaultPriceMsat int64
	clock            Clock
}

func (s *serviceImpl) CreateChallenge(ctx context.Context, assetID string, subject string, amountMsat int64) (Challenge, error) {
	if amountMsat == 0 {
		amountMsat = s.defaultPriceMsat
	}
	result, err := s.internal.CreateChallenge(ctx, assetID, subject, repo.PaymentRailLightning, amountMsat, repo.AmountUnitMsat, "BTC")
	if err != nil {
		return Challenge{}, mapServiceErr(err)
	}
	return Challenge{IntentID: result.IntentID, Offer: result.Offer, ProviderRef: result.ProviderRef, ExpiresAt: result.ExpiresAt}, nil
}

func (s *serviceImpl) HandleWebhook(ctx context.Context, paymentHash string, nowUnix int64) error {
	if nowUnix == 0 {
		nowUnix = s.clock.NowUnix()
	}
	if err := s.internal.HandleWebhook(ctx, repo.PaymentRailLightning, paymentHash, nowUnix); err != nil {
		return mapServiceErr(err)
	}
	return nil
}

func (s *serviceImpl) MintToken(ctx context.Context, intentID string, nowUnix int64) (MintedToken, error) {
	if nowUnix == 0 {
		nowUnix = s.clock.NowUnix()
	}
	tokenStr, expiresAt, resourceID, _, err := s.internal.MintToken(ctx, intentID, nowUnix)
	if err != nil {
		return MintedToken{}, mapServiceErr(err)
	}
	return MintedToken{Token: tokenStr, ExpiresAt: expiresAt, ResourceID: resourceID}, nil
}

type VerifiedToken struct {
	ResourceID   string
	Entitlements []string
}

func (s *serviceImpl) VerifyTokenForMiddleware(tokenStr string, nowUnix int64) (VerifiedToken, error) {
	if nowUnix == 0 {
		nowUnix = s.clock.NowUnix()
	}
	payload, err := s.verify(tokenStr, s.issuerPubKeyHex, nowUnix)
	if err != nil {
		return VerifiedToken{}, err
	}
	out := VerifiedToken{ResourceID: payload.ResourceID, Entitlements: make([]string, 0, len(payload.Entitlements))}
	for _, ent := range payload.Entitlements {
		out.Entitlements = append(out.Entitlements, string(ent))
	}
	return out, nil
}

func mapServiceErr(err error) error {
	if errors.Is(err, iservice.ErrIntentNotSettled) {
		return ErrIntentNotSettled
	}
	if errors.Is(err, iservice.ErrIntentExpired) {
		return ErrIntentExpired
	}
	if errors.Is(err, iservice.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

type sqliteStore struct {
	intents *sqlite.IntentsRepo
	tokens  *sqlite.TokensRepo
}

func (s *sqliteStore) CreatePendingIntent(ctx context.Context, intent IntentRecord) error {
	return s.intents.CreatePending(ctx, mapIntentToRepo(intent))
}

func (s *sqliteStore) GetIntentByID(ctx context.Context, intentID string) (IntentRecord, error) {
	intent, err := s.intents.GetByID(ctx, intentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IntentRecord{}, ErrNotFound
		}
		return IntentRecord{}, err
	}
	return mapIntentFromRepo(intent), nil
}

func (s *sqliteStore) GetIntentByProviderRef(ctx context.Context, rail PaymentRail, providerRef string) (IntentRecord, error) {
	intent, err := s.intents.GetByProviderRef(ctx, repo.PaymentRail(rail), providerRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IntentRecord{}, ErrNotFound
		}
		return IntentRecord{}, err
	}
	return mapIntentFromRepo(intent), nil
}

func (s *sqliteStore) MarkIntentSettledByProviderRef(ctx context.Context, rail PaymentRail, providerRef string, settledAt int64) error {
	err := s.intents.MarkSettledByProviderRef(ctx, repo.PaymentRail(rail), providerRef, settledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *sqliteStore) UpsertToken(ctx context.Context, rec TokenRecord) error {
	return s.tokens.Upsert(ctx, repo.TokenRecord{
		ID:          rec.ID,
		PaymentHash: rec.PaymentRef,
		ResourceID:  rec.ResourceID,
		Token:       rec.Token,
		IssuedAt:    rec.IssuedAt,
		ExpiresAt:   rec.ExpiresAt,
	})
}

func (s *sqliteStore) GetTokenByPaymentRefResource(ctx context.Context, paymentRef string, resourceID string) (TokenRecord, error) {
	rec, err := s.tokens.GetByPaymentHashResource(ctx, paymentRef, resourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TokenRecord{}, ErrNotFound
		}
		return TokenRecord{}, err
	}
	return TokenRecord{ID: rec.ID, PaymentRef: rec.PaymentHash, ResourceID: rec.ResourceID, Token: rec.Token, IssuedAt: rec.IssuedAt, ExpiresAt: rec.ExpiresAt}, nil
}

func mapIntentFromRepo(i repo.PaymentIntent) IntentRecord {
	return IntentRecord{
		ID:          i.ID,
		ResourceID:  i.ResourceID,
		AssetID:     i.AssetID,
		Subject:     i.Subject,
		Status:      i.Status,
		CreatedAt:   i.CreatedAt,
		ExpiresAt:   i.ExpiresAt,
		SettledAt:   i.SettledAt,
		Rail:        PaymentRail(i.Rail),
		Amount:      i.Amount,
		AmountUnit:  string(i.AmountUnit),
		Asset:       i.Asset,
		ProviderRef: i.ProviderRef,
		Offer:       i.Offer,
		AmountMSat:  i.AmountMSat,
		Bolt11:      i.Bolt11,
		PaymentHash: i.PaymentHash,
	}
}

func mapIntentToRepo(i IntentRecord) repo.PaymentIntent {
	return repo.PaymentIntent{
		ID:          i.ID,
		ResourceID:  i.ResourceID,
		AssetID:     i.AssetID,
		Subject:     i.Subject,
		Status:      i.Status,
		CreatedAt:   i.CreatedAt,
		ExpiresAt:   i.ExpiresAt,
		SettledAt:   i.SettledAt,
		Rail:        repo.PaymentRail(i.Rail),
		Amount:      i.Amount,
		AmountUnit:  repo.AmountUnit(i.AmountUnit),
		Asset:       i.Asset,
		ProviderRef: i.ProviderRef,
		Offer:       i.Offer,
		AmountMSat:  i.AmountMSat,
		Bolt11:      i.Bolt11,
		PaymentHash: i.PaymentHash,
	}
}

type lnbitsPayments struct{ adapter *lnbits.Adapter }

func (p *lnbitsPayments) CreateOffer(ctx context.Context, amountMsat int64, memo string, expirySeconds int64) (string, string, int64, error) {
	offer, err := p.adapter.CreateOffer(ctx, ipayment.CreateOfferRequest{
		Amount:        amountMsat,
		AmountUnit:    repo.AmountUnitMsat,
		Asset:         "BTC",
		Memo:          memo,
		ExpirySeconds: expirySeconds,
	})
	if err != nil {
		return "", "", 0, err
	}
	return offer.Offer, offer.ProviderRef, offer.ExpiresAt, nil
}

func (p *lnbitsPayments) VerifySettlement(ctx context.Context, providerRef string) (bool, *int64, error) {
	status, err := p.adapter.VerifySettlement(ctx, providerRef)
	if err != nil {
		return false, nil, err
	}
	return status.Settled, status.SettledAt, nil
}

type paymentsAdapter struct{ payments Payments }

func (a *paymentsAdapter) Rail() repo.PaymentRail { return repo.PaymentRailLightning }

func (a *paymentsAdapter) CreateOffer(ctx context.Context, req ipayment.CreateOfferRequest) (ipayment.PaymentOffer, error) {
	offer, providerRef, expiresAt, err := a.payments.CreateOffer(ctx, req.Amount, req.Memo, req.ExpirySeconds)
	if err != nil {
		return ipayment.PaymentOffer{}, err
	}
	return ipayment.PaymentOffer{Rail: repo.PaymentRailLightning, ProviderRef: providerRef, Offer: offer, ExpiresAt: expiresAt}, nil
}

func (a *paymentsAdapter) VerifySettlement(ctx context.Context, providerRef string) (ipayment.SettlementStatus, error) {
	settled, settledAt, err := a.payments.VerifySettlement(ctx, providerRef)
	if err != nil {
		return ipayment.SettlementStatus{}, err
	}
	return ipayment.SettlementStatus{Settled: settled, SettledAt: settledAt}, nil
}

type intentRepoAdapter struct{ store Store }

func (a *intentRepoAdapter) CreatePending(ctx context.Context, intent repo.PaymentIntent) error {
	return a.store.CreatePendingIntent(ctx, mapIntentFromRepo(intent))
}

func (a *intentRepoAdapter) GetByID(ctx context.Context, intentID string) (repo.PaymentIntent, error) {
	intent, err := a.store.GetIntentByID(ctx, intentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return repo.PaymentIntent{}, sql.ErrNoRows
		}
		return repo.PaymentIntent{}, err
	}
	return mapIntentToRepo(intent), nil
}

func (a *intentRepoAdapter) GetByProviderRef(ctx context.Context, rail repo.PaymentRail, providerRef string) (repo.PaymentIntent, error) {
	intent, err := a.store.GetIntentByProviderRef(ctx, PaymentRail(rail), providerRef)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return repo.PaymentIntent{}, sql.ErrNoRows
		}
		return repo.PaymentIntent{}, err
	}
	return mapIntentToRepo(intent), nil
}

func (a *intentRepoAdapter) MarkSettledByProviderRef(ctx context.Context, rail repo.PaymentRail, providerRef string, settledAt int64) error {
	err := a.store.MarkIntentSettledByProviderRef(ctx, PaymentRail(rail), providerRef, settledAt)
	if errors.Is(err, ErrNotFound) {
		return sql.ErrNoRows
	}
	return err
}

type tokenRepoAdapter struct{ store Store }

func (a *tokenRepoAdapter) Upsert(ctx context.Context, rec repo.TokenRecord) error {
	return a.store.UpsertToken(ctx, TokenRecord{
		ID:         rec.ID,
		PaymentRef: rec.PaymentHash,
		ResourceID: rec.ResourceID,
		Token:      rec.Token,
		IssuedAt:   rec.IssuedAt,
		ExpiresAt:  rec.ExpiresAt,
	})
}

func (a *tokenRepoAdapter) GetByPaymentHashResource(ctx context.Context, paymentHash string, resourceID string) (repo.TokenRecord, error) {
	rec, err := a.store.GetTokenByPaymentRefResource(ctx, paymentHash, resourceID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return repo.TokenRecord{}, sql.ErrNoRows
		}
		return repo.TokenRecord{}, err
	}
	return repo.TokenRecord{ID: rec.ID, PaymentHash: rec.PaymentRef, ResourceID: rec.ResourceID, Token: rec.Token, IssuedAt: rec.IssuedAt, ExpiresAt: rec.ExpiresAt}, nil
}

func parsePrivateKeyHex(privHex string) (*btcec.PrivateKey, error) {
	raw, err := hex.DecodeString(privHex)
	if err != nil {
		return nil, fmt.Errorf("decode issuer private key hex: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("issuer private key must be 32 bytes, got %d", len(raw))
	}
	priv, _ := btcec.PrivKeyFromBytes(raw)
	return priv, nil
}

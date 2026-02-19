package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/yourorg/fap/internal/fap/token"
	"github.com/yourorg/fap/internal/pay/payment"
	"github.com/yourorg/fap/internal/security"
	"github.com/yourorg/fap/internal/store/repo"
)

const (
	intentStatusPending = "pending"
	intentStatusSettled = "settled"
)

type Service struct {
	intents  IntentRepository
	tokens   TokenRepository
	adapters AdapterRegistry
	sign     TokenSigner
	verify   TokenVerifier
	cfg      ServiceConfig
	nowUnix  func() int64
}

func New(intents IntentRepository, tokens TokenRepository, adapters AdapterRegistry, signer TokenSigner, verifier TokenVerifier, cfg ServiceConfig) *Service {
	if cfg.TokenExpiryLeewaySec == 0 {
		cfg.TokenExpiryLeewaySec = 30
	}
	if cfg.MinAmountMSat <= 0 {
		cfg.MinAmountMSat = security.DefaultMinAmountMSat
	}
	if cfg.MaxAmountMSat <= 0 {
		cfg.MaxAmountMSat = security.DefaultMaxAmountMSat
	}
	return &Service{
		intents:  intents,
		tokens:   tokens,
		adapters: adapters,
		sign:     signer,
		verify:   verifier,
		cfg:      cfg,
		nowUnix:  func() int64 { return time.Now().Unix() },
	}
}

func (s *Service) CreateChallenge(ctx context.Context, assetID string, subject string, rail repo.PaymentRail, amount int64, amountUnit repo.AmountUnit, asset string) (ChallengeResult, error) {
	normalizedRail := rail
	if normalizedRail == "" {
		normalizedRail = repo.PaymentRailLightning
	}
	if amountUnit == "" {
		amountUnit = repo.AmountUnitMsat
	}
	if asset == "" {
		asset = "BTC"
	}

	if err := validateCreateChallengeInput(assetID, subject, amount, s.cfg); err != nil {
		return ChallengeResult{}, err
	}

	adapter, ok := s.adapters.Get(normalizedRail)
	if !ok {
		return ChallengeResult{}, ErrUnsupportedRail
	}

	resourceID := "hls:key:" + assetID
	offer, err := adapter.CreateOffer(ctx, payment.CreateOfferRequest{
		Amount:        amount,
		AmountUnit:    amountUnit,
		Asset:         asset,
		Memo:          resourceID,
		ExpirySeconds: s.cfg.InvoiceExpirySeconds,
	})
	if err != nil {
		return ChallengeResult{}, fmt.Errorf("create offer: %w", err)
	}

	intentID, err := randomID()
	if err != nil {
		return ChallengeResult{}, fmt.Errorf("generate intent id: %w", err)
	}

	intent := repo.PaymentIntent{
		ID:          intentID,
		ResourceID:  resourceID,
		AssetID:     assetID,
		Subject:     subject,
		Status:      intentStatusPending,
		CreatedAt:   s.nowUnix(),
		ExpiresAt:   offer.ExpiresAt,
		Rail:        offer.Rail,
		Amount:      amount,
		AmountUnit:  amountUnit,
		Asset:       asset,
		ProviderRef: offer.ProviderRef,
		Offer:       offer.Offer,
	}

	// Backward compatibility for existing lightning-based storage/index usage.
	if offer.Rail == repo.PaymentRailLightning {
		intent.AmountMSat = amount
		intent.Bolt11 = offer.Offer
		intent.PaymentHash = offer.ProviderRef
	}

	if err := s.intents.CreatePending(ctx, intent); err != nil {
		return ChallengeResult{}, fmt.Errorf("store payment intent: %w", err)
	}

	return ChallengeResult{
		IntentID:    intentID,
		Rail:        offer.Rail,
		Offer:       offer.Offer,
		ProviderRef: offer.ProviderRef,
		ExpiresAt:   offer.ExpiresAt,
	}, nil
}

func (s *Service) HandleWebhook(ctx context.Context, rail repo.PaymentRail, providerRef string, now int64) error {
	normalizedRail := rail
	if normalizedRail == "" {
		normalizedRail = repo.PaymentRailLightning
	}

	adapter, ok := s.adapters.Get(normalizedRail)
	if !ok {
		return ErrUnsupportedRail
	}

	status, err := adapter.VerifySettlement(ctx, providerRef)
	if err != nil {
		return fmt.Errorf("verify settlement: %w", err)
	}
	if !status.Settled {
		return nil
	}

	intent, err := s.intents.GetByProviderRef(ctx, normalizedRail, providerRef)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("load intent by provider_ref: %w", err)
	}
	if intent.Status == intentStatusSettled {
		return nil
	}

	settledAt := now
	if status.SettledAt != nil {
		settledAt = *status.SettledAt
	}
	if err := s.intents.MarkSettledByProviderRef(ctx, normalizedRail, providerRef, settledAt); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("mark intent settled: %w", err)
	}
	return nil
}

func (s *Service) MintToken(ctx context.Context, intentID string, now int64) (string, int64, string, repo.PaymentRail, error) {
	intent, err := s.intents.GetByID(ctx, intentID)
	if err != nil {
		if isNotFound(err) {
			return "", 0, "", "", ErrNotFound
		}
		return "", 0, "", "", fmt.Errorf("get intent: %w", err)
	}
	if intent.Status != intentStatusSettled {
		return "", 0, "", "", ErrIntentNotSettled
	}
	if now > intent.ExpiresAt {
		return "", 0, "", "", ErrIntentExpired
	}

	existing, err := s.tokens.GetByPaymentHashResource(ctx, intent.ProviderRef, intent.ResourceID)
	if err == nil {
		return existing.Token, existing.ExpiresAt, intent.ResourceID, intent.Rail, nil
	}
	if !isNotFound(err) {
		return "", 0, "", "", fmt.Errorf("lookup existing token: %w", err)
	}

	expiresAt := now + s.cfg.TokenTTLSeconds
	maxAllowed := intent.ExpiresAt + s.cfg.TokenExpiryLeewaySec
	if expiresAt > maxAllowed {
		expiresAt = maxAllowed
	}

	nonce, err := randomID()
	if err != nil {
		return "", 0, "", "", fmt.Errorf("generate nonce: %w", err)
	}

	payload := token.AccessTokenPayload{
		Version:         "1",
		IssuerPubKeyHex: s.cfg.IssuerPubKeyHex,
		Subject:         intent.Subject,
		ResourceID:      intent.ResourceID,
		Entitlements:    []token.Entitlement{"hls:key"},
		IssuedAt:        now,
		ExpiresAt:       expiresAt,
		PaymentHash:     intent.ProviderRef, // "ph" is interpreted as generic payment reference across rails.
		Nonce:           nonce,
	}

	tokenStr, err := s.sign(payload)
	if err != nil {
		return "", 0, "", "", fmt.Errorf("sign token: %w", err)
	}
	if _, err := s.verify(tokenStr, s.cfg.IssuerPubKeyHex, now); err != nil {
		return "", 0, "", "", fmt.Errorf("verify minted token: %w", err)
	}

	tokenID, err := randomID()
	if err != nil {
		return "", 0, "", "", fmt.Errorf("generate token id: %w", err)
	}

	rec := repo.TokenRecord{
		ID:          tokenID,
		PaymentHash: intent.ProviderRef,
		ResourceID:  intent.ResourceID,
		Token:       tokenStr,
		IssuedAt:    now,
		ExpiresAt:   expiresAt,
	}
	if err := s.tokens.Upsert(ctx, rec); err != nil {
		return "", 0, "", "", fmt.Errorf("persist token: %w", err)
	}

	return tokenStr, expiresAt, intent.ResourceID, intent.Rail, nil
}

func randomID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows)
}

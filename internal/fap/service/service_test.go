package service

import (
	"context"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/yourorg/fap/internal/fap/token"
	"github.com/yourorg/fap/internal/pay/payment"
	"github.com/yourorg/fap/internal/store/repo"
)

type fakeIntentsRepo struct {
	byID          map[string]repo.PaymentIntent
	byProviderRef map[string]string
}

func newFakeIntentsRepo() *fakeIntentsRepo {
	return &fakeIntentsRepo{
		byID:          make(map[string]repo.PaymentIntent),
		byProviderRef: make(map[string]string),
	}
}

func (r *fakeIntentsRepo) CreatePending(_ context.Context, intent repo.PaymentIntent) error {
	if _, ok := r.byID[intent.ID]; ok {
		return errors.New("duplicate intent id")
	}
	r.byID[intent.ID] = intent
	r.byProviderRef[string(intent.Rail)+"|"+intent.ProviderRef] = intent.ID
	return nil
}

func (r *fakeIntentsRepo) GetByID(_ context.Context, intentID string) (repo.PaymentIntent, error) {
	intent, ok := r.byID[intentID]
	if !ok {
		return repo.PaymentIntent{}, ErrNotFound
	}
	return intent, nil
}

func (r *fakeIntentsRepo) GetByProviderRef(_ context.Context, rail repo.PaymentRail, providerRef string) (repo.PaymentIntent, error) {
	id, ok := r.byProviderRef[string(rail)+"|"+providerRef]
	if !ok {
		return repo.PaymentIntent{}, ErrNotFound
	}
	return r.byID[id], nil
}

func (r *fakeIntentsRepo) MarkSettledByProviderRef(_ context.Context, rail repo.PaymentRail, providerRef string, settledAt int64) error {
	id, ok := r.byProviderRef[string(rail)+"|"+providerRef]
	if !ok {
		return ErrNotFound
	}
	intent := r.byID[id]
	intent.Status = intentStatusSettled
	intent.SettledAt = &settledAt
	r.byID[id] = intent
	return nil
}

type fakeTokensRepo struct {
	byKey map[string]repo.TokenRecord
}

func newFakeTokensRepo() *fakeTokensRepo {
	return &fakeTokensRepo{byKey: make(map[string]repo.TokenRecord)}
}

func (r *fakeTokensRepo) Upsert(_ context.Context, rec repo.TokenRecord) error {
	r.byKey[rec.PaymentHash+"|"+rec.ResourceID] = rec
	return nil
}

func (r *fakeTokensRepo) GetByPaymentHashResource(_ context.Context, paymentHash string, resourceID string) (repo.TokenRecord, error) {
	rec, ok := r.byKey[paymentHash+"|"+resourceID]
	if !ok {
		return repo.TokenRecord{}, ErrNotFound
	}
	return rec, nil
}

type fakePaymentAdapter struct {
	rail        repo.PaymentRail
	offerResp   payment.PaymentOffer
	settleResp  payment.SettlementStatus
	createErr   error
	verifyErr   error
	createCalls int
	verifyCalls int
}

func (a *fakePaymentAdapter) Rail() repo.PaymentRail {
	return a.rail
}

func (a *fakePaymentAdapter) CreateOffer(_ context.Context, _ payment.CreateOfferRequest) (payment.PaymentOffer, error) {
	a.createCalls++
	if a.createErr != nil {
		return payment.PaymentOffer{}, a.createErr
	}
	return a.offerResp, nil
}

func (a *fakePaymentAdapter) VerifySettlement(_ context.Context, _ string) (payment.SettlementStatus, error) {
	a.verifyCalls++
	if a.verifyErr != nil {
		return payment.SettlementStatus{}, a.verifyErr
	}
	return a.settleResp, nil
}

func TestCreateChallengeStoresPendingIntent(t *testing.T) {
	ctx := context.Background()
	intents := newFakeIntentsRepo()
	tokens := newFakeTokensRepo()
	adapter := &fakePaymentAdapter{
		rail: repo.PaymentRailLightning,
		offerResp: payment.PaymentOffer{
			Rail:        repo.PaymentRailLightning,
			ProviderRef: "ph-1",
			Offer:       "lnbc1...",
			ExpiresAt:   1700000900,
		},
	}
	registry := NewStaticAdapterRegistry(adapter)

	_, verify, issuerHex := signerVerifier(t)
	svc := New(intents, tokens, registry, func(_ token.AccessTokenPayload) (string, error) {
		return "unused", nil
	}, verify, ServiceConfig{
		IssuerPubKeyHex:      issuerHex,
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		TokenExpiryLeewaySec: 30,
	})
	svc.nowUnix = func() int64 { return 1700000000 }

	result, err := svc.CreateChallenge(ctx, "asset-1", "user-1", "", 100000, "", "")
	if err != nil {
		t.Fatalf("CreateChallenge error: %v", err)
	}
	if result.Offer != "lnbc1..." || result.ProviderRef != "ph-1" || result.Rail != repo.PaymentRailLightning || result.ExpiresAt != 1700000900 {
		t.Fatalf("unexpected challenge result: %+v", result)
	}

	intent, err := intents.GetByID(ctx, result.IntentID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if intent.Status != intentStatusPending {
		t.Fatalf("expected pending intent, got %s", intent.Status)
	}
	if intent.ResourceID != "hls:key:asset-1" {
		t.Fatalf("unexpected resource id: %s", intent.ResourceID)
	}
	if intent.ProviderRef != "ph-1" || intent.Offer != "lnbc1..." {
		t.Fatalf("unexpected provider_ref/offer: %+v", intent)
	}
}

func TestHandleWebhookMarksSettledIdempotent(t *testing.T) {
	ctx := context.Background()
	intents := newFakeIntentsRepo()
	tokens := newFakeTokensRepo()
	settledAt := int64(1700000010)
	adapter := &fakePaymentAdapter{
		rail:       repo.PaymentRailLightning,
		settleResp: payment.SettlementStatus{Settled: true, SettledAt: &settledAt},
	}
	registry := NewStaticAdapterRegistry(adapter)

	_, verify, issuerHex := signerVerifier(t)
	svc := New(intents, tokens, registry, func(_ token.AccessTokenPayload) (string, error) {
		return "unused", nil
	}, verify, ServiceConfig{
		IssuerPubKeyHex:      issuerHex,
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		TokenExpiryLeewaySec: 30,
	})

	if err := intents.CreatePending(ctx, newSettledOrPendingIntent("intent-1", "ph-1", intentStatusPending)); err != nil {
		t.Fatalf("seed intent: %v", err)
	}

	if err := svc.HandleWebhook(ctx, repo.PaymentRailLightning, "ph-1", 1700000005); err != nil {
		t.Fatalf("HandleWebhook first call: %v", err)
	}
	if err := svc.HandleWebhook(ctx, repo.PaymentRailLightning, "ph-1", 1700000006); err != nil {
		t.Fatalf("HandleWebhook second call: %v", err)
	}

	intent, err := intents.GetByProviderRef(ctx, repo.PaymentRailLightning, "ph-1")
	if err != nil {
		t.Fatalf("GetByProviderRef: %v", err)
	}
	if intent.Status != intentStatusSettled {
		t.Fatalf("expected settled status, got %s", intent.Status)
	}
	if intent.SettledAt == nil || *intent.SettledAt != settledAt {
		t.Fatalf("unexpected settledAt: %v", intent.SettledAt)
	}
}

func TestHandleWebhookUnpaidDoesNotSettle(t *testing.T) {
	ctx := context.Background()
	intents := newFakeIntentsRepo()
	tokens := newFakeTokensRepo()
	adapter := &fakePaymentAdapter{
		rail:       repo.PaymentRailLightning,
		settleResp: payment.SettlementStatus{Settled: false},
	}
	registry := NewStaticAdapterRegistry(adapter)

	_, verify, issuerHex := signerVerifier(t)
	svc := New(intents, tokens, registry, func(_ token.AccessTokenPayload) (string, error) {
		return "unused", nil
	}, verify, ServiceConfig{
		IssuerPubKeyHex:      issuerHex,
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		TokenExpiryLeewaySec: 30,
	})

	if err := intents.CreatePending(ctx, newSettledOrPendingIntent("intent-1", "ph-1", intentStatusPending)); err != nil {
		t.Fatalf("seed intent: %v", err)
	}

	if err := svc.HandleWebhook(ctx, repo.PaymentRailLightning, "ph-1", 1700000010); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}

	intent, err := intents.GetByProviderRef(ctx, repo.PaymentRailLightning, "ph-1")
	if err != nil {
		t.Fatalf("GetByProviderRef: %v", err)
	}
	if intent.Status != intentStatusPending {
		t.Fatalf("expected pending status, got %s", intent.Status)
	}
}

func TestMintTokenSettledAndIdempotentWithExpiryClamp(t *testing.T) {
	ctx := context.Background()
	intents := newFakeIntentsRepo()
	tokens := newFakeTokensRepo()
	adapter := &fakePaymentAdapter{rail: repo.PaymentRailLightning}
	registry := NewStaticAdapterRegistry(adapter)

	sign, verify, issuerHex := signerVerifier(t)
	svc := New(intents, tokens, registry, sign, verify, ServiceConfig{
		IssuerPubKeyHex:      issuerHex,
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		TokenExpiryLeewaySec: 30,
	})

	intent := newSettledOrPendingIntent("intent-1", "ph-1", intentStatusSettled)
	intent.ExpiresAt = 1700000500
	intent.SettledAt = int64ptr(1700000001)
	if err := intents.CreatePending(ctx, intent); err != nil {
		t.Fatalf("seed intent: %v", err)
	}

	tok1, exp1, rid1, rail1, err := svc.MintToken(ctx, "intent-1", 1700000000)
	if err != nil {
		t.Fatalf("MintToken first: %v", err)
	}
	wantExp := int64(1700000530)
	if exp1 != wantExp || rid1 != "hls:key:asset-1" || rail1 != repo.PaymentRailLightning {
		t.Fatalf("unexpected token mint result: token=%s exp=%d rid=%s rail=%s", tok1, exp1, rid1, rail1)
	}

	payload, err := verify(tok1, issuerHex, 1700000000)
	if err != nil {
		t.Fatalf("verify minted token: %v", err)
	}
	if payload.PaymentHash != "ph-1" {
		t.Fatalf("expected payload payment ref ph-1, got %s", payload.PaymentHash)
	}

	tok2, exp2, rid2, rail2, err := svc.MintToken(ctx, "intent-1", 1700000001)
	if err != nil {
		t.Fatalf("MintToken second: %v", err)
	}
	if !reflect.DeepEqual(tok1, tok2) || exp1 != exp2 || rid1 != rid2 || rail1 != rail2 {
		t.Fatalf("expected idempotent mint, got tok2=%s exp2=%d rid2=%s rail2=%s", tok2, exp2, rid2, rail2)
	}
}

func TestMintTokenFailsWhenIntentExpired(t *testing.T) {
	ctx := context.Background()
	intents := newFakeIntentsRepo()
	tokens := newFakeTokensRepo()
	adapter := &fakePaymentAdapter{rail: repo.PaymentRailLightning}
	registry := NewStaticAdapterRegistry(adapter)

	sign, verify, issuerHex := signerVerifier(t)
	svc := New(intents, tokens, registry, sign, verify, ServiceConfig{
		IssuerPubKeyHex:      issuerHex,
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		TokenExpiryLeewaySec: 30,
	})

	intent := newSettledOrPendingIntent("intent-expired", "ph-expired", intentStatusSettled)
	intent.ExpiresAt = 1700000005
	intent.SettledAt = int64ptr(1700000001)
	if err := intents.CreatePending(ctx, intent); err != nil {
		t.Fatalf("seed intent: %v", err)
	}

	if _, _, _, _, err := svc.MintToken(ctx, "intent-expired", 1700000006); err == nil {
		t.Fatal("expected error for expired intent")
	}
}

func newSettledOrPendingIntent(id string, providerRef string, status string) repo.PaymentIntent {
	return repo.PaymentIntent{
		ID:          id,
		ResourceID:  "hls:key:asset-1",
		AssetID:     "asset-1",
		Subject:     "user-1",
		Status:      status,
		CreatedAt:   1700000000,
		ExpiresAt:   1700000900,
		Rail:        repo.PaymentRailLightning,
		Amount:      100000,
		AmountUnit:  repo.AmountUnitMsat,
		Asset:       "BTC",
		ProviderRef: providerRef,
		Offer:       "lnbc...",
		AmountMSat:  100000,
		Bolt11:      "lnbc...",
		PaymentHash: providerRef,
	}
}

func signerVerifier(t *testing.T) (TokenSigner, TokenVerifier, string) {
	t.Helper()
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("new private key: %v", err)
	}
	issuerHex := hex.EncodeToString(schnorr.SerializePubKey(priv.PubKey()))
	sign := func(payload token.AccessTokenPayload) (string, error) {
		return token.SignToken(payload, priv)
	}
	verify := func(tokenStr string, expectedIssuerPubKeyHex string, nowUnix int64) (token.AccessTokenPayload, error) {
		return token.VerifyToken(tokenStr, expectedIssuerPubKeyHex, nowUnix)
	}
	return sign, verify, issuerHex
}

func int64ptr(v int64) *int64 {
	return &v
}

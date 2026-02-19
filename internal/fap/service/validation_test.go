package service

import (
	"context"
	"errors"
	"testing"

	"github.com/yourorg/fap/internal/fap/token"
	"github.com/yourorg/fap/internal/pay/payment"
	"github.com/yourorg/fap/internal/store/repo"
)

func TestCreateChallengeRejectsAmountBelowMin(t *testing.T) {
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
		MinAmountMSat:        1_000,
		MaxAmountMSat:        2_000,
	})

	_, err := svc.CreateChallenge(ctx, "asset-1", "user-1", repo.PaymentRailLightning, 999, repo.AmountUnitMsat, "BTC")
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
	if adapter.createCalls != 0 {
		t.Fatalf("expected no adapter call, got %d", adapter.createCalls)
	}
}

func TestCreateChallengeRejectsAmountAboveMax(t *testing.T) {
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
		MinAmountMSat:        1_000,
		MaxAmountMSat:        2_000,
	})

	_, err := svc.CreateChallenge(ctx, "asset-1", "user-1", repo.PaymentRailLightning, 2_001, repo.AmountUnitMsat, "BTC")
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
	if adapter.createCalls != 0 {
		t.Fatalf("expected no adapter call, got %d", adapter.createCalls)
	}
}

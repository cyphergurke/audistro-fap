package service

import (
	"context"
	"errors"

	"github.com/yourorg/fap/internal/fap/token"
	"github.com/yourorg/fap/internal/pay/payment"
	"github.com/yourorg/fap/internal/store/repo"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrIntentNotSettled = errors.New("intent not settled")
	ErrIntentExpired    = errors.New("intent expired")
	ErrUnsupportedRail  = errors.New("unsupported rail")
)

type IntentRepository interface {
	CreatePending(ctx context.Context, intent repo.PaymentIntent) error
	GetByID(ctx context.Context, intentID string) (repo.PaymentIntent, error)
	GetByProviderRef(ctx context.Context, rail repo.PaymentRail, providerRef string) (repo.PaymentIntent, error)
	MarkSettledByProviderRef(ctx context.Context, rail repo.PaymentRail, providerRef string, settledAt int64) error
}

type TokenRepository interface {
	Upsert(ctx context.Context, rec repo.TokenRecord) error
	GetByPaymentHashResource(ctx context.Context, paymentHash string, resourceID string) (repo.TokenRecord, error)
}

type AdapterRegistry interface {
	Get(rail repo.PaymentRail) (payment.PaymentAdapter, bool)
}

type StaticAdapterRegistry struct {
	adapters map[repo.PaymentRail]payment.PaymentAdapter
}

func NewStaticAdapterRegistry(adapters ...payment.PaymentAdapter) *StaticAdapterRegistry {
	m := make(map[repo.PaymentRail]payment.PaymentAdapter, len(adapters))
	for _, adapter := range adapters {
		m[adapter.Rail()] = adapter
	}
	return &StaticAdapterRegistry{adapters: m}
}

func (r *StaticAdapterRegistry) Get(rail repo.PaymentRail) (payment.PaymentAdapter, bool) {
	adapter, ok := r.adapters[rail]
	return adapter, ok
}

type TokenSigner func(payload token.AccessTokenPayload) (string, error)
type TokenVerifier func(tokenStr string, expectedIssuerPubKeyHex string, nowUnix int64) (token.AccessTokenPayload, error)

type ServiceConfig struct {
	IssuerPubKeyHex      string
	TokenTTLSeconds      int64
	InvoiceExpirySeconds int64
	TokenExpiryLeewaySec int64
	MinAmountMSat        int64
	MaxAmountMSat        int64
}

type ChallengeResult struct {
	IntentID    string
	Rail        repo.PaymentRail
	Offer       string
	ProviderRef string
	ExpiresAt   int64
}

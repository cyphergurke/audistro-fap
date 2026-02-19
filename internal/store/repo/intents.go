package repo

import "context"

type PaymentRail string

const PaymentRailLightning PaymentRail = "lightning"

type AmountUnit string

const AmountUnitMsat AmountUnit = "msat"

type PaymentIntent struct {
	ID         string
	ResourceID string
	AssetID    string
	Subject    string
	Status     string
	CreatedAt  int64
	ExpiresAt  int64
	SettledAt  *int64

	// Generic payment fields (preferred by new code).
	Rail        PaymentRail
	Amount      int64
	AmountUnit  AmountUnit
	Asset       string
	ProviderRef string
	Offer       string

	// Legacy lightning-only compatibility fields.
	AmountMSat  int64
	Bolt11      string
	PaymentHash string
}

type IntentsRepository interface {
	CreatePending(ctx context.Context, intent PaymentIntent) error
	GetByID(ctx context.Context, intentID string) (PaymentIntent, error)
	GetByProviderRef(ctx context.Context, rail PaymentRail, providerRef string) (PaymentIntent, error)
	MarkSettledByProviderRef(ctx context.Context, rail PaymentRail, providerRef string, settledAt int64) error
}

package payment

import (
	"context"

	"github.com/yourorg/fap/internal/store/repo"
)

type PaymentAdapter interface {
	Rail() repo.PaymentRail
	CreateOffer(ctx context.Context, req CreateOfferRequest) (PaymentOffer, error)
	VerifySettlement(ctx context.Context, providerRef string) (SettlementStatus, error)
}

type CreateOfferRequest struct {
	Amount        int64
	AmountUnit    repo.AmountUnit
	Asset         string
	Memo          string
	ExpirySeconds int64
}

type PaymentOffer struct {
	Rail        repo.PaymentRail
	ProviderRef string
	Offer       string
	ExpiresAt   int64
}

type SettlementStatus struct {
	Settled   bool
	SettledAt *int64
	ExpiresAt *int64
}

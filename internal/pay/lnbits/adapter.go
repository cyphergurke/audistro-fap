package lnbits

import (
	"context"
	"fmt"

	"github.com/yourorg/fap/internal/pay/payment"
	"github.com/yourorg/fap/internal/store/repo"
)

type Adapter struct {
	client *Client
}

func NewAdapter(baseURL string, invoiceAPIKey string, readonlyAPIKey string) *Adapter {
	return &Adapter{client: NewClient(baseURL, invoiceAPIKey, readonlyAPIKey)}
}

func NewAdapterWithClient(client *Client) *Adapter {
	return &Adapter{client: client}
}

func (a *Adapter) Rail() repo.PaymentRail {
	return repo.PaymentRailLightning
}

func (a *Adapter) CreateOffer(ctx context.Context, req payment.CreateOfferRequest) (payment.PaymentOffer, error) {
	if req.AmountUnit != repo.AmountUnitMsat {
		return payment.PaymentOffer{}, fmt.Errorf("lnbits only supports msat amount unit")
	}
	if req.Asset != "BTC" {
		return payment.PaymentOffer{}, fmt.Errorf("lnbits only supports BTC asset")
	}

	bolt11, paymentHash, expiresAt, err := a.client.CreateInvoice(ctx, req.Amount, req.Memo, req.ExpirySeconds)
	if err != nil {
		return payment.PaymentOffer{}, err
	}

	return payment.PaymentOffer{
		Rail:        repo.PaymentRailLightning,
		ProviderRef: paymentHash,
		Offer:       bolt11,
		ExpiresAt:   expiresAt,
	}, nil
}

func (a *Adapter) VerifySettlement(ctx context.Context, providerRef string) (payment.SettlementStatus, error) {
	settled, settledAt, err := a.client.LookupPayment(ctx, providerRef)
	if err != nil {
		return payment.SettlementStatus{}, err
	}

	return payment.SettlementStatus{
		Settled:   settled,
		SettledAt: settledAt,
	}, nil
}

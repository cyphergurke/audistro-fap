package merchantlnbits

import (
	"context"
	"fmt"
	"strings"

	"fap/internal/lnbits"
	"fap/internal/pay"
)

type Adapter struct {
	baseURL    string
	invoiceKey string
	readKey    string
	client     lnbits.Client
}

func New(baseURL string, invoiceKey string, readKey string) *Adapter {
	return &Adapter{
		baseURL:    strings.TrimRight(baseURL, "/"),
		invoiceKey: invoiceKey,
		readKey:    readKey,
		client:     lnbits.NewHTTPClient(),
	}
}

var _ pay.PaymentAdapter = (*Adapter)(nil)

func (a *Adapter) CreateInvoice(ctx context.Context, amountMsat int64, memo string, expirySeconds int64) (string, string, string, int64, error) {
	invoice, err := a.client.CreateInvoice(ctx, a.baseURL, a.invoiceKey, amountMsat, memo, expirySeconds)
	if err != nil {
		return "", "", "", 0, err
	}
	return invoice.Bolt11, invoice.PaymentHash, invoice.CheckingID, invoice.ExpiresAt, nil
}

func (a *Adapter) IsSettled(ctx context.Context, paymentHash string) (bool, *int64, error) {
	if strings.TrimSpace(paymentHash) == "" {
		return false, nil, fmt.Errorf("payment hash is required")
	}
	status, err := a.client.VerifyPayment(ctx, a.baseURL, a.readKey, paymentHash)
	if err != nil {
		return false, nil, err
	}
	if !status.Paid {
		return false, nil, nil
	}
	return true, status.PaidAt, nil
}

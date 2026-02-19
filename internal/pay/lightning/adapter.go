package lightning

import "context"

type CreateInvoiceRequest struct {
	AmountMSat    int64
	Memo          string
	ExpirySeconds int64
}

type CreateInvoiceResponse struct {
	Bolt11      string
	PaymentHash string
	ExpiresAt   int64
}

type LookupPaymentResponse struct {
	PaymentHash string
	Settled     bool
	SettledAt   *int64
}

type Adapter interface {
	CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (CreateInvoiceResponse, error)
	LookupPayment(ctx context.Context, paymentHash string) (LookupPaymentResponse, error)
}

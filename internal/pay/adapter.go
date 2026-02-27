package pay

import "context"

type PaymentAdapter interface {
	CreateInvoice(ctx context.Context, amountMsat int64, memo string, expirySeconds int64) (bolt11 string, paymentHash string, checkingID string, expiresAt int64, err error)
	IsSettled(ctx context.Context, paymentHash string) (settled bool, settledAt *int64, err error)
}

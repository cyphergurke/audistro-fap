package repo

import "context"

type TokenRecord struct {
	ID          string
	PaymentHash string
	ResourceID  string
	Token       string
	IssuedAt    int64
	ExpiresAt   int64
}

type TokensRepository interface {
	Upsert(ctx context.Context, rec TokenRecord) error
	GetByPaymentHashResource(ctx context.Context, paymentHash string, resourceID string) (TokenRecord, error)
}

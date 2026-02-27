package fap

import "context"

type Claims struct {
	IssuerPubKeyHex string
	Sub             string
	ResourceID      string
	PaymentHash     string
	IssuedAt        int64
	ExpiresAt       int64
}

type TokenVerifier interface {
	Verify(token string, nowUnix int64) (Claims, error)
}

type Payee struct {
	PayeeID     string
	DisplayName string
	Rail        string
	Mode        string
	BaseURL     string
	CreatedAt   int64
	UpdatedAt   int64
}

type Asset struct {
	AssetID    string
	PayeeID    string
	Title      string
	PriceMSat  int64
	ResourceID string
	CreatedAt  int64
	UpdatedAt  int64
}

type Challenge struct {
	ChallengeID string
	IntentID    string
	AssetID     string
	PayeeID     string
	Bolt11      string
	PaymentHash string
	CheckingID  string
	ExpiresAt   int64
	AmountMSat  int64
	ResourceID  string
	Status      string
	PaidAt      *int64
}

type AccessToken struct {
	Token      string
	ExpiresAt  int64
	ResourceID string
}

type FAPService interface {
	CreatePayee(ctx context.Context, req CreatePayeeRequest) (Payee, error)
	CreateAsset(ctx context.Context, req CreateAssetRequest) (Asset, error)
	CreateChallenge(ctx context.Context, req CreateChallengeRequest) (Challenge, error)
	HandleWebhook(ctx context.Context, paymentHash string, nowUnix int64) error
	MintToken(ctx context.Context, challengeID string, subject string, nowUnix int64) (AccessToken, error)
}

type CreatePayeeRequest struct {
	DisplayName      string
	LNBitsBaseURL    string
	LNBitsInvoiceKey string
	LNBitsReadKey    string
}

type CreateAssetRequest struct {
	AssetID   string
	PayeeID   string
	Title     string
	PriceMSat int64
}

type CreateChallengeRequest struct {
	AssetID        string
	PayeeID        string
	AmountMSat     int64
	Memo           string
	IdempotencyKey string
	Subject        string
}

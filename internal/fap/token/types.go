package token

type Entitlement string

type AccessTokenPayload struct {
	Version         string        `json:"v"`
	IssuerPubKeyHex string        `json:"iss"`
	Subject         string        `json:"sub"`
	ResourceID      string        `json:"rid"`
	Entitlements    []Entitlement `json:"ent"`
	IssuedAt        int64         `json:"iat"`
	ExpiresAt       int64         `json:"exp"`
	PaymentHash     string        `json:"ph"`
	Nonce           string        `json:"nonce"`
}

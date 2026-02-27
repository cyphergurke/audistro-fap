package fap

import (
	"fmt"

	itoken "fap/internal/fap/token"
)

type SchnorrVerifier struct {
	issuerPubKeyHex string
}

func NewSchnorrVerifier(issuerPubKeyHex string) *SchnorrVerifier {
	return &SchnorrVerifier{issuerPubKeyHex: issuerPubKeyHex}
}

func (v *SchnorrVerifier) Verify(token string, nowUnix int64) (Claims, error) {
	payload, err := itoken.VerifyToken(token, v.issuerPubKeyHex, nowUnix)
	if err != nil {
		return Claims{}, err
	}
	return Claims{
		IssuerPubKeyHex: payload.IssuerPubKeyHex,
		Sub:             payload.Subject,
		ResourceID:      payload.ResourceID,
		PaymentHash:     payload.PaymentHash,
		IssuedAt:        payload.IssuedAt,
		ExpiresAt:       payload.ExpiresAt,
	}, nil
}

func SignClaimsUnsupported() error {
	return fmt.Errorf("signing is handled by server service wiring")
}

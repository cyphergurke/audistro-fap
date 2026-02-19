package token

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

func SignToken(payload AccessTokenPayload, priv *btcec.PrivateKey) (string, error) {
	if priv == nil {
		return "", fmt.Errorf("private key is nil")
	}

	payloadBytes, err := Canonicalize(payload)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(payloadBytes)

	sig, err := schnorr.Sign(priv, hash[:])
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	payloadPart := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signaturePart := base64.RawURLEncoding.EncodeToString(sig.Serialize())

	return payloadPart + "." + signaturePart, nil
}

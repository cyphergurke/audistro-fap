package token

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

func VerifyToken(token string, expectedIssuerPubKeyHex string, nowUnix int64) (AccessTokenPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return AccessTokenPayload{}, fmt.Errorf("invalid token format")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return AccessTokenPayload{}, fmt.Errorf("decode payload: %w", err)
	}

	signatureBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessTokenPayload{}, fmt.Errorf("decode signature: %w", err)
	}

	signature, err := schnorr.ParseSignature(signatureBytes)
	if err != nil {
		return AccessTokenPayload{}, fmt.Errorf("parse signature: %w", err)
	}

	expectedPubKeyBytes, err := hex.DecodeString(expectedIssuerPubKeyHex)
	if err != nil {
		return AccessTokenPayload{}, fmt.Errorf("decode expected issuer pubkey hex: %w", err)
	}

	expectedPubKey, err := schnorr.ParsePubKey(expectedPubKeyBytes)
	if err != nil {
		return AccessTokenPayload{}, fmt.Errorf("parse expected issuer pubkey: %w", err)
	}

	hash := sha256.Sum256(payloadBytes)
	if !signature.Verify(hash[:], expectedPubKey) {
		return AccessTokenPayload{}, fmt.Errorf("invalid token signature")
	}

	var payload AccessTokenPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return AccessTokenPayload{}, fmt.Errorf("unmarshal payload: %w", err)
	}

	if payload.IssuerPubKeyHex != expectedIssuerPubKeyHex {
		return AccessTokenPayload{}, fmt.Errorf("issuer mismatch: token=%s expected=%s", payload.IssuerPubKeyHex, expectedIssuerPubKeyHex)
	}

	if nowUnix > payload.ExpiresAt {
		return AccessTokenPayload{}, fmt.Errorf("token expired at %d", payload.ExpiresAt)
	}

	return payload, nil
}

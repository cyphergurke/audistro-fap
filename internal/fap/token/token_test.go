package token

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

func TestSignAndVerifyTokenRoundTrip(t *testing.T) {
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("new private key: %v", err)
	}

	now := int64(1_700_000_000)
	issuerHex := keyHex(priv)
	payload := AccessTokenPayload{
		Version:         "1",
		IssuerPubKeyHex: issuerHex,
		Subject:         "user-123",
		ResourceID:      "track-999",
		Entitlements:    []Entitlement{"stream"},
		IssuedAt:        now,
		ExpiresAt:       now + 600,
		PaymentHash:     "abc123",
		Nonce:           "nonce-1",
	}

	token, err := SignToken(payload, priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	got, err := VerifyToken(token, issuerHex, now)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}

	if !reflect.DeepEqual(got, payload) {
		t.Fatalf("payload mismatch: got %+v want %+v", got, payload)
	}
}

func TestVerifyTokenFailsWhenPayloadIsTampered(t *testing.T) {
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("new private key: %v", err)
	}

	now := int64(1_700_000_000)
	issuerHex := keyHex(priv)
	payload := AccessTokenPayload{
		Version:         "1",
		IssuerPubKeyHex: issuerHex,
		Subject:         "user-123",
		ResourceID:      "track-999",
		Entitlements:    []Entitlement{"stream"},
		IssuedAt:        now,
		ExpiresAt:       now + 600,
		PaymentHash:     "abc123",
		Nonce:           "nonce-1",
	}

	token, err := SignToken(payload, priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	parts := splitToken(t, token)
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var tampered AccessTokenPayload
	if err := json.Unmarshal(payloadBytes, &tampered); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	tampered.Subject = "user-456"

	canonical, err := Canonicalize(tampered)
	if err != nil {
		t.Fatalf("canonicalize tampered payload: %v", err)
	}

	tamperedToken := base64.RawURLEncoding.EncodeToString(canonical) + "." + parts[1]

	if _, err := VerifyToken(tamperedToken, issuerHex, now); err == nil {
		t.Fatal("expected verify to fail for tampered payload")
	}
}

func TestVerifyTokenFailsWhenExpired(t *testing.T) {
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("new private key: %v", err)
	}

	now := int64(1_700_000_000)
	issuerHex := keyHex(priv)
	payload := AccessTokenPayload{
		Version:         "1",
		IssuerPubKeyHex: issuerHex,
		Subject:         "user-123",
		ResourceID:      "track-999",
		Entitlements:    []Entitlement{"stream"},
		IssuedAt:        now - 1000,
		ExpiresAt:       now - 1,
		PaymentHash:     "abc123",
		Nonce:           "nonce-1",
	}

	token, err := SignToken(payload, priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := VerifyToken(token, issuerHex, now); err == nil {
		t.Fatal("expected verify to fail for expired token")
	}
}

func keyHex(priv *btcec.PrivateKey) string {
	return hex.EncodeToString(schnorr.SerializePubKey(priv.PubKey()))
}

func splitToken(t *testing.T, token string) []string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("invalid token in test: %s", token)
	}
	return parts
}

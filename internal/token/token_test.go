package token

import (
	"bytes"
	"testing"
	"time"
)

func TestIssueAndValidateDeterministic(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	nonce := []byte{
		0x00, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b,
		0x0c, 0x0d, 0x0e, 0x0f,
	}
	now := time.Unix(1700000000, 0).UTC()

	issuer, err := NewIssuer(secret, 15*time.Minute, bytes.NewReader(nonce))
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	token, expiresAt, err := issuer.Issue("asset-1", now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if expiresAt != 1700000900 {
		t.Fatalf("unexpected expiresAt: got %d", expiresAt)
	}

	const expected = "YXNzZXQtMXwxNzAwMDAwOTAwfDAwMDEwMjAzMDQwNTA2MDcwODA5MGEwYjBjMGQwZTBm.97db2c54576a446e7f832fa4997ae7f41df81663f483d6be9787838790849d6f"
	if token != expected {
		t.Fatalf("unexpected token: got %s", token)
	}

	if err := issuer.Validate(token, "asset-1", now); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateFailsWrongAssetID(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1700000000, 0).UTC()
	issuer, err := NewIssuer(secret, 15*time.Minute, bytes.NewReader(bytes.Repeat([]byte{0x11}, 16)))
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	token, _, err := issuer.Issue("asset-1", now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := issuer.Validate(token, "asset-2", now); err == nil {
		t.Fatal("expected asset mismatch error")
	}
}

func TestValidateFailsExpired(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1700000000, 0).UTC()
	issuer, err := NewIssuer(secret, 15*time.Minute, bytes.NewReader(bytes.Repeat([]byte{0x22}, 16)))
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	token, _, err := issuer.Issue("asset-1", now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := issuer.Validate(token, "asset-1", now.Add(901*time.Second)); err == nil {
		t.Fatal("expected expired token error")
	}
}

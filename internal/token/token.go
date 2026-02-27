package token

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const nonceBytes = 16

var (
	ErrInvalidToken  = errors.New("invalid token")
	ErrExpiredToken  = errors.New("expired token")
	ErrAssetMismatch = errors.New("asset mismatch")
)

type Issuer struct {
	secret      []byte
	ttl         time.Duration
	nonceSource io.Reader
}

func NewIssuer(secret []byte, ttl time.Duration, nonceSource io.Reader) (*Issuer, error) {
	if len(secret) < 16 {
		return nil, fmt.Errorf("token secret must be at least 16 bytes")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("token ttl must be > 0")
	}
	if nonceSource == nil {
		nonceSource = rand.Reader
	}
	secretCopy := make([]byte, len(secret))
	copy(secretCopy, secret)
	return &Issuer{
		secret:      secretCopy,
		ttl:         ttl,
		nonceSource: nonceSource,
	}, nil
}

func (i *Issuer) Issue(assetID string, now time.Time) (token string, expiresAt int64, err error) {
	nonce := make([]byte, nonceBytes)
	if _, err := io.ReadFull(i.nonceSource, nonce); err != nil {
		return "", 0, fmt.Errorf("read nonce: %w", err)
	}
	expiresAt = now.Add(i.ttl).Unix()
	payload := assetID + "|" + strconv.FormatInt(expiresAt, 10) + "|" + hex.EncodeToString(nonce)
	sigHex := hex.EncodeToString(computeHMAC(i.secret, payload))
	token = base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sigHex
	return token, expiresAt, nil
}

func (i *Issuer) Validate(token string, assetID string, now time.Time) error {
	payloadEncoded, sigHex, ok := strings.Cut(token, ".")
	if !ok || payloadEncoded == "" || sigHex == "" {
		return ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadEncoded)
	if err != nil {
		return ErrInvalidToken
	}
	payload := string(payloadBytes)

	parts := strings.Split(payload, "|")
	if len(parts) != 3 {
		return ErrInvalidToken
	}
	tokenAssetID := parts[0]
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return ErrInvalidToken
	}
	nonce, err := hex.DecodeString(parts[2])
	if err != nil || len(nonce) != nonceBytes {
		return ErrInvalidToken
	}
	if expiresAt < now.Unix() {
		return ErrExpiredToken
	}

	providedSig, err := hex.DecodeString(sigHex)
	if err != nil || len(providedSig) != sha256.Size {
		return ErrInvalidToken
	}
	expectedSig := computeHMAC(i.secret, payload)
	if !hmac.Equal(providedSig, expectedSig) {
		return ErrInvalidToken
	}
	if tokenAssetID != assetID {
		return ErrAssetMismatch
	}
	return nil
}

func computeHMAC(secret []byte, payload string) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

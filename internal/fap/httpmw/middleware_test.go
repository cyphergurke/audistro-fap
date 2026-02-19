package httpmw

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/yourorg/fap/internal/fap/token"
)

func TestMissingAuthorizationReturnsPaymentRequired(t *testing.T) {
	_, issuerHex := mustKeyPair(t)
	mw := New(issuerHex)
	mw.nowUnix = func() int64 { return 1700000000 }

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/hls/asset-1/key", nil)
	rec := httptest.NewRecorder()

	mw.Wrap(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d", rec.Code)
	}

	var out paymentRequiredResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Error != "payment_required" {
		t.Fatalf("unexpected error: %s", out.Error)
	}
	if out.ChallengeEndpoint != "/fap/challenge" {
		t.Fatalf("unexpected challenge endpoint: %s", out.ChallengeEndpoint)
	}
	if out.AssetID != "asset-1" {
		t.Fatalf("unexpected asset_id: %s", out.AssetID)
	}
	if out.RequiredResourceID != "hls:key:asset-1" {
		t.Fatalf("unexpected required_resource_id: %s", out.RequiredResourceID)
	}
}

func TestInvalidOrExpiredTokenReturnsUnauthorized(t *testing.T) {
	_, issuerHex := mustKeyPair(t)
	mw := New(issuerHex)
	mw.nowUnix = func() int64 { return 1700000000 }

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	reqInvalid := httptest.NewRequest(http.MethodGet, "/hls/asset-1/key", nil)
	reqInvalid.Header.Set("Authorization", "Bearer not-a-token")
	recInvalid := httptest.NewRecorder()
	mw.Wrap(next).ServeHTTP(recInvalid, reqInvalid)
	if recInvalid.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", recInvalid.Code)
	}

	priv, _ := mustKeyPair(t)
	expiredToken := mustSignedToken(t, priv, token.AccessTokenPayload{
		Version:         "1",
		IssuerPubKeyHex: issuerHex,
		Subject:         "user-1",
		ResourceID:      "hls:key:asset-1",
		Entitlements:    []token.Entitlement{"hls:key"},
		IssuedAt:        1699999900,
		ExpiresAt:       1699999999,
		PaymentHash:     "ph-1",
		Nonce:           "n-1",
	})
	reqExpired := httptest.NewRequest(http.MethodGet, "/hls/asset-1/key", nil)
	reqExpired.Header.Set("Authorization", "Bearer "+expiredToken)
	recExpired := httptest.NewRecorder()
	mw.Wrap(next).ServeHTTP(recExpired, reqExpired)
	if recExpired.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d", recExpired.Code)
	}
}

func TestValidTokenWithResourceMismatchReturnsForbidden(t *testing.T) {
	priv, issuerHex := mustKeyPair(t)
	mw := New(issuerHex)
	mw.nowUnix = func() int64 { return 1700000000 }

	mismatchToken := mustSignedToken(t, priv, token.AccessTokenPayload{
		Version:         "1",
		IssuerPubKeyHex: issuerHex,
		Subject:         "user-1",
		ResourceID:      "hls:key:asset-other",
		Entitlements:    []token.Entitlement{"hls:key"},
		IssuedAt:        1700000000,
		ExpiresAt:       1700000600,
		PaymentHash:     "ph-1",
		Nonce:           "n-1",
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/hls/asset-1/key", nil)
	req.Header.Set("Authorization", "Bearer "+mismatchToken)
	rec := httptest.NewRecorder()
	mw.Wrap(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestValidTokenWithMissingEntitlementReturnsForbidden(t *testing.T) {
	priv, issuerHex := mustKeyPair(t)
	mw := New(issuerHex)
	mw.nowUnix = func() int64 { return 1700000000 }

	tokenNoEntitlement := mustSignedToken(t, priv, token.AccessTokenPayload{
		Version:         "1",
		IssuerPubKeyHex: issuerHex,
		Subject:         "user-1",
		ResourceID:      "hls:key:asset-1",
		Entitlements:    []token.Entitlement{"hls:key:decrypt"},
		IssuedAt:        1700000000,
		ExpiresAt:       1700000600,
		PaymentHash:     "ph-1",
		Nonce:           "n-1",
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/hls/asset-1/key", nil)
	req.Header.Set("Authorization", "Bearer "+tokenNoEntitlement)
	rec := httptest.NewRecorder()
	mw.Wrap(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestValidTokenWithMatchingResourceAndEntitlementCallsNext(t *testing.T) {
	priv, issuerHex := mustKeyPair(t)
	mw := New(issuerHex)
	mw.nowUnix = func() int64 { return 1700000000 }

	okToken := mustSignedToken(t, priv, token.AccessTokenPayload{
		Version:         "1",
		IssuerPubKeyHex: issuerHex,
		Subject:         "user-1",
		ResourceID:      "hls:key:asset-1",
		Entitlements:    []token.Entitlement{"hls:key"},
		IssuedAt:        1700000000,
		ExpiresAt:       1700000600,
		PaymentHash:     "ph-1",
		Nonce:           "n-1",
	})

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/hls/asset-1/key", nil)
	req.Header.Set("Authorization", "Bearer "+okToken)
	rec := httptest.NewRecorder()
	mw.Wrap(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !called {
		t.Fatal("expected next handler to be called")
	}
}

func mustKeyPair(t *testing.T) (*btcec.PrivateKey, string) {
	t.Helper()

	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("new private key: %v", err)
	}
	issuerHex := hex.EncodeToString(schnorr.SerializePubKey(priv.PubKey()))
	return priv, issuerHex
}

func mustSignedToken(t *testing.T, priv *btcec.PrivateKey, payload token.AccessTokenPayload) string {
	t.Helper()

	tokenStr, err := token.SignToken(payload, priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tokenStr
}

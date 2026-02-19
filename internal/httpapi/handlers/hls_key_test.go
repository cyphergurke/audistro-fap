package handlers

import (
	"context"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/yourorg/fap/internal/fap/token"
	"github.com/yourorg/fap/internal/hls"
	"github.com/yourorg/fap/internal/store/sqlite"
)

func TestHLSKeyEndpointReturnsKeyBytesAndHeaders(t *testing.T) {
	ctx := context.Background()
	db := mustTestDB(t, ctx)
	defer db.Close()

	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	keyHex := "00112233445566778899aabbccddeeff"
	if _, err := db.ExecContext(ctx, `INSERT INTO hls_keys(asset_id, key_hex) VALUES (?, ?)`, "asset123", keyHex); err != nil {
		t.Fatalf("insert hls key: %v", err)
	}

	priv, issuerHex := mustIssuer(t)
	tokenStr := mustToken(t, priv, token.AccessTokenPayload{
		Version:         "1",
		IssuerPubKeyHex: issuerHex,
		Subject:         "user-1",
		ResourceID:      "hls:key:asset123",
		Entitlements:    []token.Entitlement{"hls:key"},
		IssuedAt:        1700000000,
		ExpiresAt:       1700000600,
		PaymentHash:     "ph-1",
		Nonce:           "nonce-1",
	})

	api := NewAPIWithHLS(nil, hls.NewSQLiteRepository(db), "secret", issuerHex, 100000)
	api.hlsMW.SetNowUnix(func() int64 { return 1700000000 })
	req := httptest.NewRequest(http.MethodGet, "/hls/asset123/key", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rec := httptest.NewRecorder()

	api.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("unexpected content-type: %s", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unexpected cache-control: %s", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("unexpected pragma: %s", got)
	}

	wantBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatalf("decode key hex: %v", err)
	}
	if got := rec.Body.Bytes(); string(got) != string(wantBytes) {
		t.Fatalf("unexpected key bytes: got %x want %x", got, wantBytes)
	}
}

func TestHLSKeyEndpointRequiresPaymentToken(t *testing.T) {
	ctx := context.Background()
	db := mustTestDB(t, ctx)
	defer db.Close()

	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO hls_keys(asset_id, key_hex) VALUES (?, ?)`, "asset123", "00112233445566778899aabbccddeeff"); err != nil {
		t.Fatalf("insert hls key: %v", err)
	}

	_, issuerHex := mustIssuer(t)
	api := NewAPIWithHLS(nil, hls.NewSQLiteRepository(db), "secret", issuerHex, 100000)

	req := httptest.NewRequest(http.MethodGet, "/hls/asset123/key", nil)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d", rec.Code)
	}
}

func mustTestDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()

	db, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func mustIssuer(t *testing.T) (*btcec.PrivateKey, string) {
	t.Helper()

	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("new private key: %v", err)
	}
	return priv, hex.EncodeToString(schnorr.SerializePubKey(priv.PubKey()))
}

func mustToken(t *testing.T, priv *btcec.PrivateKey, payload token.AccessTokenPayload) string {
	t.Helper()

	tokenStr, err := token.SignToken(payload, priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tokenStr
}

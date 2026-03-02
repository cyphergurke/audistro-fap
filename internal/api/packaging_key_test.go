package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"audistro-fap/internal/hlskey"
	devtoken "audistro-fap/internal/token"
)

func TestPackagingKeyRequiresAdminToken(t *testing.T) {
	api := NewWithOptions(nil, Options{
		AdminToken:           "admin-secret",
		InternalAllowedCIDRs: "127.0.0.1/32",
		HLSKeyDerive: func(assetID string) [16]byte {
			return hlskey.AES128Key([]byte("0123456789abcdef0123456789abcdef"), assetID)
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/assets/asset-internal/packaging-key", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	api.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestPackagingKeyRequiresAllowedCIDR(t *testing.T) {
	api := NewWithOptions(nil, Options{
		AdminToken:           "admin-secret",
		InternalAllowedCIDRs: "127.0.0.1/32",
		HLSKeyDerive: func(assetID string) [16]byte {
			return hlskey.AES128Key([]byte("0123456789abcdef0123456789abcdef"), assetID)
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/assets/asset-internal/packaging-key", nil)
	req.RemoteAddr = "10.10.10.10:1234"
	req.Header.Set("X-Admin-Token", "admin-secret")
	api.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestPackagingKeyMatchesPublicHLSKeyDerivation(t *testing.T) {
	masterKey := []byte("0123456789abcdef0123456789abcdef")
	issuer, err := devtoken.NewIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, bytes.NewReader(bytes.Repeat([]byte{0x51}, 16)))
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	token, _, err := issuer.Issue("asset-phase2-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	api := NewWithOptions(nil, Options{
		DevMode:              true,
		AccessTokenIssuer:    issuer,
		AdminToken:           "admin-secret",
		InternalAllowedCIDRs: "127.0.0.1/32",
		HLSKeyDerive: func(assetID string) [16]byte {
			return hlskey.AES128Key(masterKey, assetID)
		},
	})

	publicRec := httptest.NewRecorder()
	publicReq := httptest.NewRequest(http.MethodGet, "/hls/asset-phase2-1/key", nil)
	publicReq.Header.Set("Authorization", "Bearer "+token)
	api.Router().ServeHTTP(publicRec, publicReq)
	if publicRec.Code != http.StatusOK {
		t.Fatalf("expected public key 200, got %d", publicRec.Code)
	}

	internalRec := httptest.NewRecorder()
	internalReq := httptest.NewRequest(http.MethodGet, "/internal/assets/asset-phase2-1/packaging-key", nil)
	internalReq.RemoteAddr = "127.0.0.1:1234"
	internalReq.Header.Set("X-Admin-Token", "admin-secret")
	api.Router().ServeHTTP(internalRec, internalReq)
	if internalRec.Code != http.StatusOK {
		t.Fatalf("expected internal key 200, got %d", internalRec.Code)
	}

	publicBody, _ := io.ReadAll(publicRec.Body)
	internalBody, _ := io.ReadAll(internalRec.Body)
	if len(publicBody) != 16 || len(internalBody) != 16 {
		t.Fatalf("expected 16-byte keys, got public=%d internal=%d", len(publicBody), len(internalBody))
	}
	if !bytes.Equal(publicBody, internalBody) {
		t.Fatalf("expected identical keys public=%x internal=%x", publicBody, internalBody)
	}
}

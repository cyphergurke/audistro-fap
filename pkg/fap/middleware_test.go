package fap

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeVerifier struct {
	claims Claims
	err    error
}

func (v fakeVerifier) Verify(_ string, _ int64) (Claims, error) {
	if v.err != nil {
		return Claims{}, v.err
	}
	return v.claims, nil
}

func TestHLSKeyExtractor(t *testing.T) {
	extract := NewHLSKeyExtractor()
	r := httptest.NewRequest(http.MethodGet, "/hls/asset123/key", nil)
	assetID, ok := extract(r)
	if !ok || assetID != "asset123" {
		t.Fatalf("unexpected extract result: ok=%v asset=%q", ok, assetID)
	}
}

func TestResourceGateMiddlewareBranches(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	extract := NewHLSKeyExtractor()

	mwMissing := NewResourceGateMiddleware(fakeVerifier{}, extract, "hls:key")
	rec := httptest.NewRecorder()
	mwMissing(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hls/a1/key", nil))
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d", rec.Code)
	}

	mwInvalid := NewResourceGateMiddleware(fakeVerifier{err: errors.New("bad token")}, extract, "hls:key")
	rec = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/hls/a1/key", nil)
	r.Header.Set("Authorization", "Bearer token")
	mwInvalid(next).ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	mwMismatch := NewResourceGateMiddleware(fakeVerifier{claims: Claims{ResourceID: "hls:key:other"}}, extract, "hls:key")
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/hls/a1/key", nil)
	r.Header.Set("Authorization", "Bearer token")
	mwMismatch(next).ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	mwOK := NewResourceGateMiddleware(fakeVerifier{claims: Claims{ResourceID: "hls:key:a1"}}, extract, "hls:key")
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/hls/a1/key", nil)
	r.Header.Set("Authorization", "Bearer token")
	mwOK(next).ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

package fapkit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/yourorg/fap/pkg/fapkit"
	faphttp "github.com/yourorg/fap/pkg/fapkit/http"
)

type fakePayments struct {
	settled bool
}

func (f *fakePayments) CreateOffer(_ context.Context, amountMsat int64, _ string, expirySeconds int64) (string, string, int64, error) {
	_ = amountMsat
	return "lnbc_test_offer", "ph_test_ref", 1700000000 + expirySeconds, nil
}

func (f *fakePayments) VerifySettlement(_ context.Context, _ string) (bool, *int64, error) {
	if !f.settled {
		return false, nil, nil
	}
	settledAt := int64(1700000100)
	return true, &settledAt, nil
}

func TestPublicPkgIntegration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.db")
	store, closeFn, err := fapkit.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = closeFn() }()

	payments := &fakePayments{settled: false}
	svc, err := fapkit.NewService(fapkit.Config{
		HTTPAddr:             ":8080",
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		DBPath:               dbPath,
		LNBitsBaseURL:        "http://unused",
		LNBitsInvoiceAPIKey:  "unused",
		LNBitsReadOnlyAPIKey: "unused",
		WebhookSecret:        "secret",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		PriceMsatDefault:     100000,
	}, fapkit.Dependencies{
		Store:    store,
		Payments: payments,
		Clock:    fixedClock{now: 1700000000},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	mux := http.NewServeMux()
	faphttp.RegisterRoutes(mux, svc, faphttp.HTTPOptions{WebhookSecret: "secret"})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// challenge
	challengeBody := []byte(`{"asset_id":"asset123","subject":"user-1","amount_msat":100000}`)
	challengeResp, err := http.Post(ts.URL+"/fap/challenge", "application/json", bytes.NewReader(challengeBody))
	if err != nil {
		t.Fatalf("challenge request: %v", err)
	}
	if challengeResp.StatusCode != http.StatusOK {
		t.Fatalf("challenge status: %d", challengeResp.StatusCode)
	}
	var challenge struct {
		IntentID    string `json:"intent_id"`
		Offer       string `json:"offer"`
		ProviderRef string `json:"provider_ref"`
	}
	if err := json.NewDecoder(challengeResp.Body).Decode(&challenge); err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	_ = challengeResp.Body.Close()
	if challenge.IntentID == "" || challenge.Offer == "" || challenge.ProviderRef == "" {
		t.Fatalf("unexpected challenge response: %+v", challenge)
	}

	// token before settlement -> 409
	tokenPre, err := http.Post(ts.URL+"/fap/token", "application/json", bytes.NewReader([]byte(`{"intent_id":"`+challenge.IntentID+`"}`)))
	if err != nil {
		t.Fatalf("token pre request: %v", err)
	}
	if tokenPre.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 before settlement, got %d", tokenPre.StatusCode)
	}
	_ = tokenPre.Body.Close()

	// settle through webhook alias
	payments.settled = true
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/fap/webhook/lnbits", bytes.NewReader([]byte(`{"payment_hash":"`+challenge.ProviderRef+`"}`)))
	if err != nil {
		t.Fatalf("webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-FAP-Webhook-Secret", "secret")
	webhookResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("webhook do: %v", err)
	}
	if webhookResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 webhook, got %d", webhookResp.StatusCode)
	}
	_ = webhookResp.Body.Close()

	// token after settlement -> 200
	tokenPost, err := http.Post(ts.URL+"/fap/token", "application/json", bytes.NewReader([]byte(`{"intent_id":"`+challenge.IntentID+`"}`)))
	if err != nil {
		t.Fatalf("token post request: %v", err)
	}
	if tokenPost.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after settlement, got %d", tokenPost.StatusCode)
	}
	var minted struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(tokenPost.Body).Decode(&minted); err != nil {
		t.Fatalf("decode minted token: %v", err)
	}
	_ = tokenPost.Body.Close()
	if minted.Token == "" {
		t.Fatal("expected non-empty token")
	}
}

type fixedClock struct {
	now int64
}

func (f fixedClock) NowUnix() int64 { return f.now }

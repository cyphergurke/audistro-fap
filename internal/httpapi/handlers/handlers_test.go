package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yourorg/fap/internal/fap/service"
	"github.com/yourorg/fap/internal/store/repo"
)

type fakeService struct {
	createChallengeFn func(ctx context.Context, assetID string, subject string, rail repo.PaymentRail, amount int64, amountUnit repo.AmountUnit, asset string) (service.ChallengeResult, error)
	handleWebhookFn   func(ctx context.Context, rail repo.PaymentRail, providerRef string, now int64) error
	mintTokenFn       func(ctx context.Context, intentID string, now int64) (string, int64, string, repo.PaymentRail, error)
}

func (f *fakeService) CreateChallenge(ctx context.Context, assetID string, subject string, rail repo.PaymentRail, amount int64, amountUnit repo.AmountUnit, asset string) (service.ChallengeResult, error) {
	return f.createChallengeFn(ctx, assetID, subject, rail, amount, amountUnit, asset)
}
func (f *fakeService) HandleWebhook(ctx context.Context, rail repo.PaymentRail, providerRef string, now int64) error {
	return f.handleWebhookFn(ctx, rail, providerRef, now)
}
func (f *fakeService) MintToken(ctx context.Context, intentID string, now int64) (string, int64, string, repo.PaymentRail, error) {
	return f.mintTokenFn(ctx, intentID, now)
}

func TestChallengeDefaultsToLightningAndReturnsOfferProviderRef(t *testing.T) {
	svc := &fakeService{
		createChallengeFn: func(_ context.Context, assetID string, subject string, rail repo.PaymentRail, amount int64, amountUnit repo.AmountUnit, asset string) (service.ChallengeResult, error) {
			if assetID != "asset-1" || subject != "user-1" {
				t.Fatalf("unexpected asset/subject: %s/%s", assetID, subject)
			}
			if rail != repo.PaymentRailLightning || amount != 100000 || amountUnit != repo.AmountUnitMsat || asset != "BTC" {
				t.Fatalf("unexpected defaults rail=%s amount=%d amount_unit=%s asset=%s", rail, amount, amountUnit, asset)
			}
			return service.ChallengeResult{
				IntentID:    "intent-1",
				Rail:        repo.PaymentRailLightning,
				Offer:       "lnbc1...",
				ProviderRef: "ph-1",
				ExpiresAt:   1700000900,
			}, nil
		},
		handleWebhookFn: func(_ context.Context, _ repo.PaymentRail, _ string, _ int64) error { return nil },
		mintTokenFn: func(_ context.Context, _ string, _ int64) (string, int64, string, repo.PaymentRail, error) {
			return "", 0, "", "", nil
		},
	}

	api := NewAPI(svc, "secret", 100000)
	req := httptest.NewRequest(http.MethodPost, "/fap/challenge", bytes.NewBufferString(`{"asset_id":"asset-1","subject":"user-1"}`))
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out challengeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.IntentID != "intent-1" || out.Rail != "lightning" || out.Offer != "lnbc1..." || out.ProviderRef != "ph-1" || out.ExpiresAt != 1700000900 {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestChallengeEndpointRejectsUnknownFields(t *testing.T) {
	svc := &fakeService{
		createChallengeFn: func(_ context.Context, _ string, _ string, _ repo.PaymentRail, _ int64, _ repo.AmountUnit, _ string) (service.ChallengeResult, error) {
			return service.ChallengeResult{}, nil
		},
		handleWebhookFn: func(_ context.Context, _ repo.PaymentRail, _ string, _ int64) error { return nil },
		mintTokenFn: func(_ context.Context, _ string, _ int64) (string, int64, string, repo.PaymentRail, error) {
			return "", 0, "", "", nil
		},
	}

	api := NewAPI(svc, "secret", 100000)
	req := httptest.NewRequest(http.MethodPost, "/fap/challenge", bytes.NewBufferString(`{"asset_id":"asset-1","subject":"user-1","amount":100000,"extra":true}`))
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTokenEndpointReturnsConflictWhenNotSettled(t *testing.T) {
	svc := &fakeService{
		createChallengeFn: func(_ context.Context, _ string, _ string, _ repo.PaymentRail, _ int64, _ repo.AmountUnit, _ string) (service.ChallengeResult, error) {
			return service.ChallengeResult{}, nil
		},
		handleWebhookFn: func(_ context.Context, _ repo.PaymentRail, _ string, _ int64) error { return nil },
		mintTokenFn: func(_ context.Context, _ string, _ int64) (string, int64, string, repo.PaymentRail, error) {
			return "", 0, "", "", service.ErrIntentNotSettled
		},
	}

	api := NewAPI(svc, "secret", 100000)
	api.nowUnix = func() int64 { return 1700000000 }
	req := httptest.NewRequest(http.MethodPost, "/fap/token", bytes.NewBufferString(`{"intent_id":"intent-1"}`))
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}

func TestTokenEndpointContract(t *testing.T) {
	svc := &fakeService{
		createChallengeFn: func(_ context.Context, _ string, _ string, _ repo.PaymentRail, _ int64, _ repo.AmountUnit, _ string) (service.ChallengeResult, error) {
			return service.ChallengeResult{}, nil
		},
		handleWebhookFn: func(_ context.Context, _ repo.PaymentRail, _ string, _ int64) error { return nil },
		mintTokenFn: func(_ context.Context, intentID string, now int64) (string, int64, string, repo.PaymentRail, error) {
			if intentID != "intent-1" || now != 1700000000 {
				t.Fatalf("unexpected inputs: %s %d", intentID, now)
			}
			return "token-abc", 1700000600, "hls:key:asset-1", repo.PaymentRailLightning, nil
		},
	}

	api := NewAPI(svc, "secret", 100000)
	api.nowUnix = func() int64 { return 1700000000 }
	req := httptest.NewRequest(http.MethodPost, "/fap/token", bytes.NewBufferString(`{"intent_id":"intent-1"}`))
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Token != "token-abc" || out.ExpiresAt != 1700000600 || out.ResourceID != "hls:key:asset-1" || out.Rail != "lightning" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestWebhookLightniningEndpointWithProviderRef(t *testing.T) {
	called := false
	svc := &fakeService{
		createChallengeFn: func(_ context.Context, _ string, _ string, _ repo.PaymentRail, _ int64, _ repo.AmountUnit, _ string) (service.ChallengeResult, error) {
			return service.ChallengeResult{}, nil
		},
		handleWebhookFn: func(_ context.Context, rail repo.PaymentRail, providerRef string, now int64) error {
			called = true
			if rail != repo.PaymentRailLightning || providerRef != "ph-1" || now != 1700000000 {
				t.Fatalf("unexpected inputs rail=%s provider_ref=%s now=%d", rail, providerRef, now)
			}
			return nil
		},
		mintTokenFn: func(_ context.Context, _ string, _ int64) (string, int64, string, repo.PaymentRail, error) {
			return "", 0, "", "", nil
		},
	}

	api := NewAPI(svc, "secret", 100000)
	api.nowUnix = func() int64 { return 1700000000 }
	req := httptest.NewRequest(http.MethodPost, "/fap/webhook/lightning", bytes.NewBufferString(`{"provider_ref":"ph-1"}`))
	req.Header.Set("X-FAP-Webhook-Secret", "secret")
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if !called {
		t.Fatal("expected webhook handler to call service")
	}
}

func TestWebhookLNBitsAliasAcceptsPaymentHash(t *testing.T) {
	called := false
	svc := &fakeService{
		createChallengeFn: func(_ context.Context, _ string, _ string, _ repo.PaymentRail, _ int64, _ repo.AmountUnit, _ string) (service.ChallengeResult, error) {
			return service.ChallengeResult{}, nil
		},
		handleWebhookFn: func(_ context.Context, rail repo.PaymentRail, providerRef string, _ int64) error {
			called = true
			if rail != repo.PaymentRailLightning || providerRef != "ph-1" {
				t.Fatalf("unexpected webhook args rail=%s provider_ref=%s", rail, providerRef)
			}
			return nil
		},
		mintTokenFn: func(_ context.Context, _ string, _ int64) (string, int64, string, repo.PaymentRail, error) {
			return "", 0, "", "", nil
		},
	}

	api := NewAPI(svc, "secret", 100000)
	req := httptest.NewRequest(http.MethodPost, "/fap/webhook/lnbits", bytes.NewBufferString(`{"payment_hash":"ph-1"}`))
	req.Header.Set("X-FAP-Webhook-Secret", "secret")
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if !called {
		t.Fatal("expected webhook alias to call service")
	}
}

func TestWebhookEndpointRequiresSecret(t *testing.T) {
	svc := &fakeService{
		createChallengeFn: func(_ context.Context, _ string, _ string, _ repo.PaymentRail, _ int64, _ repo.AmountUnit, _ string) (service.ChallengeResult, error) {
			return service.ChallengeResult{}, nil
		},
		handleWebhookFn: func(_ context.Context, _ repo.PaymentRail, _ string, _ int64) error {
			return errors.New("should not be called")
		},
		mintTokenFn: func(_ context.Context, _ string, _ int64) (string, int64, string, repo.PaymentRail, error) {
			return "", 0, "", "", nil
		},
	}

	api := NewAPI(svc, "secret", 100000)
	req := httptest.NewRequest(http.MethodPost, "/fap/webhook/lightning", bytes.NewBufferString(`{"provider_ref":"ph-1"}`))
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMetricsEndpointSmoke(t *testing.T) {
	svc := &fakeService{
		createChallengeFn: func(_ context.Context, _ string, _ string, _ repo.PaymentRail, _ int64, _ repo.AmountUnit, _ string) (service.ChallengeResult, error) {
			return service.ChallengeResult{
				IntentID:    "intent-1",
				Rail:        repo.PaymentRailLightning,
				Offer:       "lnbc1...",
				ProviderRef: "ph-1",
				ExpiresAt:   1700000900,
			}, nil
		},
		handleWebhookFn: func(_ context.Context, _ repo.PaymentRail, _ string, _ int64) error { return nil },
		mintTokenFn: func(_ context.Context, _ string, _ int64) (string, int64, string, repo.PaymentRail, error) {
			return "", 0, "", "", nil
		},
	}

	api := NewAPI(svc, "secret", 100000)
	challengeReq := httptest.NewRequest(http.MethodPost, "/fap/challenge", bytes.NewBufferString(`{"asset_id":"asset-1","subject":"user-1"}`))
	challengeRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(challengeRec, challengeReq)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "fap_http_handler_requests_total") {
		t.Fatalf("expected metric name in response body, got: %s", rec.Body.String())
	}
}

func TestOpenAPIDocsEndpoints(t *testing.T) {
	svc := &fakeService{
		createChallengeFn: func(_ context.Context, _ string, _ string, _ repo.PaymentRail, _ int64, _ repo.AmountUnit, _ string) (service.ChallengeResult, error) {
			return service.ChallengeResult{}, nil
		},
		handleWebhookFn: func(_ context.Context, _ repo.PaymentRail, _ string, _ int64) error { return nil },
		mintTokenFn:     func(_ context.Context, _ string, _ int64) (string, int64, string, repo.PaymentRail, error) { return "", 0, "", "", nil },
	}
	api := NewAPI(svc, "secret", 100000)

	specReq := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	specRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(specRec, specReq)
	if specRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /openapi.yaml, got %d", specRec.Code)
	}
	if !strings.Contains(specRec.Body.String(), "openapi: 3.0.3") {
		t.Fatalf("expected OpenAPI header in spec, got: %s", specRec.Body.String())
	}

	docsReq := httptest.NewRequest(http.MethodGet, "/docs", nil)
	docsRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(docsRec, docsReq)
	if docsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /docs, got %d", docsRec.Code)
	}
	if !strings.Contains(docsRec.Body.String(), "@scalar/api-reference") {
		t.Fatalf("expected scalar script in docs HTML, got: %s", docsRec.Body.String())
	}
}

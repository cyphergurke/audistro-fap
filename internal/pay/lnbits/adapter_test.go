package lnbits

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yourorg/fap/internal/pay/payment"
	"github.com/yourorg/fap/internal/store/repo"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCreateOfferParsesFieldsAndExpiryFromResponse(t *testing.T) {
	var called int32

	client := NewClient("http://lnbits.test", "invoice-key", "readonly-key")
	client.nowUnix = func() int64 { return 1700000000 }
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/api/v1/payments" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if got := r.Header.Get("X-Api-Key"); got != "invoice-key" {
				t.Fatalf("unexpected invoice api key: %s", got)
			}
			atomic.AddInt32(&called, 1)

			return &http.Response{
				StatusCode: http.StatusCreated,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"payment_request":"lnbc123","payment_hash":"hash123","expires_at":1700000900}`)),
			}, nil
		}),
	}
	adapter := NewAdapterWithClient(client)

	resp, err := adapter.CreateOffer(context.Background(), payment.CreateOfferRequest{
		Amount:        100000,
		AmountUnit:    repo.AmountUnitMsat,
		Asset:         "BTC",
		Memo:          "test",
		ExpirySeconds: 900,
	})
	if err != nil {
		t.Fatalf("CreateOffer error: %v", err)
	}

	if resp.Offer != "lnbc123" {
		t.Fatalf("unexpected offer: %s", resp.Offer)
	}
	if resp.ProviderRef != "hash123" {
		t.Fatalf("unexpected provider ref: %s", resp.ProviderRef)
	}
	if resp.ExpiresAt != 1700000900 {
		t.Fatalf("unexpected expires_at: %d", resp.ExpiresAt)
	}
	if resp.Rail != repo.PaymentRailLightning {
		t.Fatalf("unexpected rail: %s", resp.Rail)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Fatalf("expected one request, got %d", called)
	}
}

func TestVerifySettlementReturnsSettledAndSettledAt(t *testing.T) {
	client := NewClient("http://lnbits.test", "invoice-key", "readonly-key")
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/api/v1/payments/hash123" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if got := r.Header.Get("X-Api-Key"); got != "readonly-key" {
				t.Fatalf("unexpected readonly api key: %s", got)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"payment_hash":"hash123","paid":true,"paid_at":1700000010}`)),
			}, nil
		}),
	}
	adapter := NewAdapterWithClient(client)

	resp, err := adapter.VerifySettlement(context.Background(), "hash123")
	if err != nil {
		t.Fatalf("VerifySettlement error: %v", err)
	}
	if !resp.Settled {
		t.Fatal("expected settled=true")
	}
	if resp.SettledAt == nil || *resp.SettledAt != 1700000010 {
		t.Fatalf("unexpected settledAt: %v", resp.SettledAt)
	}
}

func TestCreateOfferRetriesOn5xx(t *testing.T) {
	var called int32

	client := NewClient("http://lnbits.test", "invoice-key", "readonly-key")
	client.nowUnix = func() int64 { return 1700000000 }
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			n := atomic.AddInt32(&called, 1)
			if n <= 2 {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString("temporary")),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"payment_request":"lnbc123","payment_hash":"hash123"}`)),
			}, nil
		}),
	}
	adapter := NewAdapterWithClient(client)

	resp, err := adapter.CreateOffer(context.Background(), payment.CreateOfferRequest{
		Amount:        100000,
		AmountUnit:    repo.AmountUnitMsat,
		Asset:         "BTC",
		Memo:          "retry-test",
		ExpirySeconds: 900,
	})
	if err != nil {
		t.Fatalf("CreateOffer error: %v", err)
	}

	if resp.ProviderRef != "hash123" {
		t.Fatalf("unexpected provider ref: %s", resp.ProviderRef)
	}
	if resp.ExpiresAt != 1700000900 {
		t.Fatalf("unexpected expires_at fallback: %d", resp.ExpiresAt)
	}
	if atomic.LoadInt32(&called) != 3 {
		t.Fatalf("expected 3 attempts, got %d", called)
	}
}

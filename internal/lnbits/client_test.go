package lnbits

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientCreateInvoiceAndVerifyPayment(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("X-Api-Key") != "inv-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"payment_request":"lnbc1x","payment_hash":"ph1","checking_id":"chk1","expires_at":1700001000}`))
	})
	mux.HandleFunc("/api/v1/payments/chk1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("X-Api-Key") != "read-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":true,"time":1700002000}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewHTTPClient()
	inv, err := client.CreateInvoice(context.Background(), server.URL, "inv-key", 100_000, "memo", 900)
	if err != nil {
		t.Fatalf("CreateInvoice: %v", err)
	}
	if inv.Bolt11 != "lnbc1x" || inv.PaymentHash != "ph1" || inv.CheckingID != "chk1" || inv.ExpiresAt != 1700001000 {
		t.Fatalf("unexpected invoice: %+v", inv)
	}

	status, err := client.VerifyPayment(context.Background(), server.URL, "read-key", "chk1")
	if err != nil {
		t.Fatalf("VerifyPayment: %v", err)
	}
	if !status.Paid || status.PaidAt == nil || *status.PaidAt != 1700002000 {
		t.Fatalf("unexpected payment status: %+v", status)
	}
}

func TestHTTPClientCreateInvoiceHandlesStringExpiry(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"payment_request":"lnbc1x","payment_hash":"ph1","checking_id":"chk1","expiry":"900"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewHTTPClient()
	inv, err := client.CreateInvoice(context.Background(), server.URL, "inv-key", 100_000, "memo", 900)
	if err != nil {
		t.Fatalf("CreateInvoice: %v", err)
	}
	if inv.Bolt11 != "lnbc1x" || inv.PaymentHash != "ph1" || inv.CheckingID != "chk1" {
		t.Fatalf("unexpected invoice payload: %+v", inv)
	}
	if inv.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("expected future expires_at, got %d", inv.ExpiresAt)
	}
}

func TestHTTPClientVerifyPaymentHandlesStringTime(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/payments/chk-time", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":true,"time":"1700003000"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewHTTPClient()
	status, err := client.VerifyPayment(context.Background(), server.URL, "read-key", "chk-time")
	if err != nil {
		t.Fatalf("VerifyPayment: %v", err)
	}
	if !status.Paid || status.PaidAt == nil || *status.PaidAt != 1700003000 {
		t.Fatalf("unexpected payment status: %+v", status)
	}
}

func TestHTTPClientCreateInvoiceStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer server.Close()

	client := NewHTTPClient()
	_, err := client.CreateInvoice(context.Background(), server.URL, "inv-key", 100_000, "memo", 900)
	if err == nil {
		t.Fatal("expected error")
	}
	statusErr, ok := err.(*HTTPStatusError)
	if !ok {
		t.Fatalf("expected HTTPStatusError, got %T", err)
	}
	if statusErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("unexpected status: %d", statusErr.StatusCode)
	}
}

package merchantlnbits

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateInvoiceAndIsSettled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("X-Api-Key") != "invoice-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"payment_request":"lnbc1abc","payment_hash":"ph123","expires_at":1700001000}`))
	})
	mux.HandleFunc("/api/v1/payments/ph123", func(w http.ResponseWriter, r *http.Request) {
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

	adapter := New(server.URL, "invoice-key", "read-key")

	bolt11, paymentHash, checkingID, expiresAt, err := adapter.CreateInvoice(context.Background(), 100000, "memo", 900)
	if err != nil {
		t.Fatalf("CreateInvoice: %v", err)
	}
	if bolt11 != "lnbc1abc" || paymentHash != "ph123" || checkingID != "ph123" || expiresAt != 1700001000 {
		t.Fatalf("unexpected invoice response: %s %s %s %d", bolt11, paymentHash, checkingID, expiresAt)
	}

	settled, settledAt, err := adapter.IsSettled(context.Background(), paymentHash)
	if err != nil {
		t.Fatalf("IsSettled: %v", err)
	}
	if !settled || settledAt == nil || *settledAt != 1700002000 {
		t.Fatalf("unexpected settlement: settled=%v settledAt=%v", settled, settledAt)
	}
}

func TestCreateInvoiceServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	adapter := New(server.URL, "invoice-key", "read-key")
	_, _, _, _, err := adapter.CreateInvoice(context.Background(), 100000, "memo", 900)
	if err == nil || !strings.Contains(err.Error(), "lnbits status 500") {
		t.Fatalf("expected lnbits status error, got: %v", err)
	}
}

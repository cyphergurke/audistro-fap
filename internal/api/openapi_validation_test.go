package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAPIValidationRejectsWrongContentTypeForPayees(t *testing.T) {
	api := NewWithOptions(nil, Options{})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	body := []byte(`{"display_name":"Smoke Payee","lnbits_base_url":"http://lnbits:5000"}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/payees", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var out errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Error != "invalid_request" {
		t.Fatalf("expected invalid_request, got %#v", out)
	}
}

func TestOpenAPIValidationRejectsWrongContentTypeForChallenge(t *testing.T) {
	api := NewWithOptions(nil, Options{})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	body := []byte(`{"asset_id":"asset1"}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/fap/challenge", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var out errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Error != "invalid_request" {
		t.Fatalf("expected invalid_request, got %#v", out)
	}
}

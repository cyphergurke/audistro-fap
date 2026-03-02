package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type openAPIValidationResponse struct {
	Error   string         `json:"error"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func TestOpenAPIValidationRejectsChallengeWrongContentType(t *testing.T) {
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
	assertOpenAPIInvalidRequest(t, resp)
}

func TestOpenAPIValidationRejectsTokenWrongContentType(t *testing.T) {
	api := NewWithOptions(nil, Options{})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	body := []byte(`{"intent_id":"intent-1","subject":"sub-1"}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/fap/token", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	assertOpenAPIInvalidRequest(t, resp)
}

func TestOpenAPIValidationRejectsHLSKeyWithInvalidAssetID(t *testing.T) {
	api := NewWithOptions(nil, Options{})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/hls/asset$id/key", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	assertOpenAPIInvalidRequest(t, resp)
}

func TestOpenAPIValidationRejectsPackagingKeyWithInvalidAssetID(t *testing.T) {
	api := NewWithOptions(nil, Options{
		AdminToken:           "admin-secret",
		InternalAllowedCIDRs: "127.0.0.1/32",
	})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/internal/assets/asset$id/packaging-key", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Admin-Token", "admin-secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	assertOpenAPIInvalidRequest(t, resp)
}

func assertOpenAPIInvalidRequest(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var out openAPIValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Error != "invalid_request" {
		t.Fatalf("expected invalid_request, got %#v", out)
	}
	if out.Message == "" {
		t.Fatalf("expected validation message, got %#v", out)
	}
	if len(out.Details) == 0 {
		t.Fatalf("expected validation details, got %#v", out)
	}
}

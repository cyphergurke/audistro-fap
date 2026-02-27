package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"audistro-fap/internal/crypto/secretbox"
	"audistro-fap/internal/pay"
	"audistro-fap/internal/service"
	"audistro-fap/internal/store"
)

type fakeAdapter struct {
	settled bool
}

func (a *fakeAdapter) CreateInvoice(_ context.Context, _ int64, _ string, _ int64) (string, string, string, int64, error) {
	return "lnbc1fake", "ph-http-1", "chk-http-1", 1900000000, nil
}

func (a *fakeAdapter) IsSettled(_ context.Context, _ string) (bool, *int64, error) {
	if !a.settled {
		return false, nil, nil
	}
	ts := int64(1700000100)
	return true, &ts, nil
}

type factory struct {
	adapterByPayee map[string]*fakeAdapter
}

func (f *factory) ForPayee(_ context.Context, payeeID string) (pay.PaymentAdapter, error) {
	ad, ok := f.adapterByPayee[payeeID]
	if !ok {
		ad = &fakeAdapter{}
		f.adapterByPayee[payeeID] = ad
	}
	return ad, nil
}

func TestOpenAPIAndDocsEndpoints(t *testing.T) {
	api := NewWithOptions(nil, Options{})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	specResp := mustRequest(t, http.MethodGet, ts.URL+"/openapi.yaml", nil)
	if specResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /openapi.yaml status=%d body=%s", specResp.StatusCode, readBody(t, specResp))
	}
	if ct := specResp.Header.Get("Content-Type"); !strings.Contains(ct, "application/yaml") {
		t.Fatalf("expected yaml content-type, got %q", ct)
	}
	specBody := readBody(t, specResp)
	requiredPaths := []string{
		"/healthz:",
		"/openapi.yaml:",
		"/docs:",
		"/v1/payees:",
		"/v1/assets:",
		"/v1/boost:",
		"/v1/boost/{boostId}:",
		"/v1/boost/{boostId}/mark_paid:",
		"/v1/ledger:",
		"/v1/fap/challenge:",
		"/v1/device/bootstrap:",
		"/v1/fap/webhook/lnbits:",
		"/v1/fap/token:",
		"/v1/access/{assetId}:",
		"/v1/access/grants:",
		"/hls/{assetId}/key:",
	}
	for _, path := range requiredPaths {
		if !strings.Contains(specBody, path) {
			t.Fatalf("openapi spec missing path %s", path)
		}
	}

	docsResp := mustRequest(t, http.MethodGet, ts.URL+"/docs", nil)
	if docsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /docs status=%d body=%s", docsResp.StatusCode, readBody(t, docsResp))
	}
	if ct := docsResp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected html content-type, got %q", ct)
	}
	docsBody := readBody(t, docsResp)
	if !strings.Contains(docsBody, "data-url=\"/openapi.yaml\"") {
		t.Fatalf("docs page missing openapi url")
	}
	if !strings.Contains(docsBody, "@scalar/api-reference") {
		t.Fatalf("docs page missing scalar script")
	}
}

func TestHTTPFlow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              true,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
		HLSKeyDerive: func(_ string) [16]byte {
			return [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		},
	})
	master := bytes.Repeat([]byte{0x44}, 32)
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithMasterKey(r.Context(), master)
		ctx = WithEncryptor(ctx, secretbox.Encrypt)
		api.Router().ServeHTTP(w, r.WithContext(ctx))
	})
	ts := httptest.NewServer(router)
	defer ts.Close()

	payeeID := createPayee(t, ts.URL)
	createAsset(t, ts.URL, payeeID)
	challenge := createChallenge(t, ts.URL)

	status := postToken(t, ts.URL, challenge.IntentID, challenge.DeviceCookie, "sub-1")
	if status != http.StatusConflict {
		t.Fatalf("expected 409 before settlement, got %d", status)
	}

	adapter := f.adapterByPayee[payeeID]
	adapter.settled = true
	callWebhook(t, ts.URL, challenge.PaymentHash)

	status = postToken(t, ts.URL, challenge.IntentID, challenge.DeviceCookie, "sub-1")
	if status != http.StatusOK {
		t.Fatalf("expected 200 after settlement, got %d", status)
	}
}

func TestDeviceBootstrapSetsCookie(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              false,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
	})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	first := mustRequest(t, http.MethodPost, ts.URL+"/v1/device/bootstrap", nil)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/device/bootstrap status=%d body=%s", first.StatusCode, readBody(t, first))
	}
	var firstOut deviceBootstrapResponse
	if err := json.NewDecoder(first.Body).Decode(&firstOut); err != nil {
		t.Fatalf("decode first bootstrap response: %v", err)
	}
	cookies := first.Cookies()
	_ = first.Body.Close()
	if len(cookies) == 0 {
		t.Fatal("expected bootstrap to set cookie")
	}
	deviceCookie := ""
	for _, cookie := range cookies {
		if cookie.Name == "fap_device_id" {
			deviceCookie = cookie.Name + "=" + cookie.Value
			break
		}
	}
	if deviceCookie == "" || firstOut.DeviceID == "" {
		t.Fatalf("expected fap_device_id cookie and device_id response, cookie=%q body=%+v", deviceCookie, firstOut)
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/device/bootstrap", nil)
	if err != nil {
		t.Fatalf("new second bootstrap request: %v", err)
	}
	req.Header.Set("Cookie", deviceCookie)
	second, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do second bootstrap: %v", err)
	}
	if second.StatusCode != http.StatusOK {
		t.Fatalf("second bootstrap status=%d body=%s", second.StatusCode, readBody(t, second))
	}
	var secondOut deviceBootstrapResponse
	if err := json.NewDecoder(second.Body).Decode(&secondOut); err != nil {
		t.Fatalf("decode second bootstrap response: %v", err)
	}
	_ = second.Body.Close()
	if secondOut.DeviceID != firstOut.DeviceID {
		t.Fatalf("expected same device_id on repeated bootstrap, got %s vs %s", secondOut.DeviceID, firstOut.DeviceID)
	}
}

func TestHTTPChallengeCatalogModeWithoutAsset(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              false,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
		HLSKeyDerive: func(_ string) [16]byte {
			return [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		},
	})
	master := bytes.Repeat([]byte{0x44}, 32)
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithMasterKey(r.Context(), master)
		ctx = WithEncryptor(ctx, secretbox.Encrypt)
		api.Router().ServeHTTP(w, r.WithContext(ctx))
	})
	ts := httptest.NewServer(router)
	defer ts.Close()

	payeeID := createPayee(t, ts.URL)
	ch := createCatalogChallenge(t, ts.URL, payeeID, "asset-catalog-http-1", "idem-http-catalog-1")

	status := postTokenByChallenge(t, ts.URL, ch.ChallengeID, ch.DeviceCookie, "")
	if status != http.StatusConflict {
		t.Fatalf("expected 409 before settlement, got %d", status)
	}

	callWebhookWithCheckingID(t, ts.URL, "chk-http-1")

	status = postTokenByChallenge(t, ts.URL, ch.ChallengeID, ch.DeviceCookie, "")
	if status != http.StatusOK {
		t.Fatalf("expected 200 after settlement, got %d", status)
	}

	tokenReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/fap/token", bytes.NewReader([]byte(`{"challenge_id":"`+ch.ChallengeID+`"}`)))
	if err != nil {
		t.Fatalf("new token request: %v", err)
	}
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenReq.Header.Set("Cookie", ch.DeviceCookie)
	tokenResp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		t.Fatalf("do token request: %v", err)
	}
	if tokenResp.StatusCode != http.StatusOK {
		t.Fatalf("expected token response 200, got %d body=%s", tokenResp.StatusCode, readBody(t, tokenResp))
	}
	var minted tokenResponse
	if err := json.NewDecoder(tokenResp.Body).Decode(&minted); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	_ = tokenResp.Body.Close()
	if strings.TrimSpace(minted.Token) == "" {
		t.Fatalf("expected token in response, got %+v", minted)
	}

	keyReq, err := http.NewRequest(http.MethodGet, ts.URL+"/hls/asset-catalog-http-1/key", nil)
	if err != nil {
		t.Fatalf("new key request: %v", err)
	}
	keyReq.Header.Set("Authorization", "Bearer "+minted.Token)
	keyReq.Header.Set("Cookie", ch.DeviceCookie)
	keyResp, err := http.DefaultClient.Do(keyReq)
	if err != nil {
		t.Fatalf("do key request: %v", err)
	}
	keyBytes, _ := io.ReadAll(keyResp.Body)
	_ = keyResp.Body.Close()
	if keyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected hls key status 200, got %d body=%s", keyResp.StatusCode, string(keyBytes))
	}
	if len(keyBytes) != 16 {
		t.Fatalf("expected 16-byte key, got %d", len(keyBytes))
	}
}

func TestTokenRejectsDeviceMismatch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              false,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}
	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
	})
	master := bytes.Repeat([]byte{0x44}, 32)
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithMasterKey(r.Context(), master)
		ctx = WithEncryptor(ctx, secretbox.Encrypt)
		api.Router().ServeHTTP(w, r.WithContext(ctx))
	})
	ts := httptest.NewServer(router)
	defer ts.Close()

	payeeID := createPayee(t, ts.URL)
	ch := createCatalogChallenge(t, ts.URL, payeeID, "asset-catalog-http-2", "idem-http-catalog-2")
	callWebhookWithCheckingID(t, ts.URL, "chk-http-1")

	status := postTokenByChallenge(t, ts.URL, ch.ChallengeID, "fap_device_id=device_other", "")
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for device mismatch, got %d", status)
	}
}

func TestLedgerReturnsOnlyCurrentDeviceEntries(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              false,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}
	api := NewWithOptions(svc, Options{WebhookSecret: "hook-secret"})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	if err := repo.InsertLedgerEntryIfNotExists(context.Background(), store.LedgerEntry{
		EntryID:    "entry-device-a-1",
		DeviceID:   "device_a",
		Kind:       "access",
		Status:     "paid",
		AssetID:    "asset-a",
		PayeeID:    "payee-a",
		AmountMSat: 1000,
		Currency:   "msat",
		RelatedID:  "challenge-a-1",
		CreatedAt:  100,
		UpdatedAt:  100,
	}); err != nil {
		t.Fatalf("InsertLedgerEntryIfNotExists device_a: %v", err)
	}
	if err := repo.InsertLedgerEntryIfNotExists(context.Background(), store.LedgerEntry{
		EntryID:    "entry-device-b-1",
		DeviceID:   "device_b",
		Kind:       "boost",
		Status:     "pending",
		AssetID:    "asset-b",
		PayeeID:    "payee-b",
		AmountMSat: 2000,
		Currency:   "msat",
		RelatedID:  "boost-b-1",
		CreatedAt:  101,
		UpdatedAt:  101,
	}); err != nil {
		t.Fatalf("InsertLedgerEntryIfNotExists device_b: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/ledger", nil)
	if err != nil {
		t.Fatalf("new ledger request: %v", err)
	}
	req.Header.Set("Cookie", "fap_device_id=device_a")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ledger request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var out ledgerListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode ledger response: %v", err)
	}
	_ = resp.Body.Close()
	if out.DeviceID != "device_a" {
		t.Fatalf("expected device_id device_a, got %s", out.DeviceID)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item for device_a, got %d", len(out.Items))
	}
	if out.Items[0].EntryID != "entry-device-a-1" {
		t.Fatalf("unexpected ledger entry %+v", out.Items[0])
	}
}

func TestLedgerPaginationAndFilters(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              false,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}
	api := NewWithOptions(svc, Options{WebhookSecret: "hook-secret"})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	seed := []store.LedgerEntry{
		{EntryID: "entry-3", DeviceID: "device_p", Kind: "access", Status: "paid", AssetID: "asset-p", PayeeID: "payee-p", AmountMSat: 3000, Currency: "msat", RelatedID: "challenge-3", CreatedAt: 300, UpdatedAt: 300},
		{EntryID: "entry-2", DeviceID: "device_p", Kind: "boost", Status: "pending", AssetID: "asset-q", PayeeID: "payee-p", AmountMSat: 2000, Currency: "msat", RelatedID: "boost-2", CreatedAt: 200, UpdatedAt: 200},
		{EntryID: "entry-1", DeviceID: "device_p", Kind: "access", Status: "paid", AssetID: "asset-p", PayeeID: "payee-p", AmountMSat: 1000, Currency: "msat", RelatedID: "challenge-1", CreatedAt: 100, UpdatedAt: 100},
	}
	for _, item := range seed {
		if err := repo.InsertLedgerEntryIfNotExists(context.Background(), item); err != nil {
			t.Fatalf("seed ledger entry %s: %v", item.EntryID, err)
		}
	}

	firstReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/ledger?limit=2", nil)
	if err != nil {
		t.Fatalf("new first ledger request: %v", err)
	}
	firstReq.Header.Set("Cookie", "fap_device_id=device_p")
	firstResp, err := http.DefaultClient.Do(firstReq)
	if err != nil {
		t.Fatalf("first ledger request failed: %v", err)
	}
	if firstResp.StatusCode != http.StatusOK {
		t.Fatalf("expected first ledger 200, got %d body=%s", firstResp.StatusCode, readBody(t, firstResp))
	}
	var firstOut ledgerListResponse
	if err := json.NewDecoder(firstResp.Body).Decode(&firstOut); err != nil {
		t.Fatalf("decode first ledger response: %v", err)
	}
	_ = firstResp.Body.Close()
	if len(firstOut.Items) != 2 {
		t.Fatalf("expected 2 items on first page, got %d", len(firstOut.Items))
	}
	if firstOut.Items[0].EntryID != "entry-3" || firstOut.Items[1].EntryID != "entry-2" {
		t.Fatalf("unexpected order on first page: %+v", firstOut.Items)
	}
	if strings.TrimSpace(firstOut.NextCursor) == "" {
		t.Fatal("expected next_cursor on first page")
	}

	secondReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/ledger?limit=2&cursor="+neturl.QueryEscape(firstOut.NextCursor), nil)
	if err != nil {
		t.Fatalf("new second ledger request: %v", err)
	}
	secondReq.Header.Set("Cookie", "fap_device_id=device_p")
	secondResp, err := http.DefaultClient.Do(secondReq)
	if err != nil {
		t.Fatalf("second ledger request failed: %v", err)
	}
	if secondResp.StatusCode != http.StatusOK {
		t.Fatalf("expected second ledger 200, got %d body=%s", secondResp.StatusCode, readBody(t, secondResp))
	}
	var secondOut ledgerListResponse
	if err := json.NewDecoder(secondResp.Body).Decode(&secondOut); err != nil {
		t.Fatalf("decode second ledger response: %v", err)
	}
	_ = secondResp.Body.Close()
	if len(secondOut.Items) != 1 || secondOut.Items[0].EntryID != "entry-1" {
		t.Fatalf("unexpected second page: %+v", secondOut.Items)
	}

	filterReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/ledger?kind=access&status=paid&asset_id=asset-p", nil)
	if err != nil {
		t.Fatalf("new filtered ledger request: %v", err)
	}
	filterReq.Header.Set("Cookie", "fap_device_id=device_p")
	filterResp, err := http.DefaultClient.Do(filterReq)
	if err != nil {
		t.Fatalf("filtered ledger request failed: %v", err)
	}
	if filterResp.StatusCode != http.StatusOK {
		t.Fatalf("expected filtered ledger 200, got %d body=%s", filterResp.StatusCode, readBody(t, filterResp))
	}
	var filterOut ledgerListResponse
	if err := json.NewDecoder(filterResp.Body).Decode(&filterOut); err != nil {
		t.Fatalf("decode filtered ledger response: %v", err)
	}
	_ = filterResp.Body.Close()
	if len(filterOut.Items) != 2 {
		t.Fatalf("expected 2 filtered entries, got %d", len(filterOut.Items))
	}
	for _, item := range filterOut.Items {
		if item.Kind != "access" || item.Status != "paid" || item.AssetID != "asset-p" {
			t.Fatalf("unexpected filtered item: %+v", item)
		}
	}
}

func TestRateLimitChallengeByDeviceCookie(t *testing.T) {
	api := NewWithOptions(nil, Options{
		ChallengeRateRPS:   0.000001,
		ChallengeRateBurst: 1,
		TokenRateRPS:       1000,
		TokenRateBurst:     1000,
		KeyRateRPS:         1000,
		KeyRateBurst:       1000,
	})
	router := api.Router()

	req1 := httptest.NewRequest(http.MethodPost, "/v1/fap/challenge", bytes.NewReader([]byte(`{`)))
	req1.Header.Set("Content-Type", "application/json")
	req1.AddCookie(&http.Cookie{Name: "fap_device_id", Value: "device-rate-1"})
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code == http.StatusTooManyRequests {
		t.Fatalf("first challenge request should not be rate-limited: %s", rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/v1/fap/challenge", bytes.NewReader([]byte(`{`)))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(&http.Cookie{Name: "fap_device_id", Value: "device-rate-1"})
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second challenge request to be rate-limited, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestRateLimitTokenFallsBackToIPWithoutCookie(t *testing.T) {
	api := NewWithOptions(nil, Options{
		ChallengeRateRPS:   1000,
		ChallengeRateBurst: 1000,
		TokenRateRPS:       0.000001,
		TokenRateBurst:     1,
		KeyRateRPS:         1000,
		KeyRateBurst:       1000,
	})
	router := api.Router()

	req1 := httptest.NewRequest(http.MethodPost, "/v1/fap/token", bytes.NewReader([]byte(`{`)))
	req1.Header.Set("Content-Type", "application/json")
	req1.RemoteAddr = "203.0.113.10:12345"
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code == http.StatusTooManyRequests {
		t.Fatalf("first token request should not be rate-limited: %s", rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/v1/fap/token", bytes.NewReader([]byte(`{`)))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "203.0.113.10:12345"
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second token request to be rate-limited by IP, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestRateLimitHLSKeyByDeviceCookie(t *testing.T) {
	api := NewWithOptions(nil, Options{
		ChallengeRateRPS:   1000,
		ChallengeRateBurst: 1000,
		TokenRateRPS:       1000,
		TokenRateBurst:     1000,
		KeyRateRPS:         0.000001,
		KeyRateBurst:       1,
	})
	router := api.Router()

	req1 := httptest.NewRequest(http.MethodGet, "/hls/asset-rate/key", nil)
	req1.AddCookie(&http.Cookie{Name: "fap_device_id", Value: "device-rate-key-1"})
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code == http.StatusTooManyRequests {
		t.Fatalf("first hls key request should not be rate-limited: %s", rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/hls/asset-rate/key", nil)
	req2.AddCookie(&http.Cookie{Name: "fap_device_id", Value: "device-rate-key-1"})
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second hls key request to be rate-limited, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestHLSKeyReturnsPaymentRequiredWithoutGrant(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              false,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
		HLSKeyDerive: func(_ string) [16]byte {
			return [16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
		},
	})
	master := bytes.Repeat([]byte{0x44}, 32)
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithMasterKey(r.Context(), master)
		ctx = WithEncryptor(ctx, secretbox.Encrypt)
		api.Router().ServeHTTP(w, r.WithContext(ctx))
	})
	ts := httptest.NewServer(router)
	defer ts.Close()

	payeeID := createPayee(t, ts.URL)
	createAsset(t, ts.URL, payeeID)
	challenge := createChallenge(t, ts.URL)
	f.adapterByPayee[payeeID].settled = true
	callWebhook(t, ts.URL, challenge.PaymentHash)

	tokenReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/fap/token", bytes.NewReader([]byte(`{"challenge_id":"`+challenge.ChallengeID+`"}`)))
	if err != nil {
		t.Fatalf("new token request: %v", err)
	}
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenReq.Header.Set("Cookie", challenge.DeviceCookie)
	tokenResp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		t.Fatalf("token request failed: %v", err)
	}
	if tokenResp.StatusCode != http.StatusOK {
		t.Fatalf("expected token status 200, got %d body=%s", tokenResp.StatusCode, readBody(t, tokenResp))
	}
	var minted tokenResponse
	if err := json.NewDecoder(tokenResp.Body).Decode(&minted); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	_ = tokenResp.Body.Close()

	keyReq, err := http.NewRequest(http.MethodGet, ts.URL+"/hls/asset-http-1/key", nil)
	if err != nil {
		t.Fatalf("new key request: %v", err)
	}
	keyReq.Header.Set("Authorization", "Bearer "+minted.Token)
	keyReq.Header.Set("Cookie", challenge.DeviceCookie)
	keyResp, err := http.DefaultClient.Do(keyReq)
	if err != nil {
		t.Fatalf("key request failed: %v", err)
	}
	if keyResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 payment_required, got %d body=%s", keyResp.StatusCode, readBody(t, keyResp))
	}
	var out errorResponse
	if err := json.NewDecoder(keyResp.Body).Decode(&out); err != nil {
		t.Fatalf("decode key error: %v", err)
	}
	_ = keyResp.Body.Close()
	if out.Error != "payment_required" {
		t.Fatalf("expected payment_required, got %q", out.Error)
	}
}

func TestHLSKeyReturnsGrantExpired(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              false,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
		HLSKeyDerive: func(_ string) [16]byte {
			return [16]byte{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}
		},
	})
	master := bytes.Repeat([]byte{0x44}, 32)
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithMasterKey(r.Context(), master)
		ctx = WithEncryptor(ctx, secretbox.Encrypt)
		api.Router().ServeHTTP(w, r.WithContext(ctx))
	})
	ts := httptest.NewServer(router)
	defer ts.Close()

	payeeID := createPayee(t, ts.URL)
	challenge := createCatalogChallenge(t, ts.URL, payeeID, "asset-expired-1", "idem-expired-1")
	callWebhookWithCheckingID(t, ts.URL, "chk-http-1")

	tokenReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/fap/token", bytes.NewReader([]byte(`{"challenge_id":"`+challenge.ChallengeID+`"}`)))
	if err != nil {
		t.Fatalf("new token request: %v", err)
	}
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenReq.Header.Set("Cookie", challenge.DeviceCookie)
	tokenResp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		t.Fatalf("token request failed: %v", err)
	}
	if tokenResp.StatusCode != http.StatusOK {
		t.Fatalf("expected token status 200, got %d body=%s", tokenResp.StatusCode, readBody(t, tokenResp))
	}
	var minted tokenResponse
	if err := json.NewDecoder(tokenResp.Body).Decode(&minted); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	_ = tokenResp.Body.Close()

	firstKeyReq, err := http.NewRequest(http.MethodGet, ts.URL+"/hls/asset-expired-1/key", nil)
	if err != nil {
		t.Fatalf("new first key request: %v", err)
	}
	firstKeyReq.Header.Set("Authorization", "Bearer "+minted.Token)
	firstKeyReq.Header.Set("Cookie", challenge.DeviceCookie)
	firstKeyResp, err := http.DefaultClient.Do(firstKeyReq)
	if err != nil {
		t.Fatalf("first key request failed: %v", err)
	}
	firstKeyBody, _ := io.ReadAll(firstKeyResp.Body)
	_ = firstKeyResp.Body.Close()
	if firstKeyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected first key request 200, got %d body=%s", firstKeyResp.StatusCode, string(firstKeyBody))
	}

	grant, err := repo.GetAccessGrantByChallengeID(context.Background(), challenge.ChallengeID)
	if err != nil {
		t.Fatalf("GetAccessGrantByChallengeID: %v", err)
	}
	if err := repo.UpdateAccessGrantStatus(context.Background(), grant.GrantID, "expired", time.Now().Unix()); err != nil {
		t.Fatalf("UpdateAccessGrantStatus expired: %v", err)
	}

	secondKeyReq, err := http.NewRequest(http.MethodGet, ts.URL+"/hls/asset-expired-1/key", nil)
	if err != nil {
		t.Fatalf("new second key request: %v", err)
	}
	secondKeyReq.Header.Set("Authorization", "Bearer "+minted.Token)
	secondKeyReq.Header.Set("Cookie", challenge.DeviceCookie)
	secondKeyResp, err := http.DefaultClient.Do(secondKeyReq)
	if err != nil {
		t.Fatalf("second key request failed: %v", err)
	}
	if secondKeyResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 grant_expired, got %d body=%s", secondKeyResp.StatusCode, readBody(t, secondKeyResp))
	}
	var out errorResponse
	if err := json.NewDecoder(secondKeyResp.Body).Decode(&out); err != nil {
		t.Fatalf("decode second key response: %v", err)
	}
	_ = secondKeyResp.Body.Close()
	if out.Error != "grant_expired" {
		t.Fatalf("expected grant_expired, got %q", out.Error)
	}
}

type challengeResp struct {
	ChallengeID  string `json:"challenge_id"`
	IntentID     string `json:"intent_id"`
	DeviceID     string `json:"device_id"`
	AssetID      string `json:"asset_id"`
	PayeeID      string `json:"payee_id"`
	PaymentHash  string `json:"payment_hash"`
	CheckingID   string `json:"checking_id"`
	DeviceCookie string `json:"-"`
}

func createPayee(t *testing.T, baseURL string) string {
	t.Helper()
	body := []byte(`{"display_name":"Artist","lnbits_base_url":"http://lnbits","FAP_LNBITS_INVOICE_API_KEY":"inv","FAP_LNBITS_READONLY_API_KEY":"read"}`)
	resp, err := http.Post(baseURL+"/v1/payees", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create payee: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create payee status %d", resp.StatusCode)
	}
	var out struct {
		PayeeID string `json:"payee_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode payee: %v", err)
	}
	return out.PayeeID
}

func createAsset(t *testing.T, baseURL string, payeeID string) {
	t.Helper()
	body := []byte(`{"asset_id":"asset-http-1","payee_id":"` + payeeID + `","title":"Song","price_msat":1000}`)
	resp, err := http.Post(baseURL+"/v1/assets", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create asset: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create asset status %d", resp.StatusCode)
	}
}

func createChallenge(t *testing.T, baseURL string) challengeResp {
	t.Helper()
	body := []byte(`{"asset_id":"asset-http-1","subject":"sub-1"}`)
	resp, err := http.Post(baseURL+"/v1/fap/challenge", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create challenge status %d", resp.StatusCode)
	}
	var out challengeResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "fap_device_id" {
			out.DeviceCookie = cookie.Name + "=" + cookie.Value
		}
	}
	if out.DeviceCookie == "" {
		t.Fatal("expected fap_device_id cookie on challenge response")
	}
	return out
}

func createCatalogChallenge(t *testing.T, baseURL string, payeeID string, assetID string, idempotencyKey string) challengeResp {
	t.Helper()
	body := []byte(`{"asset_id":"` + assetID + `","payee_id":"` + payeeID + `","amount_msat":1500,"memo":"catalog access","idempotency_key":"` + idempotencyKey + `"}`)
	resp, err := http.Post(baseURL+"/v1/fap/challenge", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create catalog challenge: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create catalog challenge status %d", resp.StatusCode)
	}
	var out challengeResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode catalog challenge: %v", err)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "fap_device_id" {
			out.DeviceCookie = cookie.Name + "=" + cookie.Value
		}
	}
	if out.DeviceCookie == "" {
		t.Fatal("expected fap_device_id cookie on catalog challenge response")
	}
	if out.ChallengeID == "" {
		t.Fatalf("expected challenge_id in catalog challenge response, got %+v", out)
	}
	return out
}

func postToken(t *testing.T, baseURL string, intentID string, cookie string, subject string) int {
	t.Helper()
	body := []byte(`{"intent_id":"` + intentID + `","subject":"` + subject + `"}`)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/fap/token", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new token request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post token: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func postTokenByChallenge(t *testing.T, baseURL string, challengeID string, cookie string, subject string) int {
	t.Helper()
	body := []byte(`{"challenge_id":"` + challengeID + `","subject":"` + subject + `"}`)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/fap/token", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new token-by-challenge request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post token by challenge: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func callWebhook(t *testing.T, baseURL string, paymentHash string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/fap/webhook/lnbits", bytes.NewReader([]byte(`{"payment_hash":"`+paymentHash+`"}`)))
	if err != nil {
		t.Fatalf("new webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-FAP-Webhook-Secret", "hook-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("webhook status %d", resp.StatusCode)
	}
}

func callWebhookWithCheckingID(t *testing.T, baseURL string, checkingID string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/fap/webhook/lnbits", bytes.NewReader([]byte(`{"checking_id":"`+checkingID+`","paid":true}`)))
	if err != nil {
		t.Fatalf("new webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-FAP-Webhook-Secret", "hook-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("webhook status %d", resp.StatusCode)
	}
}

func TestBoostEndpointsDevFlow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              true,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
		DevMode:       true,
	})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	createBody := []byte(`{"asset_id":"asset-boost-1","payee_id":"fap_payee_1","amount_msat":1000000,"memo":"boost","idempotency_key":"idem-http-boost-1"}`)
	createResp := mustRequest(t, http.MethodPost, ts.URL+"/v1/boost", createBody)
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/boost status %d body=%s", createResp.StatusCode, readBody(t, createResp))
	}
	var created boostResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create boost response: %v", err)
	}
	_ = createResp.Body.Close()
	if created.Status != "pending" || created.BoostID == "" {
		t.Fatalf("unexpected create boost response: %+v", created)
	}

	deviceCookie := ""
	for _, cookie := range createResp.Cookies() {
		if cookie.Name == "fap_device_id" {
			deviceCookie = cookie.Name + "=" + cookie.Value
			break
		}
	}
	if deviceCookie == "" {
		t.Fatal("expected fap_device_id cookie on boost create")
	}

	createAgainReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/boost", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("new duplicate boost request: %v", err)
	}
	createAgainReq.Header.Set("Content-Type", "application/json")
	createAgainReq.Header.Set("Cookie", deviceCookie)
	createAgain, err := http.DefaultClient.Do(createAgainReq)
	if err != nil {
		t.Fatalf("duplicate boost request failed: %v", err)
	}
	if createAgain.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/boost duplicate status %d body=%s", createAgain.StatusCode, readBody(t, createAgain))
	}
	var createdAgain boostResponse
	if err := json.NewDecoder(createAgain.Body).Decode(&createdAgain); err != nil {
		t.Fatalf("decode duplicate boost response: %v", err)
	}
	_ = createAgain.Body.Close()
	if createdAgain.BoostID != created.BoostID {
		t.Fatalf("expected idempotent boost id, got %s vs %s", createdAgain.BoostID, created.BoostID)
	}

	getResp := mustRequest(t, http.MethodGet, ts.URL+"/v1/boost/"+created.BoostID, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/boost/{id} status %d body=%s", getResp.StatusCode, readBody(t, getResp))
	}
	var status boostStatusResponse
	if err := json.NewDecoder(getResp.Body).Decode(&status); err != nil {
		t.Fatalf("decode get boost response: %v", err)
	}
	_ = getResp.Body.Close()
	if status.Status != "pending" {
		t.Fatalf("expected pending status, got %s", status.Status)
	}

	markPaidResp := mustRequest(t, http.MethodPost, ts.URL+"/v1/boost/"+created.BoostID+"/mark_paid", []byte(`{}`))
	if markPaidResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/boost/{id}/mark_paid status %d body=%s", markPaidResp.StatusCode, readBody(t, markPaidResp))
	}
	var paidStatus boostStatusResponse
	if err := json.NewDecoder(markPaidResp.Body).Decode(&paidStatus); err != nil {
		t.Fatalf("decode paid boost response: %v", err)
	}
	_ = markPaidResp.Body.Close()
	if paidStatus.Status != "paid" || paidStatus.PaidAt == nil {
		t.Fatalf("expected paid status, got %+v", paidStatus)
	}
}

func TestBoostCreateWorksOutsideDevMode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              false,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
		DevMode:       false,
	})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	resp := mustRequest(t, http.MethodPost, ts.URL+"/v1/boost", []byte(`{"asset_id":"asset-boost-2","payee_id":"fap_payee_2","amount_msat":1000,"idempotency_key":"idem-http-boost-2"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var out boostResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode boost response: %v", err)
	}
	_ = resp.Body.Close()
	if out.Bolt11 == "" {
		t.Fatal("expected bolt11 in response")
	}
}

func TestBoostWebhookMarksPaidWithValidSignature(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              false,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}
	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
		DevMode:       false,
	})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	createResp := mustRequest(t, http.MethodPost, ts.URL+"/v1/boost", []byte(`{"asset_id":"asset-boost-webhook","payee_id":"payee-webhook","amount_msat":1000000,"idempotency_key":"idem-webhook-1"}`))
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/boost status %d body=%s", createResp.StatusCode, readBody(t, createResp))
	}
	var created boostResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode boost response: %v", err)
	}
	_ = createResp.Body.Close()

	webhookReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/fap/webhook/lnbits", bytes.NewReader([]byte(`{"event_id":"evt-1","checking_id":"chk-http-1","paid":true,"time":1700000200}`)))
	if err != nil {
		t.Fatalf("new webhook request: %v", err)
	}
	webhookReq.Header.Set("Content-Type", "application/json")
	webhookReq.Header.Set("X-FAP-Webhook-Secret", "hook-secret")
	webhookResp, err := http.DefaultClient.Do(webhookReq)
	if err != nil {
		t.Fatalf("do webhook request: %v", err)
	}
	if webhookResp.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected webhook status %d", webhookResp.StatusCode)
	}
	_ = webhookResp.Body.Close()

	statusResp := mustRequest(t, http.MethodGet, ts.URL+"/v1/boost/"+created.BoostID, nil)
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/boost/{id} status %d body=%s", statusResp.StatusCode, readBody(t, statusResp))
	}
	var status boostStatusResponse
	if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	_ = statusResp.Body.Close()
	if status.Status != "paid" || status.PaidAt == nil {
		t.Fatalf("expected paid boost after webhook, got %+v", status)
	}
}

func TestWebhookInvalidSignatureRejected(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              false,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}
	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
		DevMode:       false,
	})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/fap/webhook/lnbits", bytes.NewReader([]byte(`{"payment_hash":"ph-http-1"}`)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-FAP-Webhook-Secret", "wrong-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestBoostListFiltersPaginationAndNoBolt11Leak(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              true,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	api := NewWithOptions(svc, Options{
		WebhookSecret:      "hook-secret",
		DevMode:            true,
		ExposeBolt11InList: false,
	})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	seedBoost(t, repo, store.Boost{BoostID: "boost-c", AssetID: "asset-a", PayeeID: "payee-x", AmountMSat: 3000, Bolt11: "lnbc-c", Status: "paid", ExpiresAt: 9999999999, CreatedAt: 300, UpdatedAt: 300, IdempotencyKey: "idem-c"})
	seedBoost(t, repo, store.Boost{BoostID: "boost-b", AssetID: "asset-a", PayeeID: "payee-x", AmountMSat: 2000, Bolt11: "lnbc-b", Status: "pending", ExpiresAt: 9999999999, CreatedAt: 200, UpdatedAt: 200, IdempotencyKey: "idem-b"})
	seedBoost(t, repo, store.Boost{BoostID: "boost-a", AssetID: "asset-a", PayeeID: "payee-y", AmountMSat: 1000, Bolt11: "lnbc-a", Status: "paid", ExpiresAt: 9999999999, CreatedAt: 100, UpdatedAt: 100, IdempotencyKey: "idem-a"})

	values := neturl.Values{}
	values.Set("asset_id", "asset-a")
	values.Set("payee_id", "payee-x")
	values.Set("limit", "1")
	first := mustRequest(t, http.MethodGet, ts.URL+"/v1/boost?"+values.Encode(), nil)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/boost first page status=%d body=%s", first.StatusCode, readBody(t, first))
	}
	var firstResp boostListResponse
	if err := json.NewDecoder(first.Body).Decode(&firstResp); err != nil {
		t.Fatalf("decode first page response: %v", err)
	}
	_ = first.Body.Close()
	if len(firstResp.Items) != 1 {
		t.Fatalf("expected 1 item on first page, got %d", len(firstResp.Items))
	}
	if firstResp.Items[0].BoostID != "boost-c" {
		t.Fatalf("expected boost-c first, got %s", firstResp.Items[0].BoostID)
	}
	if firstResp.Items[0].Bolt11 != "" {
		t.Fatalf("bolt11 should be hidden when flag is false, got %q", firstResp.Items[0].Bolt11)
	}
	if firstResp.NextCursor == "" {
		t.Fatal("expected next_cursor on first page")
	}

	values.Set("cursor", firstResp.NextCursor)
	second := mustRequest(t, http.MethodGet, ts.URL+"/v1/boost?"+values.Encode(), nil)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/boost second page status=%d body=%s", second.StatusCode, readBody(t, second))
	}
	var secondResp boostListResponse
	if err := json.NewDecoder(second.Body).Decode(&secondResp); err != nil {
		t.Fatalf("decode second page response: %v", err)
	}
	_ = second.Body.Close()
	if len(secondResp.Items) != 1 {
		t.Fatalf("expected 1 item on second page, got %d", len(secondResp.Items))
	}
	if secondResp.Items[0].BoostID != "boost-b" {
		t.Fatalf("expected boost-b second, got %s", secondResp.Items[0].BoostID)
	}
}

func TestBoostListLimitClamping(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	f := &factory{adapterByPayee: make(map[string]*fakeAdapter)}
	svc, err := service.New(repo, f, service.Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              true,
	})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	api := NewWithOptions(svc, Options{
		WebhookSecret: "hook-secret",
		DevMode:       true,
	})
	ts := httptest.NewServer(api.Router())
	defer ts.Close()

	for i := 0; i < 110; i++ {
		seedBoost(t, repo, store.Boost{
			BoostID:        fmt.Sprintf("boost-%03d", i),
			AssetID:        "asset-limit",
			PayeeID:        "payee-limit",
			AmountMSat:     int64(1000 + i),
			Bolt11:         "lnbc-limit",
			Status:         "pending",
			ExpiresAt:      9999999999,
			CreatedAt:      int64(1000 + i),
			UpdatedAt:      int64(1000 + i),
			IdempotencyKey: fmt.Sprintf("idem-%03d", i),
		})
	}

	resp := mustRequest(t, http.MethodGet, ts.URL+"/v1/boost?asset_id=asset-limit&limit=999", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/boost clamped limit status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var listResp boostListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode clamped list response: %v", err)
	}
	_ = resp.Body.Close()
	if len(listResp.Items) != 100 {
		t.Fatalf("expected clamped list size 100, got %d", len(listResp.Items))
	}
}

func seedBoost(t *testing.T, repo *store.SQLiteRepository, value store.Boost) {
	t.Helper()
	if err := repo.CreateBoost(context.Background(), value); err != nil {
		t.Fatalf("seed boost %s: %v", value.BoostID, err)
	}
}

func mustRequest(t *testing.T, method string, url string, body []byte) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "<failed to read body>"
	}
	return string(body)
}

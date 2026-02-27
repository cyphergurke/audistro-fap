package lnbits

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type HTTPStatusError struct {
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("lnbits status %d", e.StatusCode)
}

type Invoice struct {
	Bolt11      string
	PaymentHash string
	CheckingID  string
	ExpiresAt   int64
}

type PaymentStatus struct {
	Paid   bool
	PaidAt *int64
}

type Client interface {
	CreateInvoice(ctx context.Context, baseURL string, invoiceKey string, amountMSat int64, memo string, expirySeconds int64) (Invoice, error)
	VerifyPayment(ctx context.Context, baseURL string, readKey string, paymentRef string) (PaymentStatus, error)
}

type HTTPClient struct {
	httpClient *http.Client
}

type createInvoiceRequest struct {
	Out    bool   `json:"out"`
	Amount int64  `json:"amount"`
	Memo   string `json:"memo"`
	Expiry int64  `json:"expiry"`
}

type createInvoiceResponse struct {
	PaymentRequest string          `json:"payment_request"`
	PaymentHash    string          `json:"payment_hash"`
	CheckingID     string          `json:"checking_id"`
	ExpiresAtRaw   json.RawMessage `json:"expires_at"`
	ExpiryRaw      json.RawMessage `json:"expiry"`
}

type paymentResponse struct {
	Paid    bool            `json:"paid"`
	Pending *bool           `json:"pending"`
	TimeRaw json.RawMessage `json:"time"`
}

func NewHTTPClient() *HTTPClient {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &HTTPClient{
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
	}
}

func (c *HTTPClient) CreateInvoice(ctx context.Context, baseURL string, invoiceKey string, amountMSat int64, memo string, expirySeconds int64) (Invoice, error) {
	if amountMSat <= 0 {
		return Invoice{}, fmt.Errorf("amount must be > 0")
	}
	if strings.TrimSpace(invoiceKey) == "" {
		return Invoice{}, fmt.Errorf("invoice key is required")
	}
	if expirySeconds <= 0 {
		expirySeconds = 900
	}
	payload, err := json.Marshal(createInvoiceRequest{
		Out:    false,
		Amount: amountMSat / 1000,
		Memo:   memo,
		Expiry: expirySeconds,
	})
	if err != nil {
		return Invoice{}, fmt.Errorf("marshal invoice request: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, baseURL, "/api/v1/payments", invoiceKey, payload)
	if err != nil {
		return Invoice{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Invoice{}, readStatusError(resp)
	}

	var out createInvoiceResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Invoice{}, fmt.Errorf("decode invoice response: %w", err)
	}
	if strings.TrimSpace(out.PaymentRequest) == "" || strings.TrimSpace(out.PaymentHash) == "" {
		return Invoice{}, errors.New("lnbits response missing payment_request/payment_hash")
	}

	expiresAt := time.Now().Unix() + expirySeconds
	if parsed, ok := parseFlexibleInt64JSON(out.ExpiresAtRaw); ok && parsed > 0 {
		expiresAt = parsed
	} else if parsed, ok := parseFlexibleInt64JSON(out.ExpiryRaw); ok && parsed > 0 {
		expiresAt = time.Now().Unix() + parsed
	}
	checkingID := strings.TrimSpace(out.CheckingID)
	if checkingID == "" {
		checkingID = strings.TrimSpace(out.PaymentHash)
	}
	return Invoice{
		Bolt11:      out.PaymentRequest,
		PaymentHash: strings.TrimSpace(out.PaymentHash),
		CheckingID:  checkingID,
		ExpiresAt:   expiresAt,
	}, nil
}

func (c *HTTPClient) VerifyPayment(ctx context.Context, baseURL string, readKey string, paymentRef string) (PaymentStatus, error) {
	if strings.TrimSpace(readKey) == "" {
		return PaymentStatus{}, fmt.Errorf("read key is required")
	}
	ref := strings.TrimSpace(paymentRef)
	if ref == "" {
		return PaymentStatus{}, fmt.Errorf("payment reference is required")
	}
	path := "/api/v1/payments/" + url.PathEscape(ref)
	resp, err := c.doRequest(ctx, http.MethodGet, baseURL, path, readKey, nil)
	if err != nil {
		return PaymentStatus{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return PaymentStatus{Paid: false}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PaymentStatus{}, readStatusError(resp)
	}

	var out paymentResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return PaymentStatus{}, fmt.Errorf("decode payment response: %w", err)
	}
	paid := out.Paid
	if out.Pending != nil {
		paid = !*out.Pending
	}
	if !paid {
		return PaymentStatus{Paid: false}, nil
	}
	settledAt, ok := parseFlexibleInt64JSON(out.TimeRaw)
	if !ok {
		settledAt = 0
	}
	if settledAt <= 0 {
		settledAt = time.Now().Unix()
	}
	return PaymentStatus{Paid: true, PaidAt: &settledAt}, nil
}

func (c *HTTPClient) doRequest(ctx context.Context, method string, baseURL string, path string, apiKey string, payload []byte) (*http.Response, error) {
	trimmedBase := strings.TrimSpace(baseURL)
	if trimmedBase == "" {
		return nil, fmt.Errorf("lnbits base url is required")
	}
	parsedBase, err := url.Parse(trimmedBase)
	if err != nil || (parsedBase.Scheme != "http" && parsedBase.Scheme != "https") {
		return nil, fmt.Errorf("lnbits base url is invalid")
	}
	fullURL, err := url.Parse(strings.TrimRight(trimmedBase, "/") + path)
	if err != nil {
		return nil, fmt.Errorf("build request url: %w", err)
	}

	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("X-Api-Key", apiKey)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lnbits request failed: %w", err)
	}
	return resp, nil
}

func readStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return &HTTPStatusError{
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(body)),
	}
}

func parseFlexibleInt64JSON(raw json.RawMessage) (int64, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return 0, false
	}

	var asInt int64
	if err := json.Unmarshal(raw, &asInt); err == nil {
		return asInt, true
	}
	var asNumber json.Number
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		if parsed, err := asNumber.Int64(); err == nil {
			return parsed, true
		}
	}
	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		return int64(asFloat), true
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		asString = strings.TrimSpace(asString)
		if asString == "" {
			return 0, false
		}
		if parsed, err := strconv.ParseInt(asString, 10, 64); err == nil {
			return parsed, true
		}
		if parsedFloat, err := strconv.ParseFloat(asString, 64); err == nil {
			return int64(parsedFloat), true
		}
	}
	return 0, false
}

var _ Client = (*HTTPClient)(nil)

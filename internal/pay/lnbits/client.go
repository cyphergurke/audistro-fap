package lnbits

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL        string
	invoiceAPIKey  string
	readonlyAPIKey string
	httpClient     *http.Client
	nowUnix        func() int64
	maxRetries     int
}

func NewClient(baseURL string, invoiceAPIKey string, readonlyAPIKey string) *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		invoiceAPIKey:  invoiceAPIKey,
		readonlyAPIKey: readonlyAPIKey,
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
		nowUnix:    func() int64 { return time.Now().Unix() },
		maxRetries: 2,
	}
}

func (c *Client) CreateInvoice(ctx context.Context, amountMSat int64, memo string, expirySeconds int64) (string, string, int64, error) {
	request := createInvoiceRequest{
		Out:    false,
		Amount: amountMSat,
		Unit:   "msat",
		Memo:   memo,
		Expiry: expirySeconds,
	}

	var response createInvoiceResponse
	if err := c.doJSONWithRetries(ctx, http.MethodPost, "/api/v1/payments", c.invoiceAPIKey, request, &response); err != nil {
		return "", "", 0, err
	}

	bolt11 := response.PaymentRequest
	if bolt11 == "" {
		bolt11 = response.Bolt11
	}
	if bolt11 == "" {
		return "", "", 0, fmt.Errorf("lnbits create invoice response missing bolt11")
	}
	if response.PaymentHash == "" {
		return "", "", 0, fmt.Errorf("lnbits create invoice response missing payment_hash")
	}

	expiresAt := c.nowUnix() + expirySeconds
	if response.ExpiresAt != nil && *response.ExpiresAt > 0 {
		expiresAt = *response.ExpiresAt
	} else if response.Expiry != nil && *response.Expiry > 0 {
		expiresAt = c.nowUnix() + *response.Expiry
	}

	return bolt11, response.PaymentHash, expiresAt, nil
}

func (c *Client) LookupPayment(ctx context.Context, paymentHash string) (bool, *int64, error) {
	if paymentHash == "" {
		return false, nil, fmt.Errorf("payment hash is required")
	}

	var response lookupPaymentResponse
	path := "/api/v1/payments/" + paymentHash
	if err := c.doJSONWithRetries(ctx, http.MethodGet, path, c.readonlyAPIKey, nil, &response); err != nil {
		return false, nil, err
	}

	var settledAt *int64
	if response.PaidAt != nil {
		settledAt = response.PaidAt
	} else if response.SettledAt != nil {
		settledAt = response.SettledAt
	}

	return response.Paid, settledAt, nil
}

func (c *Client) doJSONWithRetries(ctx context.Context, method string, path string, apiKey string, requestBody interface{}, responseBody interface{}) error {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		retryable, err := c.doJSON(ctx, method, path, apiKey, requestBody, responseBody)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable || attempt == c.maxRetries {
			break
		}

		backoff := time.Duration(attempt+1) * 100 * time.Millisecond
		select {
		case <-ctx.Done():
			return fmt.Errorf("request canceled during retry backoff: %w", ctx.Err())
		case <-time.After(backoff):
		}
	}

	return lastErr
}

func (c *Client) doJSON(ctx context.Context, method string, path string, apiKey string, requestBody interface{}, responseBody interface{}) (bool, error) {
	var bodyReader io.Reader
	if requestBody != nil {
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return false, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Api-Key", apiKey)
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return true, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return true, fmt.Errorf("lnbits server error status=%d body=%s", resp.StatusCode, string(body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return false, fmt.Errorf("lnbits request failed status=%d body=%s", resp.StatusCode, string(body))
	}

	if responseBody == nil {
		return false, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return false, fmt.Errorf("decode response body: %w", err)
	}

	return false, nil
}

package fapkit

import (
	"fmt"
	"strings"
)

type Config struct {
	HTTPAddr             string
	IssuerPrivKeyHex     string
	DBPath               string
	LNBitsBaseURL        string
	LNBitsInvoiceAPIKey  string
	LNBitsReadOnlyAPIKey string
	WebhookSecret        string
	TokenTTLSeconds      int64
	InvoiceExpirySeconds int64
	PriceMsatDefault     int64
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.IssuerPrivKeyHex) == "" {
		return fmt.Errorf("issuer private key hex is required")
	}
	if strings.TrimSpace(c.DBPath) == "" {
		return fmt.Errorf("db path is required")
	}
	if strings.TrimSpace(c.LNBitsBaseURL) == "" {
		return fmt.Errorf("lnbits base url is required")
	}
	if strings.TrimSpace(c.LNBitsInvoiceAPIKey) == "" {
		return fmt.Errorf("lnbits invoice api key is required")
	}
	if strings.TrimSpace(c.LNBitsReadOnlyAPIKey) == "" {
		return fmt.Errorf("lnbits readonly api key is required")
	}
	if strings.TrimSpace(c.WebhookSecret) == "" {
		return fmt.Errorf("webhook secret is required")
	}
	if c.TokenTTLSeconds <= 0 {
		return fmt.Errorf("token ttl seconds must be > 0")
	}
	if c.InvoiceExpirySeconds <= 0 {
		return fmt.Errorf("invoice expiry seconds must be > 0")
	}
	if c.PriceMsatDefault <= 0 {
		return fmt.Errorf("price msat default must be > 0")
	}
	return nil
}

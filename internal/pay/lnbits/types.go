package lnbits

type createInvoiceRequest struct {
	Out    bool   `json:"out"`
	Amount int64  `json:"amount"`
	Unit   string `json:"unit"`
	Memo   string `json:"memo,omitempty"`
	Expiry int64  `json:"expiry,omitempty"`
}

type createInvoiceResponse struct {
	PaymentRequest string `json:"payment_request"`
	Bolt11         string `json:"bolt11"`
	PaymentHash    string `json:"payment_hash"`
	ExpiresAt      *int64 `json:"expires_at"`
	Expiry         *int64 `json:"expiry"`
}

type lookupPaymentResponse struct {
	Paid        bool   `json:"paid"`
	PaymentHash string `json:"payment_hash"`
	PaidAt      *int64 `json:"paid_at"`
	SettledAt   *int64 `json:"settled_at"`
	Details     struct {
		Pending bool `json:"pending"`
	} `json:"details"`
}

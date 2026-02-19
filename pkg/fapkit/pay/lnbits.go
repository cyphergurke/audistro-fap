package pay

import "github.com/yourorg/fap/pkg/fapkit"

func NewLNBitsPayments(baseURL, invoiceKey, readKey string) (fapkit.Payments, error) {
	return fapkit.NewLNBitsPayments(baseURL, invoiceKey, readKey)
}

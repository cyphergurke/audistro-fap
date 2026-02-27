package pay

import (
	"context"
	"testing"

	"fap/internal/store"
)

type fakePayeeRepo struct {
	payee store.Payee
	calls int
}

func (r *fakePayeeRepo) CreatePayee(context.Context, store.Payee) error { return nil }
func (r *fakePayeeRepo) GetByID(_ context.Context, payeeID string) (store.Payee, error) {
	r.calls++
	if payeeID != r.payee.PayeeID {
		return store.Payee{}, store.ErrNotFound
	}
	return r.payee, nil
}

type fakeAdapter struct{}

func (fakeAdapter) CreateInvoice(context.Context, int64, string, int64) (string, string, string, int64, error) {
	return "", "", "", 0, nil
}
func (fakeAdapter) IsSettled(context.Context, string) (bool, *int64, error) { return false, nil, nil }

func TestCachedFactoryCachesAdapter(t *testing.T) {
	repo := &fakePayeeRepo{payee: store.Payee{PayeeID: "p1", LNBitsInvoiceKeyEnc: []byte("inv"), LNBitsReadKeyEnc: []byte("read")}}
	decryptCalls := 0
	builderCalls := 0
	f := NewCachedFactory(
		make([]byte, 32),
		repo,
		func(_ []byte, blob []byte) ([]byte, error) {
			decryptCalls++
			return blob, nil
		},
		func(_ store.Payee, _ string, _ string) PaymentAdapter {
			builderCalls++
			return fakeAdapter{}
		},
	)

	if _, err := f.ForPayee(context.Background(), "p1"); err != nil {
		t.Fatalf("first ForPayee: %v", err)
	}
	if _, err := f.ForPayee(context.Background(), "p1"); err != nil {
		t.Fatalf("second ForPayee: %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("expected 1 repo call, got %d", repo.calls)
	}
	if decryptCalls != 2 {
		t.Fatalf("expected decrypt twice (2 keys), got %d", decryptCalls)
	}
	if builderCalls != 1 {
		t.Fatalf("expected builder once, got %d", builderCalls)
	}
}

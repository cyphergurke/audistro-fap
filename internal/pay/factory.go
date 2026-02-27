package pay

import (
	"context"
	"sync"
	"time"

	"audistro-fap/internal/store"
)

type PayeeAdapterFactory interface {
	ForPayee(ctx context.Context, payeeID string) (PaymentAdapter, error)
}

type AdapterBuilder func(payee store.Payee, invoiceKey string, readKey string) PaymentAdapter

type cacheEntry struct {
	adapter   PaymentAdapter
	expiresAt int64
}

type CachedFactory struct {
	masterKey []byte
	payees    store.PayeeRepository
	decrypt   func(masterKey []byte, blob []byte) ([]byte, error)
	build     AdapterBuilder
	ttl       int64

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

func NewCachedFactory(masterKey []byte, payees store.PayeeRepository, decryptFn func(masterKey []byte, blob []byte) ([]byte, error), build AdapterBuilder) *CachedFactory {
	return &CachedFactory{
		masterKey: masterKey,
		payees:    payees,
		decrypt:   decryptFn,
		build:     build,
		ttl:       int64((5 * time.Minute).Seconds()),
		cache:     make(map[string]cacheEntry),
	}
}

func (f *CachedFactory) ForPayee(ctx context.Context, payeeID string) (PaymentAdapter, error) {
	now := time.Now().Unix()
	f.mu.RLock()
	cached, ok := f.cache[payeeID]
	f.mu.RUnlock()
	if ok && now < cached.expiresAt {
		return cached.adapter, nil
	}

	payee, err := f.payees.GetByID(ctx, payeeID)
	if err != nil {
		return nil, err
	}
	invoiceKey, err := f.decrypt(f.masterKey, payee.LNBitsInvoiceKeyEnc)
	if err != nil {
		return nil, err
	}
	readKey, err := f.decrypt(f.masterKey, payee.LNBitsReadKeyEnc)
	if err != nil {
		return nil, err
	}

	adapter := f.build(payee, string(invoiceKey), string(readKey))
	f.mu.Lock()
	f.cache[payeeID] = cacheEntry{adapter: adapter, expiresAt: now + f.ttl}
	f.mu.Unlock()

	return adapter, nil
}

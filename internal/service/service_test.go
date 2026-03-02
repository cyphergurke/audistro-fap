package service

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"audistro-fap/internal/crypto/secretbox"
	"audistro-fap/internal/pay"
	"audistro-fap/internal/store"
)

type fakeAdapter struct {
	bolt11             string
	paymentHash        string
	checkingID         string
	expiresAt          int64
	settled            bool
	settledAt          int64
	calls              int
	lastSettledRef     string
	expectedSettledRef string
}

func (a *fakeAdapter) CreateInvoice(_ context.Context, _ int64, _ string, _ int64) (string, string, string, int64, error) {
	a.calls++
	return a.bolt11, a.paymentHash, a.checkingID, a.expiresAt, nil
}

func (a *fakeAdapter) IsSettled(_ context.Context, paymentRef string) (bool, *int64, error) {
	a.lastSettledRef = paymentRef
	if a.expectedSettledRef != "" && paymentRef != a.expectedSettledRef {
		return false, nil, nil
	}
	if !a.settled {
		return false, nil, nil
	}
	t := a.settledAt
	return true, &t, nil
}

type fakeFactory struct {
	adapters map[string]*fakeAdapter
}

func (f *fakeFactory) ForPayee(_ context.Context, payeeID string) (pay.PaymentAdapter, error) {
	adapter, ok := f.adapters[payeeID]
	if !ok {
		return nil, errors.New("missing adapter")
	}
	return adapter, nil
}

func TestCreatePayeeStoresEncryptedKeys(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()

	created, err := svc.CreatePayee(ctx, CreatePayeeRequest{
		DisplayName:      "Artist",
		LNBitsBaseURL:    "http://lnbits.local",
		LNBitsInvoiceKey: "invoice-secret",
		LNBitsReadKey:    "read-secret",
	}, secretbox.Encrypt, master)
	if err != nil {
		t.Fatalf("CreatePayee: %v", err)
	}
	stored, err := repo.GetByID(ctx, created.PayeeID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if bytes.Equal(stored.LNBitsInvoiceKeyEnc, []byte("invoice-secret")) {
		t.Fatal("invoice key not encrypted")
	}
	if bytes.Equal(stored.LNBitsReadKeyEnc, []byte("read-secret")) {
		t.Fatal("read key not encrypted")
	}
}

func TestChallengeUsesCorrectPayeeAdapter(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()

	p1, _ := svc.CreatePayee(ctx, CreatePayeeRequest{DisplayName: "A", LNBitsBaseURL: "u1", LNBitsInvoiceKey: "i1", LNBitsReadKey: "r1"}, secretbox.Encrypt, master)
	p2, _ := svc.CreatePayee(ctx, CreatePayeeRequest{DisplayName: "B", LNBitsBaseURL: "u2", LNBitsInvoiceKey: "i2", LNBitsReadKey: "r2"}, secretbox.Encrypt, master)

	if _, err := svc.CreateAsset(ctx, CreateAssetRequest{AssetID: "asset-1", PayeeID: p2.PayeeID, Title: "Track", PriceMSat: 1000}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	factory := svc.adapters.(*fakeFactory)
	factory.adapters[p1.PayeeID] = &fakeAdapter{bolt11: "ln1", paymentHash: "ph1", expiresAt: 1000}
	factory.adapters[p2.PayeeID] = &fakeAdapter{bolt11: "ln2", paymentHash: "ph2", expiresAt: 2000}

	ch, err := svc.CreateChallenge(ctx, CreateChallengeRequest{AssetID: "asset-1", Subject: "sub1"})
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}
	if ch.PaymentHash != "ph2" {
		t.Fatalf("wrong adapter used: got %s", ch.PaymentHash)
	}
	if factory.adapters[p1.PayeeID].calls != 0 {
		t.Fatalf("unexpected calls on payee1 adapter: %d", factory.adapters[p1.PayeeID].calls)
	}
	if factory.adapters[p2.PayeeID].calls != 1 {
		t.Fatalf("expected one call on payee2 adapter, got %d", factory.adapters[p2.PayeeID].calls)
	}
}

func TestWebhookAndMintIdempotentAndSubjectBound(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()

	p, _ := svc.CreatePayee(ctx, CreatePayeeRequest{DisplayName: "A", LNBitsBaseURL: "u", LNBitsInvoiceKey: "i", LNBitsReadKey: "r"}, secretbox.Encrypt, master)
	_, _ = svc.CreateAsset(ctx, CreateAssetRequest{AssetID: "asset-2", PayeeID: p.PayeeID, Title: "Track", PriceMSat: 1200})

	factory := svc.adapters.(*fakeFactory)
	factory.adapters[p.PayeeID] = &fakeAdapter{bolt11: "ln", paymentHash: "ph-2", expiresAt: 999999, settled: true, settledAt: 777}

	ch, err := svc.CreateChallenge(ctx, CreateChallengeRequest{AssetID: "asset-2", Subject: "subA"})
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}

	if err := svc.HandleWebhook(ctx, ch.PaymentHash, 1000); err != nil {
		t.Fatalf("HandleWebhook #1: %v", err)
	}
	if err := svc.HandleWebhook(ctx, ch.PaymentHash, 2000); err != nil {
		t.Fatalf("HandleWebhook #2: %v", err)
	}
	intent, err := repo.GetIntentByPaymentHash(ctx, ch.PaymentHash)
	if err != nil {
		t.Fatalf("GetIntentByPaymentHash: %v", err)
	}
	if intent.SettledAt == nil || *intent.SettledAt != 777 {
		t.Fatalf("unexpected settled_at: %+v", intent.SettledAt)
	}

	mint1, err := svc.MintToken(ctx, ch.IntentID, "subA", 800)
	if err != nil {
		t.Fatalf("MintToken #1: %v", err)
	}
	mint2, err := svc.MintToken(ctx, ch.IntentID, "subA", 850)
	if err != nil {
		t.Fatalf("MintToken #2: %v", err)
	}
	if mint1.Token != mint2.Token {
		t.Fatal("mint token should be idempotent")
	}
	if _, err := svc.MintToken(ctx, ch.IntentID, "subB", 900); !errors.Is(err, ErrSubjectMismatch) {
		t.Fatalf("expected ErrSubjectMismatch, got %v", err)
	}
}

func TestCatalogChallengeDoesNotRequireAssetAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()

	p, err := svc.CreatePayee(ctx, CreatePayeeRequest{
		DisplayName:      "Catalog Artist",
		LNBitsBaseURL:    "http://lnbits.local",
		LNBitsInvoiceKey: "invoice",
		LNBitsReadKey:    "read",
	}, secretbox.Encrypt, master)
	if err != nil {
		t.Fatalf("CreatePayee: %v", err)
	}

	factory := svc.adapters.(*fakeFactory)
	factory.adapters[p.PayeeID] = &fakeAdapter{
		bolt11:      "lnbc-catalog",
		paymentHash: "ph-catalog-1",
		checkingID:  "chk-catalog-1",
		expiresAt:   1800000000,
	}

	req := CreateChallengeRequest{
		AssetID:        "asset_catalog_1",
		PayeeID:        p.PayeeID,
		AmountMSat:     2_000,
		Memo:           "catalog access",
		IdempotencyKey: "idem-catalog-1",
	}
	first, err := svc.CreateChallenge(ctx, req)
	if err != nil {
		t.Fatalf("CreateChallenge #1: %v", err)
	}
	second, err := svc.CreateChallenge(ctx, req)
	if err != nil {
		t.Fatalf("CreateChallenge #2: %v", err)
	}
	if first.ChallengeID != second.ChallengeID {
		t.Fatalf("expected idempotent challenge id, got %s vs %s", first.ChallengeID, second.ChallengeID)
	}
	if first.AssetID != "asset_catalog_1" || first.PayeeID != p.PayeeID {
		t.Fatalf("unexpected challenge identity: %+v", first)
	}
	if factory.adapters[p.PayeeID].calls != 1 {
		t.Fatalf("expected single invoice call for idempotent challenge, got %d", factory.adapters[p.PayeeID].calls)
	}
}

func TestCatalogChallengeMintTokenPendingThenPaid(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()

	p, err := svc.CreatePayee(ctx, CreatePayeeRequest{
		DisplayName:      "Catalog Artist",
		LNBitsBaseURL:    "http://lnbits.local",
		LNBitsInvoiceKey: "invoice",
		LNBitsReadKey:    "read",
	}, secretbox.Encrypt, master)
	if err != nil {
		t.Fatalf("CreatePayee: %v", err)
	}

	factory := svc.adapters.(*fakeFactory)
	factory.adapters[p.PayeeID] = &fakeAdapter{
		bolt11:             "lnbc-catalog-pay",
		paymentHash:        "ph-catalog-pay-1",
		checkingID:         "chk-catalog-pay-1",
		expiresAt:          1900000000,
		settled:            false,
		settledAt:          0,
		expectedSettledRef: "chk-catalog-pay-1",
	}

	ch, err := svc.CreateChallenge(ctx, CreateChallengeRequest{
		AssetID:    "asset_catalog_2",
		PayeeID:    p.PayeeID,
		AmountMSat: 3_000,
		Memo:       "catalog flow",
	})
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}

	if _, err := svc.MintToken(ctx, ch.ChallengeID, "", 1700000100); !errors.Is(err, ErrNotSettled) {
		t.Fatalf("expected ErrNotSettled before payment, got %v", err)
	}
	factory.adapters[p.PayeeID].settled = true
	factory.adapters[p.PayeeID].settledAt = 1700000200

	mint1, err := svc.MintToken(ctx, ch.ChallengeID, "", 1700000210)
	if err != nil {
		t.Fatalf("MintToken #1: %v", err)
	}
	if mint1.ResourceID != "hls:key:asset_catalog_2" {
		t.Fatalf("unexpected resource id: %s", mint1.ResourceID)
	}
	mint2, err := svc.MintToken(ctx, ch.ChallengeID, "", 1700000220)
	if err != nil {
		t.Fatalf("MintToken #2: %v", err)
	}
	if mint1.Token != mint2.Token {
		t.Fatal("expected idempotent token mint for paid challenge")
	}
	if factory.adapters[p.PayeeID].lastSettledRef != "chk-catalog-pay-1" {
		t.Fatalf("expected settlement check with checking_id, got %q", factory.adapters[p.PayeeID].lastSettledRef)
	}
	storedChallenge, err := repo.GetChallengeByID(ctx, ch.ChallengeID)
	if err != nil {
		t.Fatalf("GetChallengeByID: %v", err)
	}
	if storedChallenge.Status != "paid" || storedChallenge.PaidAt == nil || *storedChallenge.PaidAt != 1700000200 {
		t.Fatalf("expected paid challenge after token polling settlement, got %+v", storedChallenge)
	}
}

func TestMintTokenReissuesAfterExpiryForPaidChallenge(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()
	svc.cfg.TokenTTLSeconds = 5

	p, err := svc.CreatePayee(ctx, CreatePayeeRequest{
		DisplayName:      "Token Refresh Artist",
		LNBitsBaseURL:    "http://lnbits.local",
		LNBitsInvoiceKey: "invoice",
		LNBitsReadKey:    "read",
	}, secretbox.Encrypt, master)
	if err != nil {
		t.Fatalf("CreatePayee: %v", err)
	}

	factory := svc.adapters.(*fakeFactory)
	factory.adapters[p.PayeeID] = &fakeAdapter{
		bolt11:      "lnbc-refresh",
		paymentHash: "ph-refresh-1",
		checkingID:  "chk-refresh-1",
		expiresAt:   1900000000,
	}

	ch, err := svc.CreateChallenge(ctx, CreateChallengeRequest{
		DeviceID:   "device_refresh_1",
		AssetID:    "asset_refresh_1",
		PayeeID:    p.PayeeID,
		AmountMSat: 2_000,
	})
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}

	paidAt := int64(1700000300)
	if err := svc.HandleLNBitsWebhook(ctx, LNBitsWebhookEvent{
		EventID:     "evt-refresh-1",
		CheckingID:  ch.CheckingID,
		PaymentHash: ch.PaymentHash,
		AmountMSat:  ch.AmountMSat,
		Paid:        true,
		PaidAt:      &paidAt,
	}, 0); err != nil {
		t.Fatalf("HandleLNBitsWebhook: %v", err)
	}

	first, err := svc.MintToken(ctx, ch.ChallengeID, "device_refresh_1", 1700000310)
	if err != nil {
		t.Fatalf("MintToken #1: %v", err)
	}
	second, err := svc.MintToken(ctx, ch.ChallengeID, "device_refresh_1", 1700000312)
	if err != nil {
		t.Fatalf("MintToken #2: %v", err)
	}
	if first.Token != second.Token {
		t.Fatal("expected same token while existing token is still valid")
	}
	third, err := svc.MintToken(ctx, ch.ChallengeID, "device_refresh_1", 1700000316)
	if err != nil {
		t.Fatalf("MintToken #3: %v", err)
	}
	if third.Token == first.Token {
		t.Fatal("expected a new token after previous token expiry")
	}
}

func TestWebhookReplayByEventIDIsIgnored(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()

	p, err := svc.CreatePayee(ctx, CreatePayeeRequest{
		DisplayName:      "Replay Artist",
		LNBitsBaseURL:    "http://lnbits.local",
		LNBitsInvoiceKey: "invoice",
		LNBitsReadKey:    "read",
	}, secretbox.Encrypt, master)
	if err != nil {
		t.Fatalf("CreatePayee: %v", err)
	}

	factory := svc.adapters.(*fakeFactory)
	adapter := &fakeAdapter{
		bolt11:      "lnbc-replay-1",
		paymentHash: "ph-replay-1",
		checkingID:  "chk-replay-1",
		expiresAt:   1900000000,
	}
	factory.adapters[p.PayeeID] = adapter

	firstChallenge, err := svc.CreateChallenge(ctx, CreateChallengeRequest{
		DeviceID:       "device_replay_1",
		AssetID:        "asset_replay_1",
		PayeeID:        p.PayeeID,
		AmountMSat:     1_500,
		IdempotencyKey: "idem-replay-1",
	})
	if err != nil {
		t.Fatalf("CreateChallenge #1: %v", err)
	}

	adapter.bolt11 = "lnbc-replay-2"
	adapter.paymentHash = "ph-replay-2"
	adapter.checkingID = "chk-replay-2"

	secondChallenge, err := svc.CreateChallenge(ctx, CreateChallengeRequest{
		DeviceID:       "device_replay_2",
		AssetID:        "asset_replay_2",
		PayeeID:        p.PayeeID,
		AmountMSat:     1_700,
		IdempotencyKey: "idem-replay-2",
	})
	if err != nil {
		t.Fatalf("CreateChallenge #2: %v", err)
	}

	firstPaidAt := int64(1700000400)
	if err := svc.HandleLNBitsWebhook(ctx, LNBitsWebhookEvent{
		EventID:     "evt-replay-1",
		CheckingID:  firstChallenge.CheckingID,
		PaymentHash: firstChallenge.PaymentHash,
		AmountMSat:  firstChallenge.AmountMSat,
		Paid:        true,
		PaidAt:      &firstPaidAt,
	}, 0); err != nil {
		t.Fatalf("HandleLNBitsWebhook first event: %v", err)
	}

	duplicatePaidAt := int64(1700000500)
	if err := svc.HandleLNBitsWebhook(ctx, LNBitsWebhookEvent{
		EventID:     "evt-replay-1",
		CheckingID:  secondChallenge.CheckingID,
		PaymentHash: secondChallenge.PaymentHash,
		AmountMSat:  secondChallenge.AmountMSat,
		Paid:        true,
		PaidAt:      &duplicatePaidAt,
	}, 0); err != nil {
		t.Fatalf("HandleLNBitsWebhook duplicate event: %v", err)
	}

	firstStored, err := repo.GetChallengeByID(ctx, firstChallenge.ChallengeID)
	if err != nil {
		t.Fatalf("GetChallengeByID first: %v", err)
	}
	if firstStored.Status != "paid" {
		t.Fatalf("expected first challenge paid, got %s", firstStored.Status)
	}

	secondStored, err := repo.GetChallengeByID(ctx, secondChallenge.ChallengeID)
	if err != nil {
		t.Fatalf("GetChallengeByID second: %v", err)
	}
	if secondStored.Status != "pending" {
		t.Fatalf("expected second challenge to remain pending due webhook replay dedupe, got %s", secondStored.Status)
	}
}

func TestHandleLNBitsWebhookPrunesRetentionWindow(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()

	svc.cfg.WebhookEventRetentionSeconds = 10
	svc.cfg.WebhookEventPruneIntervalSeconds = 1
	svc.nextWebhookPruneAt = 0

	if _, err := repo.RecordWebhookEvent(ctx, store.WebhookEvent{
		EventKey:   "evt-old-retention",
		ReceivedAt: 100,
	}); err != nil {
		t.Fatalf("seed old webhook event: %v", err)
	}

	p, err := svc.CreatePayee(ctx, CreatePayeeRequest{
		DisplayName:      "Retention Artist",
		LNBitsBaseURL:    "http://lnbits.local",
		LNBitsInvoiceKey: "invoice",
		LNBitsReadKey:    "read",
	}, secretbox.Encrypt, master)
	if err != nil {
		t.Fatalf("CreatePayee: %v", err)
	}
	factory := svc.adapters.(*fakeFactory)
	factory.adapters[p.PayeeID] = &fakeAdapter{
		bolt11:      "lnbc-retention",
		paymentHash: "ph-retention",
		checkingID:  "chk-retention",
		expiresAt:   1900000000,
	}
	ch, err := svc.CreateChallenge(ctx, CreateChallengeRequest{
		DeviceID:   "device_retention_1",
		AssetID:    "asset_retention_1",
		PayeeID:    p.PayeeID,
		AmountMSat: 1000,
	})
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}

	now := int64(200)
	if err := svc.HandleLNBitsWebhook(ctx, LNBitsWebhookEvent{
		EventID:     "evt-new-retention",
		CheckingID:  ch.CheckingID,
		PaymentHash: ch.PaymentHash,
		AmountMSat:  ch.AmountMSat,
		Paid:        true,
	}, now); err != nil {
		t.Fatalf("HandleLNBitsWebhook: %v", err)
	}

	var oldCount int
	if err := repo.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_events WHERE event_key = ?`, "evt-old-retention").Scan(&oldCount); err != nil {
		t.Fatalf("query old event count: %v", err)
	}
	if oldCount != 0 {
		t.Fatalf("expected old webhook event to be pruned, count=%d", oldCount)
	}
}

func TestBuildWebhookEventKeyTupleUsesBothRefs(t *testing.T) {
	ts := int64(1_700_000_123)
	key1 := buildWebhookEventKey("", "chk-1", "ph-1", 1000, ts)
	key2 := buildWebhookEventKey("", "chk-1", "ph-2", 1000, ts)
	if key1 == "" || key2 == "" {
		t.Fatal("expected non-empty tuple keys")
	}
	if key1 == key2 {
		t.Fatal("expected tuple key to change when payment hash changes")
	}

	same1 := buildWebhookEventKey("", "chk-1", "ph-1", 1000, ts)
	same2 := buildWebhookEventKey("", "chk-1", "ph-1", 1000, ts+120)
	if same1 != same2 {
		t.Fatal("expected same tuple key inside dedupe time window")
	}
}

func TestBootstrapDeviceCreateAndTouch(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newService(t)
	defer repo.Close()

	created, err := svc.BootstrapDevice(ctx, "", 1700000000)
	if err != nil {
		t.Fatalf("BootstrapDevice create: %v", err)
	}
	if created.DeviceID == "" {
		t.Fatal("expected device id")
	}
	stored, err := repo.GetDeviceByID(ctx, created.DeviceID)
	if err != nil {
		t.Fatalf("GetDeviceByID: %v", err)
	}
	if stored.LastSeenAt != 1700000000 {
		t.Fatalf("expected initial last_seen_at=1700000000, got %d", stored.LastSeenAt)
	}

	touched, err := svc.BootstrapDevice(ctx, created.DeviceID, 1700000100)
	if err != nil {
		t.Fatalf("BootstrapDevice touch: %v", err)
	}
	if touched.DeviceID != created.DeviceID {
		t.Fatalf("expected same device id, got %s", touched.DeviceID)
	}
	if touched.LastSeenAt != 1700000100 {
		t.Fatalf("expected touched last_seen_at=1700000100, got %d", touched.LastSeenAt)
	}
}

func TestChallengeDeviceBindingAndGrantLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()
	svc.cfg.AccessMinutesPerPay = 1

	p, err := svc.CreatePayee(ctx, CreatePayeeRequest{
		DisplayName:      "Catalog Artist",
		LNBitsBaseURL:    "http://lnbits.local",
		LNBitsInvoiceKey: "invoice",
		LNBitsReadKey:    "read",
	}, secretbox.Encrypt, master)
	if err != nil {
		t.Fatalf("CreatePayee: %v", err)
	}

	factory := svc.adapters.(*fakeFactory)
	factory.adapters[p.PayeeID] = &fakeAdapter{
		bolt11:      "lnbc-catalog-pay",
		paymentHash: "ph-catalog-pay-device",
		checkingID:  "chk-catalog-pay-device",
		expiresAt:   1900000000,
	}

	ch, err := svc.CreateChallenge(ctx, CreateChallengeRequest{
		DeviceID:       "device_123",
		AssetID:        "asset_catalog_device_1",
		PayeeID:        p.PayeeID,
		AmountMSat:     3_000,
		Memo:           "catalog flow",
		IdempotencyKey: "idem-device-1",
	})
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}
	if ch.DeviceID != "device_123" {
		t.Fatalf("expected challenge device_id device_123, got %s", ch.DeviceID)
	}

	paidAt := int64(1700000200)
	if err := svc.HandleLNBitsWebhook(ctx, LNBitsWebhookEvent{
		PaymentHash: ch.PaymentHash,
		CheckingID:  ch.CheckingID,
		Paid:        true,
		PaidAt:      &paidAt,
	}, 0); err != nil {
		t.Fatalf("HandleLNBitsWebhook: %v", err)
	}

	if _, err := svc.MintToken(ctx, ch.ChallengeID, "", paidAt+1); !errors.Is(err, ErrDeviceRequired) {
		t.Fatalf("expected ErrDeviceRequired, got %v", err)
	}
	if _, err := svc.MintToken(ctx, ch.ChallengeID, "device_other", paidAt+1); !errors.Is(err, ErrDeviceMismatch) {
		t.Fatalf("expected ErrDeviceMismatch, got %v", err)
	}
	minted, err := svc.MintToken(ctx, ch.ChallengeID, "device_123", paidAt+1)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if minted.Token == "" {
		t.Fatal("expected token")
	}

	grant, err := repo.GetAccessGrantByChallengeID(ctx, ch.ChallengeID)
	if err != nil {
		t.Fatalf("GetAccessGrantByChallengeID: %v", err)
	}
	if grant.Status != "active" || grant.ValidFrom != nil || grant.ValidUntil != nil {
		t.Fatalf("unexpected grant after payment: %+v", grant)
	}

	if err := svc.AuthorizeKeyAccess(ctx, "device_123", "asset_catalog_device_1", 1700000300); err != nil {
		t.Fatalf("AuthorizeKeyAccess activation: %v", err)
	}
	grant, err = repo.GetAccessGrantByChallengeID(ctx, ch.ChallengeID)
	if err != nil {
		t.Fatalf("GetAccessGrantByChallengeID after activation: %v", err)
	}
	if grant.ValidFrom == nil || grant.ValidUntil == nil {
		t.Fatalf("expected activated grant timestamps, got %+v", grant)
	}
	if *grant.ValidUntil != *grant.ValidFrom+60 {
		t.Fatalf("expected 60s window for 1 minute grant, got from=%d until=%d", *grant.ValidFrom, *grant.ValidUntil)
	}
	if err := svc.AuthorizeKeyAccess(ctx, "device_123", "asset_catalog_device_1", *grant.ValidUntil+1); !errors.Is(err, ErrGrantExpired) {
		t.Fatalf("expected ErrGrantExpired, got %v", err)
	}
}

func TestChallengeCreatesPendingLedgerEntry(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()

	p, err := svc.CreatePayee(ctx, CreatePayeeRequest{
		DisplayName:      "Ledger Artist",
		LNBitsBaseURL:    "http://lnbits.local",
		LNBitsInvoiceKey: "invoice",
		LNBitsReadKey:    "read",
	}, secretbox.Encrypt, master)
	if err != nil {
		t.Fatalf("CreatePayee: %v", err)
	}
	svc.adapters.(*fakeFactory).adapters[p.PayeeID] = &fakeAdapter{
		bolt11:      "lnbc-ledger-access-1",
		paymentHash: "ph-ledger-access-1",
		checkingID:  "chk-ledger-access-1",
		expiresAt:   1900000000,
	}

	ch, err := svc.CreateChallenge(ctx, CreateChallengeRequest{
		DeviceID:       "device_ledger_1",
		AssetID:        "asset_ledger_1",
		PayeeID:        p.PayeeID,
		AmountMSat:     2500,
		IdempotencyKey: "idem-ledger-access-1",
	})
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}

	list, err := svc.ListLedgerEntriesForDevice(ctx, ListLedgerEntriesRequest{DeviceID: "device_ledger_1", Kind: "access"})
	if err != nil {
		t.Fatalf("ListLedgerEntriesForDevice: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 ledger entry, got %d", len(list.Items))
	}
	item := list.Items[0]
	if item.RelatedID != ch.ChallengeID || item.Status != "pending" || item.Kind != "access" {
		t.Fatalf("unexpected ledger entry: %+v", item)
	}
}

func TestWebhookMarksChallengeLedgerPaidIdempotent(t *testing.T) {
	ctx := context.Background()
	svc, repo, master := newService(t)
	defer repo.Close()

	p, err := svc.CreatePayee(ctx, CreatePayeeRequest{
		DisplayName:      "Ledger Artist",
		LNBitsBaseURL:    "http://lnbits.local",
		LNBitsInvoiceKey: "invoice",
		LNBitsReadKey:    "read",
	}, secretbox.Encrypt, master)
	if err != nil {
		t.Fatalf("CreatePayee: %v", err)
	}
	svc.adapters.(*fakeFactory).adapters[p.PayeeID] = &fakeAdapter{
		bolt11:      "lnbc-ledger-access-2",
		paymentHash: "ph-ledger-access-2",
		checkingID:  "chk-ledger-access-2",
		expiresAt:   1900000000,
	}
	ch, err := svc.CreateChallenge(ctx, CreateChallengeRequest{
		DeviceID:       "device_ledger_2",
		AssetID:        "asset_ledger_2",
		PayeeID:        p.PayeeID,
		AmountMSat:     3300,
		IdempotencyKey: "idem-ledger-access-2",
	})
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}

	paidAt := int64(1700000600)
	event := LNBitsWebhookEvent{
		EventID:     "evt-ledger-access-2",
		CheckingID:  ch.CheckingID,
		PaymentHash: ch.PaymentHash,
		AmountMSat:  ch.AmountMSat,
		Paid:        true,
		PaidAt:      &paidAt,
	}
	if err := svc.HandleLNBitsWebhook(ctx, event, 0); err != nil {
		t.Fatalf("HandleLNBitsWebhook first: %v", err)
	}
	if err := svc.HandleLNBitsWebhook(ctx, event, 0); err != nil {
		t.Fatalf("HandleLNBitsWebhook duplicate: %v", err)
	}

	list, err := svc.ListLedgerEntriesForDevice(ctx, ListLedgerEntriesRequest{DeviceID: "device_ledger_2", Kind: "access"})
	if err != nil {
		t.Fatalf("ListLedgerEntriesForDevice: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 ledger entry after replay, got %d", len(list.Items))
	}
	item := list.Items[0]
	if item.Status != "paid" || item.PaidAt == nil || *item.PaidAt != paidAt {
		t.Fatalf("unexpected paid ledger entry: %+v", item)
	}
}

func TestBoostCreatesPendingLedgerEntry(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newService(t)
	defer repo.Close()

	boost, err := svc.CreateBoost(ctx, CreateBoostRequest{
		DeviceID:       "device_ledger_boost_1",
		AssetID:        "asset_ledger_boost_1",
		PayeeID:        "fap_payee_ledger_boost_1",
		AmountMSat:     1000000,
		IdempotencyKey: "idem-ledger-boost-1",
	})
	if err != nil {
		t.Fatalf("CreateBoost: %v", err)
	}
	list, err := svc.ListLedgerEntriesForDevice(ctx, ListLedgerEntriesRequest{DeviceID: "device_ledger_boost_1", Kind: "boost"})
	if err != nil {
		t.Fatalf("ListLedgerEntriesForDevice: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 boost ledger entry, got %d", len(list.Items))
	}
	item := list.Items[0]
	if item.RelatedID != boost.BoostID || item.Status != "pending" || item.Kind != "boost" {
		t.Fatalf("unexpected boost ledger entry: %+v", item)
	}
}

func TestWebhookMarksBoostLedgerPaid(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newService(t)
	defer repo.Close()

	svc.cfg.DevMode = false
	svc.adapters.(*fakeFactory).adapters["fap_payee_ledger_boost_2"] = &fakeAdapter{
		bolt11:      "lnbc-ledger-boost-2",
		paymentHash: "ph-ledger-boost-2",
		checkingID:  "chk-ledger-boost-2",
		expiresAt:   1900000000,
	}
	boost, err := svc.CreateBoost(ctx, CreateBoostRequest{
		DeviceID:       "device_ledger_boost_2",
		AssetID:        "asset_ledger_boost_2",
		PayeeID:        "fap_payee_ledger_boost_2",
		AmountMSat:     1000000,
		IdempotencyKey: "idem-ledger-boost-2",
	})
	if err != nil {
		t.Fatalf("CreateBoost: %v", err)
	}

	paidAt := int64(1700000700)
	event := LNBitsWebhookEvent{
		EventID:     "evt-ledger-boost-2",
		CheckingID:  boost.LNBitsCheckingID,
		PaymentHash: boost.LNBitsPaymentHash,
		AmountMSat:  boost.AmountMSat,
		Paid:        true,
		PaidAt:      &paidAt,
	}
	if err := svc.HandleLNBitsWebhook(ctx, event, 0); err != nil {
		t.Fatalf("HandleLNBitsWebhook first: %v", err)
	}
	if err := svc.HandleLNBitsWebhook(ctx, event, 0); err != nil {
		t.Fatalf("HandleLNBitsWebhook duplicate: %v", err)
	}

	list, err := svc.ListLedgerEntriesForDevice(ctx, ListLedgerEntriesRequest{DeviceID: "device_ledger_boost_2", Kind: "boost"})
	if err != nil {
		t.Fatalf("ListLedgerEntriesForDevice: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 boost ledger entry, got %d", len(list.Items))
	}
	item := list.Items[0]
	if item.Status != "paid" || item.PaidAt == nil || *item.PaidAt != paidAt {
		t.Fatalf("unexpected paid boost ledger entry: %+v", item)
	}
}

func TestBoostIdempotencyAndStatusTransitions(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newService(t)
	defer repo.Close()

	now := int64(1700000000)
	svc.nowUnix = func() int64 { return now }
	svc.cfg.InvoiceExpirySeconds = 10

	first, err := svc.CreateBoost(ctx, CreateBoostRequest{
		DeviceID:       "device_boost_1",
		AssetID:        "asset_boost_1",
		PayeeID:        "fap_payee_1",
		AmountMSat:     1_000_000,
		Memo:           "dev boost",
		IdempotencyKey: "idem-boost-1",
	})
	if err != nil {
		t.Fatalf("CreateBoost #1: %v", err)
	}
	second, err := svc.CreateBoost(ctx, CreateBoostRequest{
		DeviceID:       "device_boost_1",
		AssetID:        "asset_boost_1",
		PayeeID:        "fap_payee_1",
		AmountMSat:     1_000_000,
		Memo:           "dev boost",
		IdempotencyKey: "idem-boost-1",
	})
	if err != nil {
		t.Fatalf("CreateBoost #2: %v", err)
	}
	if first.BoostID != second.BoostID {
		t.Fatalf("expected idempotent boost id, got %s vs %s", first.BoostID, second.BoostID)
	}
	if first.Status != "pending" {
		t.Fatalf("expected pending, got %s", first.Status)
	}

	paidAt := now + 2
	paid, err := svc.MarkBoostPaid(ctx, first.BoostID, paidAt)
	if err != nil {
		t.Fatalf("MarkBoostPaid: %v", err)
	}
	if paid.Status != "paid" || paid.PaidAt == nil || *paid.PaidAt != paidAt {
		t.Fatalf("unexpected paid boost: %+v", paid)
	}
	paidAgain, err := svc.MarkBoostPaid(ctx, first.BoostID, paidAt+5)
	if err != nil {
		t.Fatalf("MarkBoostPaid idempotent call failed: %v", err)
	}
	if paidAgain.PaidAt == nil || *paidAgain.PaidAt != paidAt {
		t.Fatalf("expected paid_at to remain unchanged, got %+v", paidAgain.PaidAt)
	}

	expiring, err := svc.CreateBoost(ctx, CreateBoostRequest{
		DeviceID:       "device_boost_2",
		AssetID:        "asset_boost_2",
		PayeeID:        "fap_payee_2",
		AmountMSat:     500_000,
		Memo:           "expiring boost",
		IdempotencyKey: "idem-boost-2",
	})
	if err != nil {
		t.Fatalf("CreateBoost expiring: %v", err)
	}

	now = now + 20
	expired, err := svc.GetBoost(ctx, expiring.BoostID)
	if err != nil {
		t.Fatalf("GetBoost expired: %v", err)
	}
	if expired.Status != "expired" {
		t.Fatalf("expected expired status, got %s", expired.Status)
	}
	if _, err := svc.MarkBoostPaid(ctx, expiring.BoostID, now); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation when paying expired boost, got %v", err)
	}
}

func TestBoostIdempotencyInNonDevCallsInvoiceOnce(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newService(t)
	defer repo.Close()

	svc.cfg.DevMode = false
	factory := svc.adapters.(*fakeFactory)
	factory.adapters["fap_payee_live"] = &fakeAdapter{
		bolt11:      "lnbc1live",
		paymentHash: "ph-live-1",
		checkingID:  "chk-live-1",
		expiresAt:   1900000000,
	}

	first, err := svc.CreateBoost(ctx, CreateBoostRequest{
		DeviceID:       "device_boost_live_1",
		AssetID:        "asset_live_1",
		PayeeID:        "fap_payee_live",
		AmountMSat:     1_000_000,
		Memo:           "live boost",
		IdempotencyKey: "idem-live-1",
	})
	if err != nil {
		t.Fatalf("CreateBoost #1: %v", err)
	}
	second, err := svc.CreateBoost(ctx, CreateBoostRequest{
		DeviceID:       "device_boost_live_1",
		AssetID:        "asset_live_1",
		PayeeID:        "fap_payee_live",
		AmountMSat:     1_000_000,
		Memo:           "live boost",
		IdempotencyKey: "idem-live-1",
	})
	if err != nil {
		t.Fatalf("CreateBoost #2: %v", err)
	}
	if first.BoostID != second.BoostID {
		t.Fatalf("expected idempotent boost id, got %s vs %s", first.BoostID, second.BoostID)
	}
	if factory.adapters["fap_payee_live"].calls != 1 {
		t.Fatalf("expected invoice call once, got %d", factory.adapters["fap_payee_live"].calls)
	}
	if first.Bolt11 != "lnbc1live" || first.LNBitsPaymentHash != "ph-live-1" || first.LNBitsCheckingID != "chk-live-1" {
		t.Fatalf("unexpected live boost response: %+v", first)
	}
}

func TestLedgerReportRecomputesWhenNewPaidEntryArrives(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newService(t)
	defer repo.Close()

	initialNow := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC).Unix()
	svc.nowUnix = func() int64 { return initialNow }

	withPaidAt := func(value int64) *int64 { return &value }
	periodStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC).Unix()
	periodEnd := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC).Unix()
	paidAtA := time.Date(2026, time.March, 3, 8, 0, 0, 0, time.UTC).Unix()
	paidAtB := time.Date(2026, time.March, 5, 9, 0, 0, 0, time.UTC).Unix()

	seed := []store.LedgerEntry{
		{EntryID: "lr-svc-1", DeviceID: "device_report_service", Kind: "access", Status: "paid", AssetID: "asset-a", PayeeID: "payee-1", AmountMSat: 1500, Currency: "msat", RelatedID: "challenge-1", CreatedAt: paidAtA, UpdatedAt: paidAtA, PaidAt: withPaidAt(paidAtA)},
		{EntryID: "lr-svc-2", DeviceID: "device_report_service", Kind: "boost", Status: "paid", AssetID: "asset-b", PayeeID: "payee-2", AmountMSat: 2500, Currency: "msat", RelatedID: "boost-2", CreatedAt: paidAtB, UpdatedAt: paidAtB, PaidAt: withPaidAt(paidAtB)},
	}
	for _, item := range seed {
		if err := repo.InsertLedgerEntryIfNotExists(ctx, item); err != nil {
			t.Fatalf("seed ledger entry %s: %v", item.EntryID, err)
		}
	}

	first, err := svc.GetLedgerReportForDevice(ctx, GetLedgerReportRequest{
		DeviceID: "device_report_service",
		Month:    "2026-03",
	})
	if err != nil {
		t.Fatalf("GetLedgerReportForDevice first: %v", err)
	}
	if first.Totals.PaidMSatAccess != 1500 || first.Totals.PaidMSatBoost != 2500 || first.Totals.PaidMSatTotal != 4000 {
		t.Fatalf("unexpected first totals: %+v", first.Totals)
	}
	if first.ComputedAt != initialNow {
		t.Fatalf("unexpected first computed_at: %d", first.ComputedAt)
	}

	storedFirst, err := repo.GetLedgerReportByDevicePeriod(ctx, "device_report_service", periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetLedgerReportByDevicePeriod first: %v", err)
	}
	if storedFirst.Status != "computed" {
		t.Fatalf("expected computed status, got %+v", storedFirst)
	}

	paidAtC := initialNow + 30
	newEntry := store.LedgerEntry{
		EntryID:    "lr-svc-3",
		DeviceID:   "device_report_service",
		Kind:       "access",
		Status:     "paid",
		AssetID:    "asset-a",
		PayeeID:    "payee-1",
		AmountMSat: 700,
		Currency:   "msat",
		RelatedID:  "challenge-3",
		CreatedAt:  paidAtC,
		UpdatedAt:  paidAtC,
		PaidAt:     withPaidAt(paidAtC),
	}
	if err := repo.InsertLedgerEntryIfNotExists(ctx, newEntry); err != nil {
		t.Fatalf("insert new ledger entry: %v", err)
	}

	recomputeNow := paidAtC + 10
	svc.nowUnix = func() int64 { return recomputeNow }

	second, err := svc.GetLedgerReportForDevice(ctx, GetLedgerReportRequest{
		DeviceID: "device_report_service",
		Month:    "2026-03",
	})
	if err != nil {
		t.Fatalf("GetLedgerReportForDevice second: %v", err)
	}
	if second.Totals.PaidMSatAccess != 2200 || second.Totals.PaidMSatBoost != 2500 || second.Totals.PaidMSatTotal != 4700 {
		t.Fatalf("unexpected second totals: %+v", second.Totals)
	}
	if second.ComputedAt != recomputeNow {
		t.Fatalf("expected recomputed_at %d, got %d", recomputeNow, second.ComputedAt)
	}
	if len(second.ByAsset) != 2 || second.ByAsset[0].AssetID != "asset-b" || second.ByAsset[0].AmountMSat != 2500 || second.ByAsset[1].AssetID != "asset-a" || second.ByAsset[1].AmountMSat != 2200 {
		t.Fatalf("unexpected by_asset after recompute: %+v", second.ByAsset)
	}

	storedSecond, err := repo.GetLedgerReportByDevicePeriod(ctx, "device_report_service", periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetLedgerReportByDevicePeriod second: %v", err)
	}
	if storedSecond.Status != "computed" || storedSecond.UpdatedAt != recomputeNow {
		t.Fatalf("unexpected stored recomputed report: %+v", storedSecond)
	}
}

func newService(t *testing.T) (*FAPService, *store.SQLiteRepository, []byte) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "fap.sqlite")
	repo, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	master := bytes.Repeat([]byte{0x33}, 32)
	factory := &fakeFactory{adapters: make(map[string]*fakeAdapter)}
	svc, err := New(repo, factory, Config{
		IssuerPrivKeyHex:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		DevMode:              true,
	})
	if err != nil {
		t.Fatalf("New service: %v", err)
	}
	return svc, repo, master
}

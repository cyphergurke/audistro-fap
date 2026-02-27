package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteRepositoryBasicFlow(t *testing.T) {
	repo, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "fap.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	now := int64(1700000000)
	payee := Payee{
		PayeeID: "p1", DisplayName: "Artist", Rail: "lightning", Mode: "lnbits",
		LNBitsBaseURL: "http://lnbits", LNBitsInvoiceKeyEnc: []byte("x"), LNBitsReadKeyEnc: []byte("y"),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.CreatePayee(context.Background(), payee); err != nil {
		t.Fatalf("CreatePayee: %v", err)
	}
	if _, err := repo.GetByID(context.Background(), payee.PayeeID); err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	asset := Asset{AssetID: "asset1", PayeeID: payee.PayeeID, Title: "Song", PriceMSat: 1000, ResourceID: "hls:key:asset1", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateAsset(context.Background(), asset); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	if _, err := repo.GetAssetByID(context.Background(), asset.AssetID); err != nil {
		t.Fatalf("GetAssetByID: %v", err)
	}

	intent := PaymentIntent{
		IntentID: "i1", AssetID: asset.AssetID, PayeeID: payee.PayeeID, AmountMSat: 1000,
		Bolt11: "lnbc", PaymentHash: "ph1", Status: "pending", InvoiceExpiresAt: now + 100, CreatedAt: now,
	}
	if err := repo.CreateIntent(context.Background(), intent); err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	if err := repo.MarkIntentSettled(context.Background(), intent.IntentID, now+10); err != nil {
		t.Fatalf("MarkIntentSettled: %v", err)
	}
	storedIntent, err := repo.GetIntentByPaymentHash(context.Background(), intent.PaymentHash)
	if err != nil {
		t.Fatalf("GetIntentByPaymentHash: %v", err)
	}
	if storedIntent.SettledAt == nil {
		t.Fatal("expected settled_at")
	}

	idempotencyKey := "challenge-idem-1"
	challenge := AccessChallenge{
		ChallengeID:       "ch1",
		AssetID:           "asset-catalog-1",
		PayeeID:           "fap_payee_catalog_1",
		AmountMSat:        1000,
		Memo:              "catalog challenge",
		Status:            "pending",
		Bolt11:            "lnbc-ch",
		LNBitsCheckingID:  "chk-ch-1",
		LNBitsPaymentHash: "ph-ch-1",
		ExpiresAt:         now + 100,
		CreatedAt:         now,
		UpdatedAt:         now,
		IdempotencyKey:    &idempotencyKey,
	}
	if err := repo.CreateChallenge(context.Background(), challenge); err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}
	storedChallenge, err := repo.GetChallengeByIdempotencyKey(context.Background(), idempotencyKey)
	if err != nil {
		t.Fatalf("GetChallengeByIdempotencyKey: %v", err)
	}
	if storedChallenge.ChallengeID != challenge.ChallengeID {
		t.Fatalf("expected challenge id %s, got %s", challenge.ChallengeID, storedChallenge.ChallengeID)
	}
	paidChallengeAt := now + 11
	if err := repo.UpdateChallengeStatus(context.Background(), challenge.ChallengeID, "paid", &paidChallengeAt, paidChallengeAt); err != nil {
		t.Fatalf("UpdateChallengeStatus: %v", err)
	}
	storedChallenge, err = repo.GetChallengeByLNBitsCheckingID(context.Background(), challenge.LNBitsCheckingID)
	if err != nil {
		t.Fatalf("GetChallengeByLNBitsCheckingID: %v", err)
	}
	if storedChallenge.Status != "paid" || storedChallenge.PaidAt == nil || *storedChallenge.PaidAt != paidChallengeAt {
		t.Fatalf("unexpected challenge after paid update: %+v", storedChallenge)
	}

	token := AccessToken{
		TokenID: "t1", IntentID: intent.IntentID, PayeeID: payee.PayeeID, AssetID: asset.AssetID,
		ResourceID: asset.ResourceID, Subject: "sub1", Token: "tok1", ExpiresAt: now + 600, CreatedAt: now,
	}
	if err := repo.CreateAccessToken(context.Background(), token); err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	if _, err := repo.GetAccessTokenByIntentID(context.Background(), intent.IntentID); err != nil {
		t.Fatalf("GetAccessTokenByIntentID: %v", err)
	}

	boost := Boost{
		BoostID: "b1", DeviceID: "device_store_boost_1", AssetID: asset.AssetID, PayeeID: "fap_payee_1", AmountMSat: 200000, Bolt11: "lnbcdev1",
		LNBitsPaymentHash: "ph-boost-1", LNBitsCheckingID: "chk-boost-1",
		Status: "pending", ExpiresAt: now + 300, CreatedAt: now, UpdatedAt: now, IdempotencyKey: "idem-1",
	}
	if err := repo.CreateBoost(context.Background(), boost); err != nil {
		t.Fatalf("CreateBoost: %v", err)
	}
	storedBoost, err := repo.GetBoostByIdempotencyKey(context.Background(), boost.IdempotencyKey)
	if err != nil {
		t.Fatalf("GetBoostByIdempotencyKey: %v", err)
	}
	if storedBoost.BoostID != boost.BoostID {
		t.Fatalf("expected boost id %s, got %s", boost.BoostID, storedBoost.BoostID)
	}
	if storedBoost.DeviceID != boost.DeviceID {
		t.Fatalf("expected boost device id %s, got %s", boost.DeviceID, storedBoost.DeviceID)
	}
	byCheckingID, err := repo.GetBoostByLNBitsCheckingID(context.Background(), "chk-boost-1")
	if err != nil {
		t.Fatalf("GetBoostByLNBitsCheckingID: %v", err)
	}
	if byCheckingID.BoostID != boost.BoostID {
		t.Fatalf("expected boost from checking id %s, got %s", boost.BoostID, byCheckingID.BoostID)
	}
	byPaymentHash, err := repo.GetBoostByLNBitsPaymentHash(context.Background(), "ph-boost-1")
	if err != nil {
		t.Fatalf("GetBoostByLNBitsPaymentHash: %v", err)
	}
	if byPaymentHash.BoostID != boost.BoostID {
		t.Fatalf("expected boost from payment hash %s, got %s", boost.BoostID, byPaymentHash.BoostID)
	}
	paidAt := now + 20
	if err := repo.UpdateBoostStatus(context.Background(), boost.BoostID, "paid", &paidAt, paidAt); err != nil {
		t.Fatalf("UpdateBoostStatus: %v", err)
	}
	if err := repo.UpdateBoostLNBitsWebhookEventID(context.Background(), boost.BoostID, "evt-1", paidAt); err != nil {
		t.Fatalf("UpdateBoostLNBitsWebhookEventID: %v", err)
	}
	byEventID, err := repo.GetBoostByLNBitsWebhookEventID(context.Background(), "evt-1")
	if err != nil {
		t.Fatalf("GetBoostByLNBitsWebhookEventID: %v", err)
	}
	if byEventID.BoostID != boost.BoostID {
		t.Fatalf("expected boost from event id %s, got %s", boost.BoostID, byEventID.BoostID)
	}
	storedBoost, err = repo.GetBoostByID(context.Background(), boost.BoostID)
	if err != nil {
		t.Fatalf("GetBoostByID: %v", err)
	}
	if storedBoost.Status != "paid" || storedBoost.PaidAt == nil || *storedBoost.PaidAt != paidAt {
		t.Fatalf("unexpected boost after paid update: %+v", storedBoost)
	}
}

func TestUniquePaymentHashEnforced(t *testing.T) {
	repo, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "fap.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	now := int64(1700000000)
	payee := Payee{PayeeID: "p1", DisplayName: "Artist", Rail: "lightning", Mode: "lnbits", LNBitsBaseURL: "http://lnbits", LNBitsInvoiceKeyEnc: []byte("x"), LNBitsReadKeyEnc: []byte("y"), CreatedAt: now, UpdatedAt: now}
	asset := Asset{AssetID: "asset1", PayeeID: payee.PayeeID, Title: "Song", PriceMSat: 1000, ResourceID: "hls:key:asset1", CreatedAt: now, UpdatedAt: now}
	_ = repo.CreatePayee(context.Background(), payee)
	_ = repo.CreateAsset(context.Background(), asset)

	i1 := PaymentIntent{IntentID: "i1", AssetID: asset.AssetID, PayeeID: payee.PayeeID, AmountMSat: 1000, Bolt11: "ln1", PaymentHash: "ph1", Status: "pending", InvoiceExpiresAt: now + 100, CreatedAt: now}
	i2 := PaymentIntent{IntentID: "i2", AssetID: asset.AssetID, PayeeID: payee.PayeeID, AmountMSat: 1000, Bolt11: "ln2", PaymentHash: "ph1", Status: "pending", InvoiceExpiresAt: now + 100, CreatedAt: now}
	if err := repo.CreateIntent(context.Background(), i1); err != nil {
		t.Fatalf("CreateIntent #1: %v", err)
	}
	if err := repo.CreateIntent(context.Background(), i2); err == nil {
		t.Fatal("expected unique constraint error for duplicate payment_hash")
	}
}

func TestBoostQueriesHandleLegacyNullLNBitsColumns(t *testing.T) {
	repo, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "fap.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	now := int64(1700000000)
	_, err = repo.DB().ExecContext(
		context.Background(),
		`INSERT INTO boosts (
			boost_id, asset_id, payee_id, amount_msat, bolt11,
			status, expires_at, paid_at, created_at, updated_at, idempotency_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-null-1", "asset-legacy", "payee-legacy", int64(1000), "lnbclegacy",
		"pending", now+300, nil, now, now, "idem-legacy-null-1",
	)
	if err != nil {
		t.Fatalf("insert legacy boost row: %v", err)
	}

	items, err := repo.ListBoosts(context.Background(), ListBoostsParams{
		AssetID: "asset-legacy",
		PayeeID: "payee-legacy",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListBoosts: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].LNBitsPaymentHash != "" || items[0].LNBitsCheckingID != "" || items[0].LNBitsWebhookEventID != "" {
		t.Fatalf("expected nullable LNbits columns mapped to empty strings, got %+v", items[0])
	}

	loaded, err := repo.GetBoostByID(context.Background(), "legacy-null-1")
	if err != nil {
		t.Fatalf("GetBoostByID: %v", err)
	}
	if loaded.LNBitsPaymentHash != "" || loaded.LNBitsCheckingID != "" || loaded.LNBitsWebhookEventID != "" {
		t.Fatalf("expected nullable LNbits columns mapped to empty strings, got %+v", loaded)
	}
}

func TestDeviceAndAccessGrantFlow(t *testing.T) {
	repo, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "fap.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	now := int64(1700000000)
	device := Device{
		DeviceID:   "device_store_1",
		CreatedAt:  now,
		LastSeenAt: now,
	}
	if err := repo.CreateDevice(context.Background(), device); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if err := repo.TouchDevice(context.Background(), device.DeviceID, now+10); err != nil {
		t.Fatalf("TouchDevice: %v", err)
	}
	storedDevice, err := repo.GetDeviceByID(context.Background(), device.DeviceID)
	if err != nil {
		t.Fatalf("GetDeviceByID: %v", err)
	}
	if storedDevice.LastSeenAt != now+10 {
		t.Fatalf("expected touched last_seen_at=%d, got %d", now+10, storedDevice.LastSeenAt)
	}

	grant := AccessGrant{
		GrantID:          "grant_store_1",
		DeviceID:         device.DeviceID,
		AssetID:          "asset_store_1",
		Scope:            "hls_key",
		MinutesPurchased: 10,
		Status:           "active",
		ChallengeID:      "challenge_store_1",
		AmountMSat:       1000,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.CreateAccessGrant(context.Background(), grant); err != nil {
		t.Fatalf("CreateAccessGrant: %v", err)
	}
	fetchedByChallenge, err := repo.GetAccessGrantByChallengeID(context.Background(), grant.ChallengeID)
	if err != nil {
		t.Fatalf("GetAccessGrantByChallengeID: %v", err)
	}
	if fetchedByChallenge.GrantID != grant.GrantID {
		t.Fatalf("expected grant id %s, got %s", grant.GrantID, fetchedByChallenge.GrantID)
	}
	if err := repo.ActivateAccessGrant(context.Background(), grant.GrantID, now+1, now+601, now+1); err != nil {
		t.Fatalf("ActivateAccessGrant: %v", err)
	}
	latest, err := repo.GetLatestAccessGrantByDeviceAsset(context.Background(), device.DeviceID, grant.AssetID)
	if err != nil {
		t.Fatalf("GetLatestAccessGrantByDeviceAsset: %v", err)
	}
	if latest.ValidFrom == nil || latest.ValidUntil == nil {
		t.Fatalf("expected valid_from/valid_until set, got %+v", latest)
	}
	if err := repo.UpdateAccessGrantStatus(context.Background(), grant.GrantID, "expired", now+700); err != nil {
		t.Fatalf("UpdateAccessGrantStatus: %v", err)
	}
	listed, err := repo.ListAccessGrantsByDevice(context.Background(), device.DeviceID, "")
	if err != nil {
		t.Fatalf("ListAccessGrantsByDevice: %v", err)
	}
	if len(listed) != 1 || listed[0].Status != "expired" {
		t.Fatalf("expected expired grant in list, got %+v", listed)
	}
}

func TestWebhookEventsPrune(t *testing.T) {
	repo, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "fap.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	if _, err := repo.RecordWebhookEvent(context.Background(), WebhookEvent{
		EventKey:   "evt-old",
		ReceivedAt: 100,
	}); err != nil {
		t.Fatalf("RecordWebhookEvent old: %v", err)
	}
	if _, err := repo.RecordWebhookEvent(context.Background(), WebhookEvent{
		EventKey:   "evt-new",
		ReceivedAt: 1000,
	}); err != nil {
		t.Fatalf("RecordWebhookEvent new: %v", err)
	}

	deleted, err := repo.PruneWebhookEvents(context.Background(), 500)
	if err != nil {
		t.Fatalf("PruneWebhookEvents: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted row, got %d", deleted)
	}

	var remaining int
	if err := repo.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM webhook_events`).Scan(&remaining); err != nil {
		t.Fatalf("count webhook events: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("expected 1 remaining webhook event, got %d", remaining)
	}
}

func TestLedgerEntryListAndStatusUpdate(t *testing.T) {
	repo, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "fap.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer repo.Close()

	base := LedgerEntry{
		DeviceID:   "device_ledger_store",
		Kind:       "access",
		Status:     "pending",
		PayeeID:    "payee_store",
		AmountMSat: 1000,
		Currency:   "msat",
	}
	entryA := base
	entryA.EntryID = "ledger_store_a"
	entryA.AssetID = "asset_a"
	entryA.RelatedID = "challenge_a"
	entryA.CreatedAt = 300
	entryA.UpdatedAt = 300
	if err := repo.InsertLedgerEntryIfNotExists(context.Background(), entryA); err != nil {
		t.Fatalf("InsertLedgerEntryIfNotExists A: %v", err)
	}

	entryB := base
	entryB.EntryID = "ledger_store_b"
	entryB.Kind = "boost"
	entryB.AssetID = "asset_b"
	entryB.RelatedID = "boost_b"
	entryB.CreatedAt = 200
	entryB.UpdatedAt = 200
	if err := repo.InsertLedgerEntryIfNotExists(context.Background(), entryB); err != nil {
		t.Fatalf("InsertLedgerEntryIfNotExists B: %v", err)
	}

	paidAt := int64(333)
	if err := repo.UpdateLedgerStatus(context.Background(), "access", "challenge_a", "paid", &paidAt, "chk-a", 333); err != nil {
		t.Fatalf("UpdateLedgerStatus: %v", err)
	}

	items, err := repo.ListLedgerEntriesForDevice(context.Background(), ListLedgerEntriesParams{
		DeviceID: "device_ledger_store",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListLedgerEntriesForDevice: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(items))
	}
	if items[0].EntryID != "ledger_store_a" || items[0].Status != "paid" || items[0].PaidAt == nil || *items[0].PaidAt != paidAt {
		t.Fatalf("unexpected first ledger entry: %+v", items[0])
	}
	if items[1].EntryID != "ledger_store_b" {
		t.Fatalf("unexpected second ledger entry: %+v", items[1])
	}
}

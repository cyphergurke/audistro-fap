package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/yourorg/fap/internal/store/repo"
)

func TestMigrateRuns(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t, ctx)
	defer db.Close()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if !tableExists(t, ctx, db, "payment_intents") {
		t.Fatal("payment_intents table not found")
	}
	if !tableExists(t, ctx, db, "tokens") {
		t.Fatal("tokens table not found")
	}
	if !tableExists(t, ctx, db, "hls_keys") {
		t.Fatal("hls_keys table not found")
	}
	if !columnExists(t, ctx, db, "payment_intents", "rail") {
		t.Fatal("payment_intents.rail not found")
	}
	if !columnExists(t, ctx, db, "payment_intents", "provider_ref") {
		t.Fatal("payment_intents.provider_ref not found")
	}
}

func TestUniquePaymentHashEnforced(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t, ctx)
	defer db.Close()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	intents := NewIntentsRepo(db)
	intent := newLightningIntent("pi-1", "hash-dup")
	if err := intents.CreatePending(ctx, intent); err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	intent.ID = "pi-2"
	if err := intents.CreatePending(ctx, intent); err == nil {
		t.Fatal("expected unique payment_hash error")
	} else if !strings.Contains(err.Error(), "payment_intents.payment_hash") &&
		!strings.Contains(err.Error(), "payment_intents.rail, payment_intents.provider_ref") {
		t.Fatalf("expected unique constraint error for payment_hash or rail/provider_ref, got: %v", err)
	}
}

func TestTokenUpsertIdempotentOnPaymentHashAndResource(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t, ctx)
	defer db.Close()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tokens := NewTokensRepo(db)

	rec := repo.TokenRecord{
		ID:          "tok-1",
		PaymentHash: "ph-1",
		ResourceID:  "res-1",
		Token:       "token-1",
		IssuedAt:    1700000000,
		ExpiresAt:   1700000600,
	}
	if err := tokens.Upsert(ctx, rec); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	rec2 := rec
	rec2.ID = "tok-2"
	rec2.Token = "token-2"
	rec2.ExpiresAt = 1700001200
	if err := tokens.Upsert(ctx, rec2); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tokens WHERE payment_hash = ? AND resource_id = ?`, rec.PaymentHash, rec.ResourceID).Scan(&count); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one token row after idempotent upsert, got %d", count)
	}
}

func TestMigration003BackfillsLegacyLightningFields(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t, ctx)
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
CREATE TABLE payment_intents (
    id TEXT PRIMARY KEY,
    resource_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    amount_msat INTEGER NOT NULL,
    bolt11 TEXT NOT NULL,
    payment_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    settled_at INTEGER
);`); err != nil {
		t.Fatalf("create legacy payment_intents: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE TABLE tokens (
    id TEXT PRIMARY KEY,
    payment_hash TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    token TEXT NOT NULL,
    issued_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    UNIQUE(payment_hash, resource_id)
);`); err != nil {
		t.Fatalf("create legacy tokens: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE hls_keys (asset_id TEXT PRIMARY KEY, key_hex TEXT NOT NULL);`); err != nil {
		t.Fatalf("create legacy hls_keys: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO payment_intents (
	id, resource_id, asset_id, subject, amount_msat, bolt11, payment_hash, status, created_at, expires_at, settled_at
) VALUES (
	'pi-1', 'hls:key:asset-1', 'asset-1', 'sub-1', 12345, 'lnbc123', 'hash123', 'pending', 1700000000, 1700000900, NULL
);`); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate after legacy bootstrap: %v", err)
	}

	var rail string
	var amount int64
	var amountUnit string
	var asset string
	var providerRef string
	var offer string
	if err := db.QueryRowContext(ctx, `
SELECT rail, amount, amount_unit, asset, provider_ref, offer
FROM payment_intents WHERE id = 'pi-1'`).Scan(&rail, &amount, &amountUnit, &asset, &providerRef, &offer); err != nil {
		t.Fatalf("select backfilled row: %v", err)
	}

	if rail != "lightning" || amount != 12345 || amountUnit != "msat" || asset != "BTC" || providerRef != "hash123" || offer != "lnbc123" {
		t.Fatalf("unexpected backfill values: rail=%s amount=%d amount_unit=%s asset=%s provider_ref=%s offer=%s", rail, amount, amountUnit, asset, providerRef, offer)
	}
}

func newLightningIntent(id string, paymentHash string) repo.PaymentIntent {
	return repo.PaymentIntent{
		ID:          id,
		ResourceID:  "res-1",
		AssetID:     "asset-1",
		Subject:     "sub-1",
		Status:      "pending",
		CreatedAt:   1700000000,
		ExpiresAt:   1700000900,
		Rail:        repo.PaymentRailLightning,
		Amount:      100000,
		AmountUnit:  repo.AmountUnitMsat,
		Asset:       "BTC",
		ProviderRef: paymentHash,
		Offer:       "lnbc...",
		AmountMSat:  100000,
		Bolt11:      "lnbc...",
		PaymentHash: paymentHash,
	}
}

func newTestDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func tableExists(t *testing.T, ctx context.Context, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	return err == nil && name == table
}

func columnExists(t *testing.T, ctx context.Context, db *sql.DB, table string, column string) bool {
	t.Helper()
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

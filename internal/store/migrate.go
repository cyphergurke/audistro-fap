package store

import (
	"context"
	"database/sql"
	"fmt"
)

func runMigrations(ctx context.Context, db *sql.DB) error {
	for idx, stmt := range migrations {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply migration %d: %w", idx+1, err)
		}
	}
	if err := ensureBoostSchema(ctx, db); err != nil {
		return err
	}
	if err := ensureAccessSchema(ctx, db); err != nil {
		return err
	}
	return nil
}

func ensureBoostSchema(ctx context.Context, db *sql.DB) error {
	if err := ensureColumn(ctx, db, "boosts", "device_id", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "boosts", "lnbits_payment_hash", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "boosts", "lnbits_checking_id", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "boosts", "lnbits_webhook_event_id", "TEXT"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_boosts_checking_id ON boosts(lnbits_checking_id);`); err != nil {
		return fmt.Errorf("create index idx_boosts_checking_id: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_boosts_device_id ON boosts(device_id);`); err != nil {
		return fmt.Errorf("create index idx_boosts_device_id: %w", err)
	}
	return nil
}

func ensureAccessSchema(ctx context.Context, db *sql.DB) error {
	if err := ensureColumn(ctx, db, "challenges", "device_id", "TEXT"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_challenges_device ON challenges(device_id, created_at DESC);`); err != nil {
		return fmt.Errorf("create index idx_challenges_device: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS devices (
		device_id TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL
	);`); err != nil {
		return fmt.Errorf("create table devices: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS access_grants (
		grant_id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		asset_id TEXT NOT NULL,
		scope TEXT NOT NULL,
		minutes_purchased INTEGER NOT NULL,
		valid_from INTEGER,
		valid_until INTEGER,
		status TEXT NOT NULL,
		challenge_id TEXT NOT NULL,
		amount_msat INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`); err != nil {
		return fmt.Errorf("create table access_grants: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_grants_device_asset ON access_grants(device_id, asset_id);`); err != nil {
		return fmt.Errorf("create index idx_grants_device_asset: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_grants_device_until ON access_grants(device_id, valid_until);`); err != nil {
		return fmt.Errorf("create index idx_grants_device_until: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_grants_asset_until ON access_grants(asset_id, valid_until);`); err != nil {
		return fmt.Errorf("create index idx_grants_asset_until: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_grants_device_asset_challenge ON access_grants(device_id, asset_id, challenge_id);`); err != nil {
		return fmt.Errorf("create index idx_grants_device_asset_challenge: %w", err)
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, table string, column string, definition string) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("read schema for %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan table_info(%s): %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info(%s): %w", table, err)
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS payees (
		payee_id TEXT PRIMARY KEY,
		display_name TEXT NOT NULL,
		rail TEXT NOT NULL,
		mode TEXT NOT NULL,
		lnbits_base_url TEXT NOT NULL,
		lnbits_invoice_key_enc BLOB NOT NULL,
		lnbits_read_key_enc BLOB NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS assets (
		asset_id TEXT PRIMARY KEY,
		payee_id TEXT NOT NULL,
		title TEXT NOT NULL,
		price_msat INTEGER NOT NULL,
		resource_id TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_assets_payee_id ON assets(payee_id);`,
	`CREATE TABLE IF NOT EXISTS payment_intents (
		intent_id TEXT PRIMARY KEY,
		asset_id TEXT NOT NULL,
		payee_id TEXT NOT NULL,
		amount_msat INTEGER NOT NULL,
		bolt11 TEXT NOT NULL,
		payment_hash TEXT NOT NULL,
		status TEXT NOT NULL,
		invoice_expires_at INTEGER NOT NULL,
		settled_at INTEGER,
		created_at INTEGER NOT NULL,
		UNIQUE(payment_hash)
	);`,
	`CREATE TABLE IF NOT EXISTS challenges (
		challenge_id TEXT PRIMARY KEY,
		device_id TEXT,
		asset_id TEXT NOT NULL,
		payee_id TEXT NOT NULL,
		amount_msat INTEGER NOT NULL,
		memo TEXT,
		status TEXT NOT NULL,
		bolt11 TEXT NOT NULL,
		lnbits_checking_id TEXT,
		lnbits_payment_hash TEXT,
		expires_at INTEGER NOT NULL,
		paid_at INTEGER,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		idempotency_key TEXT UNIQUE
	);`,
	`CREATE INDEX IF NOT EXISTS idx_challenges_payment_hash ON challenges(lnbits_payment_hash);`,
	`CREATE INDEX IF NOT EXISTS idx_challenges_checking_id ON challenges(lnbits_checking_id);`,
	`CREATE INDEX IF NOT EXISTS idx_challenges_status ON challenges(status);`,
	`CREATE TABLE IF NOT EXISTS devices (
		device_id TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS access_grants (
		grant_id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		asset_id TEXT NOT NULL,
		scope TEXT NOT NULL,
		minutes_purchased INTEGER NOT NULL,
		valid_from INTEGER,
		valid_until INTEGER,
		status TEXT NOT NULL,
		challenge_id TEXT NOT NULL,
		amount_msat INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_grants_device_asset ON access_grants(device_id, asset_id);`,
	`CREATE INDEX IF NOT EXISTS idx_grants_device_until ON access_grants(device_id, valid_until);`,
	`CREATE INDEX IF NOT EXISTS idx_grants_asset_until ON access_grants(asset_id, valid_until);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_grants_device_asset_challenge ON access_grants(device_id, asset_id, challenge_id);`,
	`CREATE TABLE IF NOT EXISTS access_tokens (
		token_id TEXT PRIMARY KEY,
		intent_id TEXT NOT NULL UNIQUE,
		payee_id TEXT NOT NULL,
		asset_id TEXT NOT NULL,
		resource_id TEXT NOT NULL,
		subject TEXT NOT NULL,
		token TEXT NOT NULL UNIQUE,
		expires_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS boosts (
		boost_id TEXT PRIMARY KEY,
		device_id TEXT,
		asset_id TEXT NOT NULL,
		payee_id TEXT NOT NULL,
		amount_msat INTEGER NOT NULL,
		bolt11 TEXT NOT NULL,
		lnbits_payment_hash TEXT,
		lnbits_checking_id TEXT,
		lnbits_webhook_event_id TEXT,
		status TEXT NOT NULL,
		expires_at INTEGER NOT NULL,
		paid_at INTEGER,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		idempotency_key TEXT NOT NULL UNIQUE
	);`,
	`CREATE INDEX IF NOT EXISTS idx_boosts_expires_at ON boosts(expires_at);`,
	`CREATE INDEX IF NOT EXISTS idx_boosts_status ON boosts(status);`,
	`CREATE INDEX IF NOT EXISTS idx_boosts_asset_created ON boosts(asset_id, created_at DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_boosts_payee_created ON boosts(payee_id, created_at DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_boosts_status_created ON boosts(status, created_at DESC);`,
	`SELECT 1;`,
	`CREATE TABLE IF NOT EXISTS ledger_entries (
		entry_id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		status TEXT NOT NULL,
		asset_id TEXT,
		payee_id TEXT NOT NULL,
		amount_msat INTEGER NOT NULL,
		currency TEXT NOT NULL DEFAULT 'msat',
		related_id TEXT NOT NULL,
		reference_id TEXT,
		memo TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		paid_at INTEGER
	);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_ledger_kind_related ON ledger_entries(kind, related_id);`,
	`CREATE INDEX IF NOT EXISTS idx_ledger_device_created ON ledger_entries(device_id, created_at DESC, entry_id DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_ledger_device_status_created ON ledger_entries(device_id, status, created_at DESC, entry_id DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_ledger_payee_created ON ledger_entries(payee_id, created_at DESC, entry_id DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_ledger_asset_created ON ledger_entries(asset_id, created_at DESC, entry_id DESC);`,
	`CREATE TABLE IF NOT EXISTS webhook_events (
		event_key TEXT PRIMARY KEY,
		received_at INTEGER NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_webhook_events_received_at ON webhook_events(received_at);`,
}

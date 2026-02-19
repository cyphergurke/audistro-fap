package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

const migration001And002SQL = `
CREATE TABLE IF NOT EXISTS payment_intents (
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
);

CREATE TABLE IF NOT EXISTS tokens (
    id TEXT PRIMARY KEY,
    payment_hash TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    token TEXT NOT NULL,
    issued_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    UNIQUE(payment_hash, resource_id)
);

CREATE TABLE IF NOT EXISTS hls_keys (
    asset_id TEXT PRIMARY KEY,
    key_hex TEXT NOT NULL
);
`

func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx, migration001And002SQL); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("run base migrations: %w", err)
	}

	if err := apply003(ctx, tx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("run migration 003: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration tx: %w", err)
	}
	return nil
}

func apply003(ctx context.Context, tx *sql.Tx) error {
	if err := addColumnIfMissing(ctx, tx, "payment_intents", "rail", "TEXT NOT NULL DEFAULT 'lightning'"); err != nil {
		return err
	}
	if err := addColumnIfMissing(ctx, tx, "payment_intents", "amount", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(ctx, tx, "payment_intents", "amount_unit", "TEXT NOT NULL DEFAULT 'msat'"); err != nil {
		return err
	}
	if err := addColumnIfMissing(ctx, tx, "payment_intents", "asset", "TEXT NOT NULL DEFAULT 'BTC'"); err != nil {
		return err
	}
	if err := addColumnIfMissing(ctx, tx, "payment_intents", "provider_ref", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(ctx, tx, "payment_intents", "offer", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_intents_rail_provider_ref ON payment_intents(rail, provider_ref);`); err != nil {
		return fmt.Errorf("create rail/provider_ref unique index: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE payment_intents
SET
  rail = 'lightning',
  amount = amount_msat,
  amount_unit = 'msat',
  asset = 'BTC',
  provider_ref = payment_hash,
  offer = bolt11
WHERE
  (rail = '' OR rail = 'lightning')
  AND (amount = 0 OR amount = amount_msat)
  AND (amount_unit = '' OR amount_unit = 'msat')
  AND (asset = '' OR asset = 'BTC')
  AND (provider_ref = '' OR provider_ref = payment_hash)
  AND (offer = '' OR offer = bolt11);`); err != nil {
		return fmt.Errorf("backfill rail generic columns: %w", err)
	}

	return nil
}

func addColumnIfMissing(ctx context.Context, tx *sql.Tx, table string, column string, definition string) error {
	exists, err := hasColumn(ctx, tx, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s;`, table, column, definition)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

func hasColumn(ctx context.Context, tx *sql.Tx, table string, column string) (bool, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s);`, table))
	if err != nil {
		return false, fmt.Errorf("pragma table_info(%s): %w", table, err)
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
			return false, fmt.Errorf("scan table_info row: %w", err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate table_info rows: %w", err)
	}
	return false, nil
}

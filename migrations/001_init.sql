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

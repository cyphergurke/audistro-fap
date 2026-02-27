CREATE TABLE IF NOT EXISTS payees (
  payee_id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  rail TEXT NOT NULL,
  mode TEXT NOT NULL,
  lnbits_base_url TEXT NOT NULL,
  lnbits_invoice_key_enc BLOB NOT NULL,
  lnbits_read_key_enc BLOB NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS assets (
  asset_id TEXT PRIMARY KEY,
  payee_id TEXT NOT NULL,
  title TEXT NOT NULL,
  price_msat INTEGER NOT NULL,
  resource_id TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_assets_payee_id ON assets(payee_id);

CREATE TABLE IF NOT EXISTS payment_intents (
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
);

CREATE TABLE IF NOT EXISTS challenges (
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
);

CREATE INDEX IF NOT EXISTS idx_challenges_payment_hash ON challenges(lnbits_payment_hash);
CREATE INDEX IF NOT EXISTS idx_challenges_checking_id ON challenges(lnbits_checking_id);
CREATE INDEX IF NOT EXISTS idx_challenges_status ON challenges(status);
CREATE INDEX IF NOT EXISTS idx_challenges_device ON challenges(device_id, created_at DESC);

CREATE TABLE IF NOT EXISTS devices (
  device_id TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS access_grants (
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
);

CREATE INDEX IF NOT EXISTS idx_grants_device_asset ON access_grants(device_id, asset_id);
CREATE INDEX IF NOT EXISTS idx_grants_device_until ON access_grants(device_id, valid_until);
CREATE INDEX IF NOT EXISTS idx_grants_asset_until ON access_grants(asset_id, valid_until);
CREATE UNIQUE INDEX IF NOT EXISTS idx_grants_device_asset_challenge ON access_grants(device_id, asset_id, challenge_id);

CREATE TABLE IF NOT EXISTS access_tokens (
  token_id TEXT PRIMARY KEY,
  intent_id TEXT NOT NULL UNIQUE,
  payee_id TEXT NOT NULL,
  asset_id TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  subject TEXT NOT NULL,
  token TEXT NOT NULL UNIQUE,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);

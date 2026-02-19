ALTER TABLE payment_intents ADD COLUMN rail TEXT NOT NULL DEFAULT 'lightning';
ALTER TABLE payment_intents ADD COLUMN amount INTEGER NOT NULL DEFAULT 0;
ALTER TABLE payment_intents ADD COLUMN amount_unit TEXT NOT NULL DEFAULT 'msat';
ALTER TABLE payment_intents ADD COLUMN asset TEXT NOT NULL DEFAULT 'BTC';
ALTER TABLE payment_intents ADD COLUMN provider_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE payment_intents ADD COLUMN offer TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_intents_rail_provider_ref
ON payment_intents(rail, provider_ref);

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
  AND (offer = '' OR offer = bolt11);

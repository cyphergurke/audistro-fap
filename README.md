# audistro-fap

`audistro-fap` is the payment and access-control service for fan playback.

It handles:

- payee management (LNbits credentials encrypted at rest)
- paid access challenges and token minting
- device-bound grants for HLS key access
- boosts/tips (invoice + status)
- device-scoped ledger/audit entries
- LNbits webhook settlement

## Runtime Requirements

- Go `1.26+`
- SQLite database path writable by process
- token secret file (minimum 16 bytes)
- issuer private key hex (32 bytes)
- master encryption key hex (32 bytes)

## Required Environment Variables

- `FAP_DB_PATH`
- `FAP_ISSUER_PRIVKEY_HEX` (32-byte hex)
- `FAP_MASTER_KEY_HEX` (32-byte hex)
- `FAP_WEBHOOK_SECRET`
- `FAP_TOKEN_SECRET_PATH` (file path, min 16 bytes)

## Optional Environment Variables (with defaults)

- `FAP_HTTP_ADDR` (default `:8080`)
- `FAP_DEV_MODE` (default `false`)
- `FAP_TOKEN_TTL_SECONDS` (default `900`)
- `FAP_INVOICE_EXPIRY_SECONDS` (default `900`)
- `FAP_MAX_ACCESS_AMOUNT_MSAT` (default `50000000`)
- `FAP_ACCESS_MINUTES_PER_PAYMENT` (default `10`)
- `FAP_WEBHOOK_EVENT_RETENTION_SECONDS` (default `604800`)
- `FAP_WEBHOOK_EVENT_PRUNE_INTERVAL_SECONDS` (default `300`)
- `FAP_EXPOSE_BOLT11_IN_LIST` (default `false`)
- `FAP_DEVICE_COOKIE_SECURE` (default `false`)
- `FAP_ENABLE_CORS` (default `false`)
- `FAP_CORS_ALLOWED_ORIGINS` (optional CSV)
- `FAP_CORS_ALLOW_CREDENTIALS` (default `false`)

Note:

- `FAP_LNBITS_*` environment variables are **not** used by the server config loader.
- LNbits keys are stored per payee via `POST /v1/payees`.

## Run

```bash
set -a
source .env
set +a

go run ./cmd/fapd
```

Migrations are applied automatically on startup.

Migrate only:

```bash
go run ./cmd/fapd -migrate
```

## API Endpoints

Core:

- `GET /healthz`
- `GET /openapi.yaml`
- `GET /docs`

Payee / asset setup:

- `POST /v1/payees`
- `POST /v1/assets`

Access flow:

- `POST /v1/device/bootstrap`
- `POST /v1/fap/challenge`
- `POST /v1/fap/token`
- `GET /v1/access/grants`
- `GET /hls/{assetId}/key`
- `POST /v1/access/{assetId}` (dev mode only)

Boost flow:

- `POST /v1/boost`
- `GET /v1/boost`
- `GET /v1/boost/{boostId}`
- `POST /v1/boost/{boostId}/mark_paid` (dev mode only)

Ledger / transparency:

- `GET /v1/ledger`

Webhook:

- `POST /v1/fap/webhook/lnbits`

## Access Model (P1+)

Device identity:

- `POST /v1/device/bootstrap` sets `fap_device_id` (httpOnly cookie).
- Challenge and token exchange are device-bound.

Challenge:

- `POST /v1/fap/challenge` creates invoice-backed access intent.
- In non-dev mode, playback should use this path.

Token:

- `POST /v1/fap/token` issues access token only after settlement.
- returns conflict while payment is not settled.

Key endpoint:

- `GET /hls/{assetId}/key` requires valid access token and matching device cookie.
- first successful key fetch activates grant validity window (`valid_from`, `valid_until`).

## Boost Model

- `POST /v1/boost` creates invoice for tip/boost.
- status and listing endpoints support polling/history.
- `mark_paid` exists for dev-only manual testing.

## Ledger Model

- every access challenge / boost creates a ledger entry.
- settlement transitions `pending -> paid` (or expiry/failure states).
- `GET /v1/ledger` is device-scoped via cookie.

## LNbits Integration

Per-payee credentials:

- invoice key and read key are provided in `POST /v1/payees` payload.
- keys are encrypted in DB using `FAP_MASTER_KEY_HEX`.

Webhook:

- `/v1/fap/webhook/lnbits` requires `FAP_WEBHOOK_SECRET`.
- duplicate webhook events are safely ignored.

## Rate Limits (built-in defaults)

Applied per device cookie, with IP fallback when cookie missing:

- challenge: `2 rps`, burst `4`
- token: `2 rps`, burst `4`
- key: `5 rps`, burst `10`

Limiter buckets are TTL-pruned in-memory to avoid unbounded growth.

## Local Compose Example

Typical `env/fap.env` values in this monorepo:

```dotenv
FAP_HTTP_ADDR=:8080
FAP_DB_PATH=/var/lib/fap/fap.db
FAP_TOKEN_SECRET_PATH=/run/secrets/fap_token_secret
FAP_DEV_MODE=false
FAP_ENABLE_CORS=true
FAP_CORS_ALLOWED_ORIGINS=http://localhost:3000
```

## Quick Verification

Health:

```bash
curl -sS http://localhost:18081/healthz
```

Device bootstrap:

```bash
curl -i -X POST http://localhost:18081/v1/device/bootstrap
```

Paid-access smoke:

```bash
./scripts/smoke-paid-access.sh --wait-manual
```

## Troubleshooting

`startup error: apply migration ...`:

- usually means old SQLite schema + new binary mismatch.
- ensure the latest binary is running and migration path is consistent.
- inspect logs: `docker compose logs -f fap`.

`not found: payee` on challenge/boost:

- payee missing in FAP DB.
- create via `/v1/payees` or dev admin UI.

`device_mismatch` on token/key:

- challenge/token request not using same browser cookie context.
- bootstrap and subsequent calls must share `fap_device_id`.

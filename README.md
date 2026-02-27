# FAP (Fan Access Protocol)

Multi-artist payment-gated access server in Go.

## Required env vars

- `FAP_HTTP_ADDR`
- `FAP_DB_PATH`
- `FAP_ISSUER_PRIVKEY_HEX` (32-byte hex)
- `FAP_MASTER_KEY_HEX` (32-byte hex for AES-256-GCM key encryption)
- `FAP_WEBHOOK_SECRET`
- `FAP_TOKEN_SECRET_PATH` (path to HMAC secret file, min 16 bytes)
- `FAP_DEV_MODE` (default `false`)
- `FAP_TOKEN_TTL_SECONDS` (default `900`)
- `FAP_INVOICE_EXPIRY_SECONDS`
- `FAP_ENABLE_CORS` (default `false`, applies to `/hls/*`)
- `FAP_CORS_ALLOWED_ORIGINS` (optional comma-separated exact origins)
- `FAP_CORS_ALLOW_CREDENTIALS` (default `false`)

## Run

```bash
set -a
source .env
set +a

go run ./cmd/fapd
```

Migrations run automatically on startup.

Migrate only:

```bash
go run ./cmd/fapd -migrate
```

## API

- `GET /healthz`
- `POST /v1/payees`
- `POST /v1/assets`
- `POST /v1/fap/challenge`
- `POST /v1/fap/webhook/lnbits`
- `POST /v1/fap/token`
- `POST /v1/access/{assetId}` (dev mode only)
- `GET /hls/{assetId}/key`

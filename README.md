# FAP

Minimal payment-gated HLS key service in Go.

## Module Usage (Public API)

Dieses Repo kann als Go-Modul konsumiert werden:

```bash
go get github.com/yourorg/fap
```

Nur diese Public-Packages für Integrationen verwenden:

- `github.com/yourorg/fap/pkg/fapkit`
- `github.com/yourorg/fap/pkg/fapkit/http`
- `github.com/yourorg/fap/pkg/fapkit/mw`
- `github.com/yourorg/fap/pkg/fapkit/store`
- `github.com/yourorg/fap/pkg/fapkit/pay`

`internal/*` ist Implementierungsdetail und nicht für externe Module gedacht.

## Required Environment Variables

- `FAP_ISSUER_PRIVKEY_HEX`
- `FAP_DB_PATH`
- `FAP_LNBITS_BASE_URL`
- `FAP_LNBITS_INVOICE_API_KEY`
- `FAP_LNBITS_READONLY_API_KEY`
- `FAP_WEBHOOK_SECRET`

Optional:

- `FAP_HTTP_ADDR` (default `:8080`)
- `FAP_TOKEN_TTL_SECONDS` (default `600`)
- `FAP_INVOICE_EXPIRY_SECONDS` (default `900`)
- `FAP_PRICE_MSAT_DEFAULT` (default `100000`)

## Quickstart (Local)

1. Prepare env:

```bash
cp .env.example .env
# fill required values in .env
```

2. Export env vars in your shell:

```bash
set -a
source .env
set +a
```

Hinweis: `go run ./cmd/fapd` lädt `.env` nicht automatisch.

3. Run migrations only:

```bash
go run ./cmd/fapd -migrate
```

4. Start server (migrations also run automatically on startup):

```bash
go run ./cmd/fapd
```

Beim Start loggt `fapd` den gebundenen Port, z. B. `fapd listening on :8080`.

## Docker

1. Ensure `.env` contains required values.
2. Build image:

```bash
docker build -t fapd:local .
```

3. Start service in background:

```bash
docker compose up -d
```

Data persists in `./data` on host and is mounted to `/data` in container.
`FAP_DB_PATH` is set to `/data/fap.db` in `docker-compose.yml`.

## Useful Endpoints

- `GET /healthz`
- `GET /metrics`
- `GET /openapi.yaml`
- `GET /docs` (Scalar API UI)
- `POST /fap/challenge`
- `POST /fap/token`
- `POST /fap/webhook/lnbits`
- `POST /fap/webhook/lightning`
- `GET /hls/{assetId}/key`

## Embedding Example

```go
package main

import (
	"log"
	"net/http"

	"github.com/yourorg/fap/pkg/fapkit"
	faphttp "github.com/yourorg/fap/pkg/fapkit/http"
	"github.com/yourorg/fap/pkg/fapkit/pay"
	"github.com/yourorg/fap/pkg/fapkit/store"
)

func main() {
	cfg := fapkit.Config{
		HTTPAddr:             ":8080",
		IssuerPrivKeyHex:     "<32-byte-hex>",
		DBPath:               "./data/fap.db",
		LNBitsBaseURL:        "https://legend.lnbits.com",
		LNBitsInvoiceAPIKey:  "<invoice-key>",
		LNBitsReadOnlyAPIKey: "<readonly-key>",
		WebhookSecret:        "<secret>",
		TokenTTLSeconds:      600,
		InvoiceExpirySeconds: 900,
		PriceMsatDefault:     100000,
	}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	st, closeFn, err := store.NewSQLiteStore(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = closeFn() }()

	p, err := pay.NewLNBitsPayments(cfg.LNBitsBaseURL, cfg.LNBitsInvoiceAPIKey, cfg.LNBitsReadOnlyAPIKey)
	if err != nil {
		log.Fatal(err)
	}

	svc, err := fapkit.NewService(cfg, fapkit.Dependencies{
		Store:    st,
		Payments: p,
		Clock:    fapkit.SystemClock(),
	})
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	faphttp.RegisterRoutes(mux, svc, faphttp.HTTPOptions{
		WebhookSecret: cfg.WebhookSecret,
	})

	log.Fatal(http.ListenAndServe(cfg.HTTPAddr, mux))
}
```

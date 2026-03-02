# syntax=docker/dockerfile:1.7

FROM golang:1.26.0-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    for attempt in 1 2 3 4 5; do /usr/local/go/bin/go mod download && exit 0; sleep $((attempt * 2)); done; exit 1

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/fapd ./cmd/fapd

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app

COPY --from=builder /out/fapd /app/fapd
COPY ops /app/ops

VOLUME ["/var/lib/fap"]
EXPOSE 8080

CMD ["/app/fapd"]

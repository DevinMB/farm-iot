# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

RUN apk --no-cache add git

# Build hub daemon for all Pi architectures
# go mod tidy regenerates go.sum (needed when deps change)
WORKDIR /hub
COPY hub/ .
RUN go mod tidy \
 && CGO_ENABLED=0 GOOS=linux GOARCH=arm   GOARM=6 go build -ldflags="-s -w" -o /hub-daemon-armv6 . \
 && CGO_ENABLED=0 GOOS=linux GOARCH=arm   GOARM=7 go build -ldflags="-s -w" -o /hub-daemon-armv7 . \
 && CGO_ENABLED=0 GOOS=linux GOARCH=arm64         go build -ldflags="-s -w" -o /hub-daemon-arm64 .

# Build API (amd64)
WORKDIR /api
COPY api/go.mod api/go.sum ./
RUN go mod download
COPY api/ .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /api-server ./cmd/api

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /api-server .
COPY --from=builder /hub-daemon-armv6 ./hub-daemon-armv6
COPY --from=builder /hub-daemon-armv7 ./hub-daemon-armv7
COPY --from=builder /hub-daemon-arm64 ./hub-daemon-arm64

EXPOSE 8080
ENTRYPOINT ["/app/api-server"]

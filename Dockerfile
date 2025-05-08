# Use Debian-based image instead of Alpine to avoid musl libc compatibility issues
FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

RUN go install github.com/pressly/goose/v3/cmd/goose@latest

COPY . .

# Build with CGO enabled 
RUN CGO_ENABLED=1 go build -o webhook-delivery-service ./cmd/server

# ---

# Use a Debian-based image for the runtime
FROM debian:bookworm-slim

WORKDIR /app

# Install minimal runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /go/bin/goose /usr/local/bin/goose
COPY --from=builder /app/webhook-delivery-service .
COPY web web
COPY internal/sql/schema migrations
COPY docker-entrypoint.sh .

# Make sure the entrypoint script is executable
RUN chmod +x ./docker-entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["./docker-entrypoint.sh"]
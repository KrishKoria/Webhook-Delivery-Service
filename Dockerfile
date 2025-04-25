FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go install github.com/pressly/goose/v3/cmd/goose@latest
RUN CGO_ENABLED=0 go build -o webhook-delivery-service ./cmd/server

# ---

FROM alpine:3.21

WORKDIR /app

COPY --from=builder /go/bin/goose /usr/local/bin/goose
COPY --from=builder /app/webhook-delivery-service .
COPY web web
COPY internal/sql/schema migrations
COPY docker-entrypoint.sh .

EXPOSE 8080

ENTRYPOINT ["./docker-entrypoint.sh"]
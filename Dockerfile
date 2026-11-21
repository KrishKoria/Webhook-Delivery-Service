# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.27.4-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Pure-Go build: tursogo-serverless and modernc.org/sqlite need no CGO.
# Templates, static assets and migrations are embedded in the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/webhook-delivery-service ./cmd/server

# ---- runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/webhook-delivery-service /webhook-delivery-service
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/webhook-delivery-service"]

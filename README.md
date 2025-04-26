# Webhook Delivery Service

## Features
- Subscription CRUD (API & UI) with secret and event type filtering.
- Webhook ingestion endpoint with HMAC signature verification.
- Asynchronous delivery worker with exponential backoff retries.
- Delivery attempt logging, analytics, and retention (default: 72h).
- Redis-backed subscription caching (supports Upstash, Redis Cloud, etc.).
- Minimal UI for managing, testing, and analyzing subscriptions.
- Fully containerized with Docker and Docker Compose.
- Cloud-ready (Google Cloud Run, Fly.io, Railway, etc.).
- Health check endpoint (`/healthz`) for monitoring.
- Dead Letter Queue for failed deliveries (manual retry via UI).
---

## Setup & Run Locally (Docker)

1. **Clone the repository:**
   ```sh
   git clone https://github.com/KrishKoria/Webhook-Delivery-Service.git
   cd Webhook-Delivery-Service
   ```

2. **Copy and edit the example environment file:**
   ```sh
   cp .env.example .env
   ```
   - Set `DATABASE_URL` (Turso, SQLite, or Postgres) and `REDIS_URL` (Upstash, Redis Cloud, etc.) in `.env`.

3. **Build and start the service:**
   ```sh
   docker-compose up --build
   ```

4. **Access the app:**
   - Open [http://localhost:8080](http://localhost:8080) (redirects to `/ui/subscriptions`).

---

## Live Demo

[Live Application Link](https://webhook-delivery-110005729669.us-central1.run.app/ui/subscriptions)

---

## Architecture Choices

- **Framework:** Go + Gin for HTTP API and UI (fast, minimal, robust).
- **Database:** Turso (production, serverless SQLite) or SQLite (local/dev).
- **Async Task/Queue:** In-process Go worker, DB-backed queue for reliability.
- **Retry Strategy:** Exponential backoff (10s, 30s, 1m, 5m, 15m), max 5 attempts.
- **Caching:** Redis (Upstash/Redis Cloud) for subscription details.
- **Containerization:** Docker, orchestrated with Docker Compose.
- **Cloud Ready:** Deployable to Google Cloud Run, Fly.io, Railway, etc.
- **Dead Letter Queue:** Failed deliveries after max retries are moved to a DLQ for manual review/retry.
- **Observability:** Health check endpoint, structured logging

---

## Database Schema & Indexing

- **subscriptions:**  
  `id` (PK, UUID), `target_url`, `secret`, `event_types`, `created_at`, `updated_at`
- **delivery_tasks:**  
  `id` (PK, UUID), `subscription_id` (FK), `payload`, `status`, `created_at`, `updated_at`, `next_attempt_at`, `attempt_count`
- **delivery_logs:**  
  `id` (PK, UUID), `delivery_task_id` (FK), `subscription_id` (FK), `target_url`, `timestamp`, `attempt_number`, `outcome`, `http_status`, `error_details`
- **dead_letter_tasks:**  
  `id` (PK, UUID), `original_task_id`, `subscription_id`, `payload`, `failed_at`, `reason`
- **Indexes:**  
  On `subscription_id`, `delivery_task_id`, and `next_attempt_at` for efficient lookups and scheduling.

---

## Sample API Usage

### Create a Subscription
```bash
curl -X POST http://localhost:8080/subscriptions \
  -H "Content-Type: application/json" \
  -d '{"target_url":"https://webhook.site/your-url","secret":"mysecret","event_types":"order.created,user.updated"}'
```

### List Subscriptions
```bash
curl http://localhost:8080/subscriptions
```

### Update a Subscription
```bash
curl -X PUT http://localhost:8080/subscriptions/<id> \
  -H "Content-Type: application/json" \
  -d '{"target_url":"https://webhook.site/your-url","secret":"newsecret","event_types":"order.created"}'
```

### Delete a Subscription
```bash
curl -X DELETE http://localhost:8080/subscriptions/<id>
```

### Ingest a Webhook
```bash
curl -X POST http://localhost:8080/ingest/<subscription_id> \
  -H "Content-Type: application/json" \
  -H "X-Event-Type: order.created" \
  -H "X-Hub-Signature-256: sha256=<hmac>" \
  -d '{"event":"test"}'
```

### Check Delivery Status
```bash
curl http://localhost:8080/deliveries/<delivery_task_id>
```

### List Recent Deliveries for a Subscription
```bash
curl http://localhost:8080/subscriptions/<id>/deliveries
```

---

## Redis Caching

- Uses Redis for subscription caching (Upstash, Redis Cloud, etc.).
- Set `REDIS_URL` in your `.env` (e.g., `rediss://:<password>@<host>:6379` for Upstash).
- No local Redis container is needed if using a cloud provider.
- Fallback to in-memory cache if Redis is not configured (for local/dev).

---

## Cost Estimation

- **Compute:** 1x small container (Cloud Run, Fly.io, Railway, etc.) ≈ $0/month (free tier)
- **Database:** Turso (free tier) or SQLite (local) ≈ $0/month
- **Redis:** Upstash free tier ≈ $0/month
- **Bandwidth:** Minimal, well within free tier limits
- **Total:** ≈ $0/month on free tier, with headroom for moderate traffic

---

## Assumptions

- All secrets and sensitive data are managed via environment variables.
- The service is stateless and horizontally scalable.
- Turso is used for production DB; SQLite for local/dev.
- Delivery attempts are idempotent and safe to retry.
- Redis is optional for local/dev, required for production caching.
- Dead Letter Queue is enabled for failed deliveries.

---

## Deployment

- **Google Cloud Run:**  
  See the README section "Deploying to Google Cloud Run" for step-by-step instructions.
- **Upstash Redis:**  
  Use the provided `REDIS_URL` from Upstash in your `.env`.
- **Docker Hub:**  
  Build and push your image:
  ```sh
  docker build -t <your-dockerhub-username>/webhook-delivery-service:latest .
  docker push <your-dockerhub-username>/webhook-delivery-service:latest
  ```

---

## Special Features

- **Dead Letter Queue:**  
  Failed deliveries after max retries are moved to a DLQ for manual review and redelivery.
- **Manual Redelivery:**  
  UI allows retrying failed deliveries from the DLQ.
- **Health Check:**  
  `/healthz` endpoint for monitoring and orchestration.

---

## Credits

- [Gin](https://github.com/gin-gonic/gin)
- [sqlc](https://github.com/kyleconroy/sqlc)
- [Goose](https://github.com/pressly/goose)
- [Upstash](https://upstash.com/) for managed Redis
- Docker & Docker Compose
- [Turso](https://turso.tech/) for serverless SQLite
---

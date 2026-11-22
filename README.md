 # Webhook Delivery Service

 ## Features
 - Subscription CRUD (API & UI) with secret and event type filtering.
 - Webhook ingestion endpoint with HMAC signature verification.
 - Asynchronous delivery worker with exponential backoff retries.
 - Scheduled webhook delivery with recurrence (none, daily, weekly, monthly).
 - Delivery attempt logging, analytics, and retention 
 - Redis-backed subscription caching (supports Upstash, Redis Cloud, etc.).
 - Minimal UI for managing, testing, and analyzing subscriptions.
 - Fully containerized with Docker and Docker Compose.
 - Cloud-ready (Google Cloud Run, Fly.io, Railway, etc.).
 - Liveness and readiness probes (`/livez`, `/readyz`) plus detailed health and metrics endpoints.
 - Dead Letter Queue for failed deliveries (manual retry via UI).
 ---

## Setup & Run Locally (Docker)

**Prerequisites:**
- Docker installed
- Docker Compose installed
- Turso CLI (optional, for local SQLite or Turso Cloud setup)

 1. **Clone the repository:**
    ```bash
    git clone https://github.com/KrishKoria/Webhook-Delivery-Service.git
    cd Webhook-Delivery-Service
    ```

 2. **Copy and edit the example environment file:**
    ```bash
    cp .env.example .env
    ```
    - Configure your database settings in the `.env` file. Refer to the **"Environment Variables"** section below for details.
    
      - For `TURSO_DATABASE_URL` and `TURSO_AUTH_TOKEN`, see the **"Local Database Setup"** section below for guidance on SQLite (local) or Turso Cloud.
      - For `REDIS_URL`:
        - **Local Docker Development:** This is now optional. If `REDIS_URL` is not set or is empty in your `.env` file, the application will default to using the Redis service defined in `docker-compose.yml` (`redis://redis:6379/0`).
        - **External Redis (e.g., Upstash):** Set `REDIS_URL` in your `.env` file to connect to your cloud Redis provider. See the **"Redis Caching"** section.

 3. **Build and start the service (includes Redis):**
    ```bash
    docker-compose up --build
    ```
    This command will also start a local Redis container.


 4. **Access the app:**
    - Open [http://localhost:8080](http://localhost:8080) (redirects to the `/ui/dashboard` overview). Sign in with `ADMIN_USERNAME` / `ADMIN_PASSWORD`.

 ---

 ## Live Demo

 [Live Application Link](https://webhook-delivery-110005729669.us-central1.run.app/ui/subscriptions)

---

## API Documentation

The API is described by an OpenAPI 3.1 specification: [`doc/api-documentation.yml`](doc/api-documentation.yml). Open it in any OpenAPI viewer (Swagger Editor, Redocly, Scalar) to browse the endpoints, schemas and examples.

---

## Environment Variables

`.env.example` lists every variable with its default. The ones that matter most:

| Variable | Default | Purpose |
|---|---|---|
| `ADMIN_USERNAME`, `ADMIN_PASSWORD` | **required** | HTTP Basic credentials for `/api/*` and `/ui/*`. The server **refuses to start** without them. |
| `ADMIN_AUTH_DISABLED` | `false` | Local development only: opens every admin route (logs a warning). |
| `TURSO_DATABASE_URL` | | `libsql://`, `turso://` or `https://` for Turso; `file:./dev.db` for local SQLite. |
| `TURSO_AUTH_TOKEN` | | Turso token (empty for `file:`). |
| `MIGRATE_ON_START` | `true` | Apply embedded goose migrations at boot. Set `false` and run `webhook-delivery-service migrate` when several instances share a DB. |
| `REDIS_URL` | | Optional subscription cache. Unset means no cache (the DB is the source of truth). |
| `PORT` | `8080` | HTTP port. |
| `TRUSTED_PROXIES` | none | Proxy IPs/CIDRs whose `X-Forwarded-For` is trusted for rate limiting. |
| `CSRF_TRUSTED_ORIGINS` | none | Extra origins allowed to make state-changing admin requests. |
| `SHUTDOWN_TIMEOUT` | `9s` | Graceful shutdown budget (Cloud Run gives 10s after SIGTERM). |
| `WEBHOOK_ALLOW_PRIVATE_IPS` | `false` | Allow delivery to loopback/private/link-local targets (SSRF guard). Local testing only. |
| `WEBHOOK_TIMEOUT_SECONDS` | `30` | Per-attempt outbound timeout. |
| `WORKER_CONCURRENCY` | `10` | Parallel deliveries per instance. |
| `WORKER_MAX_ATTEMPTS` | `6` | Attempts before a task moves to the dead letter queue. |
| `INGEST_MAX_BODY_SIZE` | `1048576` | Max ingest body in bytes (413 above it). |
| `INGEST_RATE_LIMIT_RPS` / `_BURST` | `10` / `20` | Per-client ingest rate limit (429 with `Retry-After`). |
| `LOG_RETENTION_HOURS` | `72` | Delivery log retention. |
| `TASK_RETENTION_DAYS` | `7` | Retention for finished delivery tasks. |
| `DLQ_RETRIED_RETENTION_DAYS` | `30` | Retention for dead letters that were already retried. |

---

 ## Architecture Choices

 - **Framework:** Go + Gin for HTTP API and UI (fast, minimal, robust).
 - **Database:** Turso (production, serverless SQLite) or SQLite (local/dev).
 - **Async Task/Queue:** In-process Go worker, DB-backed queue for reliability.
 - **Scheduled Worker:** Separate Go worker for scheduled webhooks, enqueues delivery tasks at the scheduled time.
 - **Delivery engine:** DB-backed queue; workers claim batches with a lease and heartbeat, so several instances can share one database without double delivery.
 - **Retry Strategy:** Backoff of 10s, 30s, 1m, 5m, 15m with ±20% jitter: 6 attempts in total, then the dead letter queue. A receiver's `Retry-After` is honoured (capped at 1h); `410 Gone` dead-letters immediately.
 - **Signing:** Outbound requests follow [Standard Webhooks](https://www.standardwebhooks.com/) (`webhook-id`, `webhook-timestamp`, `webhook-signature`).
 - **Security:** Fail-closed admin auth, CSRF protection, strict CSP on the UI, SSRF validation of target URLs, ingest size and rate limits.
 - **Caching:** Redis (Upstash/Redis Cloud) for subscription details.
 - **Containerization:** Docker, orchestrated with Docker Compose.
 - **Cloud Ready:** Deployable to Google Cloud Run, Fly.io, Railway, etc.
 - **Dead Letter Queue:** Failed deliveries after max retries are moved to a DLQ for manual review/retry.
 - **Observability:** `/livez` and `/readyz` probes, `/api/health`, `/api/metrics`, structured JSON logging (`log/slog`).

 ---
## Redis Caching

- The application uses Redis for subscription caching.
- **Local Docker Development:** `docker-compose.yml` includes a Redis service. If `REDIS_URL` is not set or is empty in your `.env` file, the application will automatically connect to this local Redis service (`redis://redis:6379/0`).
- **External Redis (Upstash, Redis Cloud, etc.):**
    - To use an external Redis provider, set the `REDIS_URL` in your `.env` file (e.g., `rediss://:<password>@<host>:6379` for Upstash). This will override the local Docker Redis configuration.
    - No separate local Redis container is needed if you are using a cloud provider.
---

 ## Local Database Setup

While **Turso Cloud is the recommended method** for a more robust development experience (see below), you can use a local SQLite file for quick local testing, especially when running with Docker.

### Using a Local SQLite File with Docker (for quick testing, not recommended for data persistence)

When you run the application using `docker-compose up`, you can configure it to use a local SQLite database file. This file will be created *inside* the Docker container.

1.  **Configure your `.env` file:**
    Make sure your `.env` file (copied from `.env.example`) has the following settings:
    ```bash
    TURSO_DATABASE_URL=file:local.db
    TURSO_AUTH_TOKEN=
    # ... other variables like REDIS_URL, PORT ...
    ```
    *   `TURSO_DATABASE_URL=file:local.db`: This tells the application to use/create a SQLite database file named `local.db`.
    *   `TURSO_AUTH_TOKEN=`: This **must be empty** for local SQLite file usage.

2.  **How it works with Docker:**
    *   When you run `docker-compose up --build`, the application inside the Docker container will use `local.db`.
    *   The database migrations will be applied to this `local.db` file within the container.
    *   **Important:** By default, this `local.db` file exists only within the Docker container's filesystem. If you run `docker-compose down` (which removes the container), this `local.db` file and all its data will be **lost**.

3.  **(Optional) Persisting `local.db` with Docker Volumes:**
    If you need the `local.db` data to persist across container restarts or `docker-compose down/up` cycles, you would need to modify your `docker-compose.yml` to map a volume to where `local.db` is stored within the container (e.g., `/app/local.db` if your `WORKDIR` is `/app`). This is a more advanced Docker setup.
    Example snippet for `docker-compose.yml`:
    ```yaml
    services:
      app:
        # ... other app configurations ...
        volumes:
          - ./data/sqlite:/app/data # Assuming local.db is created at /app/data/local.db
                                    # or adjust path as needed.
                                    # Ensure TURSO_DATABASE_URL=file:/app/data/local.db
    ```
    *Note: If you use a volume, ensure the path in `TURSO_DATABASE_URL` matches the path inside the container where the volume is mounted, e.g., `TURSO_DATABASE_URL=file:/app/data/local.db`.*

**Limitations of local SQLite file with Docker (without persistent volumes):**
*   Data is ephemeral and lost when the container is removed.
*   Accessing and inspecting the `local.db` file directly from your host machine is more complex as it resides inside the container.

For these reasons, for any development beyond very quick, isolated tests, using Turso Cloud is strongly recommended.

### Using Turso Cloud (Recommended Method)

1.  **Create a Turso account:**
    Sign up at [Turso](https://turso.tech/).

2.  **Install the Turso CLI and create a database:**
    Follow the Turso documentation to install their CLI and create a new database.

3.  **Get your database URL and auth token:**
    The Turso CLI will provide you with a database URL (e.g., `libsql://your-db-name-username.turso.io`) and you can generate an auth token.

4.  **Set these in your `.env` file:**
    ```bash
    TURSO_DATABASE_URL=<your_turso_database_url_from_cli>
    TURSO_AUTH_TOKEN=<your_turso_auth_token_from_cli>
    # ... other variables ...
    ```

> **Important Note on Database URLs:**
> The database URL provided to the application (whether local or remote) must start with a valid scheme like `file:`, `libsql:`, or `https:`.
> If you use just a filename (like `local.db`) without the `file:` prefix for `TURSO_DATABASE_URL`, it will likely not work correctly with the database driver. Always use the correct scheme!

---

 ## Database Schema & Indexing

 - **subscriptions:**  
   `id` (PK, UUID), `target_url`, `secret`, `event_types`, `created_at`, `updated_at`
 - **delivery_tasks:**  
   `id` (PK, UUID), `subscription_id` (FK), `payload`, `status`, `created_at`, `last_attempt_at`, `attempt_count`
 - **delivery_logs:**  
   `id` (PK, UUID), `delivery_task_id` (FK), `subscription_id` (FK), `target_url`, `timestamp`, `attempt_number`, `outcome`, `http_status`, `error_details`
 - **scheduled_webhooks:**  
   `id` (PK, UUID), `subscription_id` (FK), `payload`, `scheduled_for`, `recurrence`, `status`, `created_at`, `updated_at`
 - **dead_letter_tasks:**  
   `id` (PK, UUID), `original_task_id`, `subscription_id`, `payload`, `failed_at`, `reason`
 - **Indexes:**  
   On `subscription_id`, `delivery_task_id`, `scheduled_for`, and `next_attempt_at` for efficient lookups and scheduling.

 ---

 ## Sample API Usage

Admin endpoints live under `/api` and need HTTP Basic auth. Requests with a body must be `application/json`.

 ### Create a Subscription
 ```bash
 curl -u "$ADMIN_USERNAME:$ADMIN_PASSWORD" -X POST http://localhost:8080/api/subscriptions \
   -H "Content-Type: application/json" \
   -d '{"target_url":"https://webhook.site/your-url","secret":"mysecret","event_types":"order.created,user.updated"}'
 ```

 ### List, Update, Delete
 ```bash
 curl -u "$ADMIN_USERNAME:$ADMIN_PASSWORD" http://localhost:8080/api/subscriptions
 curl -u "$ADMIN_USERNAME:$ADMIN_PASSWORD" -X PUT http://localhost:8080/api/subscriptions/<id> \
   -H "Content-Type: application/json" -d '{"target_url":"https://webhook.site/new-url"}'
 curl -u "$ADMIN_USERNAME:$ADMIN_PASSWORD" -X DELETE http://localhost:8080/api/subscriptions/<id>
 ```
 Omitting `secret` on update keeps the current one; send `"clear_secret": true` to remove it.

 ### Ingest a Webhook (public, no admin auth)
 ```bash
 BODY='{"event":"test"}'
 SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac mysecret | sed 's/^.* //')"
 curl -X POST http://localhost:8080/ingest/<subscription_id> \
   -H "Content-Type: application/json" \
   -H "X-Event-Type: order.created" \
   -H "X-Hub-Signature-256: $SIG" \
   -H "Idempotency-Key: order-42" \
   -d "$BODY"
 ```
 Returns `202` with a `task_id`. Repeating the same `Idempotency-Key` returns `200` with the original `task_id` instead of queueing twice.

 ### Verify a Delivery (receiver side)
 Each delivery carries `webhook-id`, `webhook-timestamp` and `webhook-signature: v1,<base64>`. The signature is HMAC-SHA256 over `{webhook-id}.{webhook-timestamp}.{raw body}`. The HMAC key depends on the secret's format:

 | Secret | HMAC key |
 |---|---|
 | `whsec_<base64>` (what `rotate-secret` issues) | the **base64-decoded** bytes after `whsec_`, as in the Standard Webhooks spec |
 | anything else (a secret you chose, or an older hex secret) | the secret's UTF-8 bytes |

 With a `whsec_` secret you can use an official [Standard Webhooks library](https://github.com/standard-webhooks/standard-webhooks) as is. By hand:
 ```python
 def signing_key(secret: str) -> bytes:
     if secret.startswith("whsec_"):
         return base64.b64decode(secret[len("whsec_"):])
     return secret.encode()

 expected = "v1," + base64.b64encode(hmac.new(signing_key(secret), f"{wid}.{ts}.".encode() + body, hashlib.sha256).digest()).decode()
 ok = any(hmac.compare_digest(expected, s) for s in signature_header.split())
 ```
 After rotating to a `whsec_` secret, update the receiver's key handling too, or every check fails. Inbound `X-Hub-Signature-256` on `/ingest` always uses the secret text itself as the key.
 Reject timestamps more than a few minutes old to stop replays, and de-duplicate on `webhook-id` (it stays the same across retries).

 ### Schedule a Webhook
 In the UI open a subscription's **Scheduled** tab, or `POST /api/scheduled` with `subscription_id`, `payload`, `scheduled_for` (RFC 3339) and `recurrence` (`none`, `daily`, `weekly`, `monthly`).

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
 - Redis is optional everywhere; without it every lookup goes to the database.
 - Dead Letter Queue is enabled for failed deliveries.
 - Scheduled webhooks are enqueued as delivery tasks at the scheduled time.

 ---

 ## Deployment

 - **Google Cloud Run:**  
   See the README section "Deploying to Google Cloud Run" for step-by-step instructions.
 - **Upstash Redis:**  
   Use the provided `REDIS_URL` from Upstash in your `.env`.
 - **Docker Hub:**  
   Build and push your image:
   ```bash
   docker build -t <your-dockerhub-username>/webhook-delivery-service:latest .
   docker push <your-dockerhub-username>/webhook-delivery-service:latest
   ```

 ---

 ## Special Features

 - **Scheduled Webhooks:**  
   Schedule one-time or recurring webhooks (daily, weekly, monthly) via the UI.  
   Scheduled worker enqueues delivery tasks at the correct time.
 - **Dead Letter Queue:**  
   Failed deliveries after max retries are moved to a DLQ for manual review and redelivery.
 - **Manual Redelivery:**  
   UI allows retrying failed deliveries from the DLQ.
 - **Health Checks:**  
   `/livez` (process up) and `/readyz` (database reachable, 503 otherwise) for orchestration; `/healthz` is kept as an alias of `/livez`.

 ---

 ## Deploying to Google Cloud Run

 You can deploy this service to [Google Cloud Run](https://cloud.google.com/run) in just a few steps:

 ### 1. **Build and Push Your Docker Image**

 Replace `<YOUR_PROJECT_ID>` and `<YOUR_IMAGE_NAME>` with your values.

 ```bash
 # Authenticate Docker with Google
 gcloud auth configure-docker

 # Build your image
 docker build -t gcr.io/<YOUR_PROJECT_ID>/<YOUR_IMAGE_NAME>:latest .

 # Push to Google Container Registry
 docker push gcr.io/<YOUR_PROJECT_ID>/<YOUR_IMAGE_NAME>:latest
 ```
 ### 2. **Deploy to Cloud Run**

 ```bash

 gcloud run deploy webhook-delivery-service \
   --image gcr.io/<YOUR_PROJECT_ID>/<YOUR_IMAGE_NAME>:latest \
   --platform managed \
   --region <YOUR_REGION> \
   --allow-unauthenticated \
   --set-env-vars TURSO_DATABASE_URL=<YOUR_DATABASE_URL>,REDIS_URL=<YOUR_REDIS_URL>,ADMIN_USERNAME=<USER> \
   --set-secrets TURSO_AUTH_TOKEN=turso-token:latest,ADMIN_PASSWORD=admin-password:latest
 ```

 > **The service will not start without `ADMIN_USERNAME` and `ADMIN_PASSWORD`.** Set them before deploying a new revision, otherwise the revision fails its startup probe. Prefer Secret Manager (`--set-secrets`) for the password and Turso token. `--allow-unauthenticated` only opens the network path: `/ingest` stays public, while `/api` and `/ui` still require the admin credentials.

 ### 3. **Access the Service**
 After deployment, you will receive a URL for your service. You can access the UI and API at 
 ```bash
 https://<your-cloud-run-url>/ui/dashboard
 ```

 ---

 ## Credits

 - [Gin](https://github.com/gin-gonic/gin)
 - [sqlc](https://github.com/kyleconroy/sqlc)
 - [Goose](https://github.com/pressly/goose)
 - [Upstash](https://upstash.com/) for managed Redis
 - Docker & Docker Compose
 - [Turso](https://turso.tech/) for serverless SQLite
 ---
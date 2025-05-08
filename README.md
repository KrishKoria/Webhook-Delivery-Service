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
 - Health check endpoint (`/healthz`) for monitoring.
 - Dead Letter Queue for failed deliveries (manual retry via UI).
 ---

 ## Setup & Run Locally (Docker)

**Prerequisites:**
- Docker installed
- Docker Compose installed
- Redis (optional, for caching)
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
    - Configure your database and Redis settings in the `.env` file, Refer to the **"Environment Variables"** section below for details.
    
      - For `TURSO_DATABASE_URL` and `TURSO_AUTH_TOKEN`, see the **"Local Database Setup"** section below for guidance on SQLite (local) or Turso Cloud.
      - For `REDIS_URL`, refer to the **"Redis Caching"** section if you plan to use Redis.

 3. **Build and start the service:**
    ```bash
    docker-compose up --build
    ```

 4. **Access the app:**
    - Open [http://localhost:8080](http://localhost:8080) (redirects to `/ui/subscriptions`).

 ---

 ## Live Demo

 [Live Application Link](https://webhook-delivery-110005729669.us-central1.run.app/ui/subscriptions)

---

## Environment Variables

The application uses the following environment variables for configuration:

- `TURSO_DATABASE_URL`: The connection URL for your Turso database (or `file:local.db` for local SQLite).
- `TURSO_AUTH_TOKEN`: The authentication token for your Turso database (can be empty for local SQLite).
- `REDIS_URL`: The connection URL for your Redis instance (e.g., `redis://localhost:6379/0` or a cloud provider URL).
- `PORT`: (Optional) The port on which the HTTP server will listen. Defaults to `8080`.

---

 ## Architecture Choices

 - **Framework:** Go + Gin for HTTP API and UI (fast, minimal, robust).
 - **Database:** Turso (production, serverless SQLite) or SQLite (local/dev).
 - **Async Task/Queue:** In-process Go worker, DB-backed queue for reliability.
 - **Scheduled Worker:** Separate Go worker for scheduled webhooks, enqueues delivery tasks at the scheduled time.
 - **Retry Strategy:** Exponential backoff (10s, 30s, 1m, 5m, 15m), max 5 attempts.
 - **Caching:** Redis (Upstash/Redis Cloud) for subscription details.
 - **Containerization:** Docker, orchestrated with Docker Compose.
 - **Cloud Ready:** Deployable to Google Cloud Run, Fly.io, Railway, etc.
 - **Dead Letter Queue:** Failed deliveries after max retries are moved to a DLQ for manual review/retry.
 - **Observability:** Health check endpoint, structured logging

 ---

 ## Local Database Setup

For local development, you should use a SQLite file.

In your `.env` file, set:
```bash
TURSO_DATABASE_URL=file:local.db
TURSO_AUTH_TOKEN=
```
This tells the app to use a local SQLite database file named `local.db`.
You can leave `TURSO_AUTH_TOKEN` blank for local SQLite.

### If you want to use Turso Cloud:

1. Create a Turso account
2. Create a database and get your connection URL and auth token from the Turso dashboard.

Then set these in your `.env` file:
```bash
TURSO_DATABASE_URL=<your_turso_database_url>
TURSO_AUTH_TOKEN=<your_turso_auth_token>
```
> Note:
> The database URL must start with a valid scheme like `libsql:`, `https:`, etc.
> If you use just a filename (like `local.db`), it will not work. Always use the correct scheme!

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
  -d '{"target_url":"https://webhook.site/your-new-url","secret":"newsecret","event_types":"order.updated,user.deleted"}'
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

 ### Schedule a Webhook (UI)
 - Go to the subscription's "Schedule New" action in the UI.
 - Fill out the form (payload, time, recurrence).
 - The scheduled webhook will be delivered at the specified time and logged.

 ---

 ## Redis Caching

 - Uses Redis for subscription caching (Upstash, Redis Cloud, etc.).
 - Set `REDIS_URL` in your `.env` (e.g., `rediss://:<password>@<host>:6379` for Upstash).
 - No local Redis container is needed if using a cloud provider.
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
 - **Health Check:**  
   `/healthz` endpoint for monitoring and orchestration.

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
   --set-env-vars TURSO_DATABASE_URL=<YOUR_DATABASE_URL>,TURSO_AUTH_TOKEN=<YOUR_AUTH_TOKEN>,REDIS_URL=<YOUR_REDIS_URL>
 ```

 ### 3. **Access the Service**
 After deployment, you will receive a URL for your service. You can access the UI and API at 
 ```bash
 https://<your-cloud-run-url>/ui/subscriptions
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
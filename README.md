 # Webhook Delivery Service
 
 ## Setup & Run Locally (Docker)
 1.  Clone the repository:
     git clone https://github.com/KrishKoria/Webhook-Delivery-Service.git
     cd Webhook-Delivery-Service
 2.  Copy the example environment file and edit as needed:
     cp .env.example .env
 3.  Build and start the services:
     docker-compose up --build
 4.  The app will be available at http://localhost:8080
 
 ## Live Demo
  [Live Application Link](https:your-live-app-url.com)
 
 ## Architecture Choices
 -  **Framework:** Go with Gin for HTTP API and UI (fast, minimal, robust).
 -  **Database:** SQLite (local/dev) or PostgreSQL (production-ready, ACID).
 -  **Async Task/Queue:** In-process Go worker with DB-backed queue for simplicity and reliability.
 -  **Retry Strategy:** Exponential backoff (10s, 30s, 1m, 5m, 15m), max 5 attempts, status tracked in DB.
 -  **Caching:** In-memory cache for subscription details to reduce DB load.
 
 ## Database Schema & Indexing
 -  **subscriptions:** id (PK, UUID), target_url, secret, event_types, created_at, updated_at
 -  **delivery_tasks:** id (PK, UUID), subscription_id (FK), payload, status, created_at, updated_at, next_attempt_at, attempt_count
 -  **delivery_logs:** id (PK, UUID), delivery_task_id (FK), subscription_id (FK), target_url, timestamp, attempt_number, outcome, http_status, error_details
 -  **Indexes:** On subscription_id, delivery_task_id, and next_attempt_at for efficient lookups and scheduling.
 
 ## Sample API Usage
 
  ### Create a Subscription
  curl -X POST http://localhost:8080/subscriptions \
    -H "Content-Type: application/json" \
    -d '{"target_url":"https://webhook.site/your-url","secret":"mysecret","event_types":"order.created,user.updated"}'
 
  ### List Subscriptions
  curl http://localhost:8080/subscriptions
 
  ### Update a Subscription
  curl -X PUT http://localhost:8080/subscriptions/<id> \
    -H "Content-Type: application/json" \
    -d '{"target_url":"https://webhook.site/your-url","secret":"newsecret","event_types":"order.created"}'
 
  ### Delete a Subscription
  curl -X DELETE http://localhost:8080/subscriptions/<id>
 
  ### Ingest a Webhook
  curl -X POST http://localhost:8080/ingest/<subscription_id> \
    -H "Content-Type: application/json" \
    -H "X-Event-Type: order.created" \
    -H "X-Hub-Signature-256: sha256=<hmac>" \
    -d '{"event":"test"}'
 
  ### Check Delivery Status
  curl http://localhost:8080/deliveries/<delivery_task_id>
 
  ### List Recent Deliveries for a Subscription
  curl http://localhost:8080/subscriptions/<id>/deliveries
 
 ## Cost Estimation (Free Tier, 24x7, ~5000 webhooks/day, avg 1.2 attempts/webhook)
 -  **Compute:** 1x small VM/container (e.g., Fly.io, Render, Railway, or AWS/GCP/Heroku free tier) ≈ $0/month (free tier)
 -  **Database:** SQLite (local) or managed Postgres (free tier, e.g., Supabase, Neon, Render) ≈ $0/month
 -  **Bandwidth:** Minimal, well within free tier limits
 -  **Total:** ≈ $0/month on free tier, with headroom for moderate traffic
 
 ## Assumptions
 -  All secrets and sensitive data are managed via environment variables.
 -  The service is run on a single instance (stateless, can be scaled horizontally).
 -  SQLite is used for local/dev; Postgres recommended for production.
 -  Delivery attempts are idempotent and safe to retry.
 
 ## Credits
 -  Gin (https://github.com/gin-gonic/gin)
 -  sqlc (https://github.com/kyleconroy/sqlc)
 -  Docker & Docker Compose
 -  [webhook.site](https://webhook.site) for testing
 -  GitHub Copilot & OpenAI for code suggestions
 
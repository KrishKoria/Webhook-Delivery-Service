#!/bin/sh
set -e

# Run goose migrations (adjust driver if needed: sqlite3, postgres, etc.)
goose -dir ./migrations turso "$TURSO_DATABASE_URL?authToken=$TURSO_AUTH_TOKEN" up

# Start the app
./webhook-delivery-service
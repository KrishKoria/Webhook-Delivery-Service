#!/bin/bash
# Cloud Run optimized entrypoint - starts server immediately for health checks

echo "=== Container Starting ==="
echo "PORT=${PORT:-8080}"
echo "TURSO_DATABASE_URL is $([ -n "$TURSO_DATABASE_URL" ] && echo "SET" || echo "NOT SET")"
echo "TURSO_AUTH_TOKEN is $([ -n "$TURSO_AUTH_TOKEN" ] && echo "SET" || echo "NOT SET")"

# Skip migrations in Cloud Run - run them separately if needed
# The app will fail fast if DB connection is wrong anyway
if [[ "$SKIP_MIGRATIONS" != "true" && -d "./migrations" && -n "$(ls -A ./migrations 2>/dev/null)" ]]; then
    echo "Running migrations (set SKIP_MIGRATIONS=true to skip)..."
    
    # Build connection string for goose
    DB_CONN_STR="$TURSO_DATABASE_URL"
    if [[ -n "$TURSO_AUTH_TOKEN" ]]; then
        if [[ "$TURSO_DATABASE_URL" == libsql://* ]]; then
            # For remote Turso - use libsql:// format with authToken
            if [[ "$DB_CONN_STR" == *"?"* ]]; then
                DB_CONN_STR="$DB_CONN_STR&authToken=$TURSO_AUTH_TOKEN"
            else
                DB_CONN_STR="$DB_CONN_STR?authToken=$TURSO_AUTH_TOKEN"
            fi
        else
            # For other URL types (https://, file:, etc.)
            if [[ "$DB_CONN_STR" == *"?"* ]]; then
                DB_CONN_STR="$DB_CONN_STR&authToken=$TURSO_AUTH_TOKEN"
            else
                DB_CONN_STR="$DB_CONN_STR?authToken=$TURSO_AUTH_TOKEN"
            fi
        fi
    fi
    
    # Run goose with timeout to prevent hanging
    timeout 30s goose -dir ./migrations turso "$DB_CONN_STR" up 2>&1 || echo "Migration warning: $?"
else
    echo "Skipping migrations (SKIP_MIGRATIONS=$SKIP_MIGRATIONS)"
fi

# Start the app immediately - this binds to PORT for Cloud Run health check
echo "=== Starting Server on port ${PORT:-8080} ==="
exec ./webhook-delivery-service
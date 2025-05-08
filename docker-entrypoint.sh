#!/bin/bash
set -e

# Start with the base URL
DB_CONN_STR="$TURSO_DATABASE_URL"

# Handle the authentication for different URL types
if [[ -n "$TURSO_AUTH_TOKEN" ]]; then
  if [[ "$TURSO_DATABASE_URL" == libsql://* ]]; then
    # For Turso's libsql protocol, we need special handling
    # Extract just the hostname part from the URL (remove libsql://)
    HOSTNAME=${TURSO_DATABASE_URL#libsql://}
    
    # Construct a properly formatted URL for goose turso driver
    DB_CONN_STR="https://$HOSTNAME?authToken=$TURSO_AUTH_TOKEN"
  elif [[ "$TURSO_DATABASE_URL" == file:* ]]; then
    # For file-based URLs
    if [[ "$DB_CONN_STR" == *"?"* ]]; then
      DB_CONN_STR="$DB_CONN_STR&authToken=$TURSO_AUTH_TOKEN"
    else
      DB_CONN_STR="$DB_CONN_STR?authToken=$TURSO_AUTH_TOKEN"
    fi
  else
    # For other URL types
    if [[ "$DB_CONN_STR" == *"?"* ]]; then
      DB_CONN_STR="$DB_CONN_STR&authToken=$TURSO_AUTH_TOKEN"
    else
      DB_CONN_STR="$DB_CONN_STR?authToken=$TURSO_AUTH_TOKEN"
    fi
  fi
fi

mkdir -p ./migrations

# For security, mask the token in logs
MASKED_CONN_STR=$(echo "$DB_CONN_STR" | sed 's/authToken=.\{10\}.*$/authToken=***MASKED***/g')
echo "Attempting to run migrations with connection string: $MASKED_CONN_STR"

# Run goose migrations
goose -dir ./migrations turso "$DB_CONN_STR" up
echo "Migrations completed."

# Start the app
echo "Starting webhook-delivery-service..."
./webhook-delivery-service
package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

var DB *sql.DB

func Init() error {
    // Load .env file if present
    _ = godotenv.Load()

    dbURL := os.Getenv("TURSO_DATABASE_URL")
    authToken := os.Getenv("TURSO_AUTH_TOKEN")
    if dbURL == "" || authToken == "" {
        return fmt.Errorf("TURSO_DATABASE_URL or TURSO_AUTH_TOKEN not set")
    }

    // Compose connection string
    connStr := fmt.Sprintf("%s?authToken=%s", dbURL, authToken)

    db, err := sql.Open("libsql", connStr)
    if err != nil {
        return fmt.Errorf("failed to open db: %w", err)
    }

    // Optionally, ping to verify connection
    if err := db.Ping(); err != nil {
        return fmt.Errorf("failed to ping db: %w", err)
    }

    DB = db
    log.Println("Connected to Turso DB")
    return nil
}
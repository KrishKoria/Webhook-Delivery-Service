package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

var DB *sql.DB

func Init() error {
	_ = godotenv.Load()

	dbURL := os.Getenv("TURSO_DATABASE_URL")
	authToken := os.Getenv("TURSO_AUTH_TOKEN")

	if dbURL == "" {
		return fmt.Errorf("TURSO_DATABASE_URL not set")
	}

	var connStr string
	isLocalFileDB := strings.HasPrefix(dbURL, "file:")

	if isLocalFileDB {
		connStr = dbURL
		if authToken != "" {
			log.Printf("Warning: TURSO_AUTH_TOKEN is set for a file-based TURSO_DATABASE_URL ('%s'). The token will be ignored for the application's DB connection.", dbURL)
		}
		log.Printf("Application DB: Using local file database: %s", connStr)
	} else {
		// For remote Turso DBs (e.g., "libsql://..." or "https://...")
		if authToken == "" {
			return fmt.Errorf("TURSO_AUTH_TOKEN not set for remote Turso database URL: %s", dbURL)
		}
		// Construct the connection string with authToken for remote DBs
		if strings.Contains(dbURL, "?") {
			connStr = fmt.Sprintf("%s&authToken=%s", dbURL, authToken)
		} else {
			connStr = fmt.Sprintf("%s?authToken=%s", dbURL, authToken)
		}
		log.Printf("Application DB: Using remote Turso database with token.")
	}
	
	// Use "libsql" driver name for libsql-client-go
	db, err := sql.Open("libsql", connStr)
	if err != nil {
		return fmt.Errorf("failed to open db with connection string '%s': %w", maskConnStr(connStr), err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping db: %w", err)
	}

	DB = db
	log.Println("Application successfully connected to the database.")
	return nil
}

// maskConnStr masks the auth token in connection strings for logging
func maskConnStr(connStr string) string {
	if idx := strings.Index(connStr, "authToken="); idx != -1 {
		return connStr[:idx+10] + "***MASKED***"
	}
	return connStr
}
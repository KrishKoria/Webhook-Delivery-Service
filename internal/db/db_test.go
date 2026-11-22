package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLocalSQLitePragmas(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "pragmas.db"))
	conn, _, err := Open(context.Background(), Config{URL: "file:" + path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	pragmas := map[string]string{
		"journal_mode": "wal",
		"synchronous":  "1", // NORMAL
		"foreign_keys": "1",
		"busy_timeout": "5000",
	}
	for name, want := range pragmas {
		var got string
		if err := conn.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", name, err)
		}
		if got != want {
			t.Errorf("PRAGMA %s = %q, want %q", name, got, want)
		}
	}
}

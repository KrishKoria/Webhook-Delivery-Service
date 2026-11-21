package api

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// dbTimeLayout is the canonical SQLite timestamp text ('YYYY-MM-DD HH:MM:SS', UTC).
// Cursors and range filters use it so SQL compares like with like.
const dbTimeLayout = "2006-01-02 15:04:05"

func dbTime(t time.Time) string { return t.UTC().Format(dbTimeLayout) }

// cursor marks the last row of a page: rows strictly after (ts, id) in
// (ts DESC, id DESC) order come next. The zero value means "first page".
type cursor struct {
	ts string
	id string
}

// encodeCursor returns an opaque cursor: base64url("<timestamp>|<id>").
func encodeCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(dbTime(t) + "|" + id))
}

func decodeCursor(s string) (cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, err
	}
	tsPart, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return cursor{}, errors.New("malformed cursor")
	}
	if _, err := time.Parse(dbTimeLayout, tsPart); err != nil {
		return cursor{}, err
	}
	return cursor{ts: tsPart, id: id}, nil
}

// params returns the cursor as nullable SQL arguments.
func (c cursor) params() (ts, id sql.NullString) {
	if c.ts == "" {
		return sql.NullString{}, sql.NullString{}
	}
	return sql.NullString{String: c.ts, Valid: true}, sql.NullString{String: c.id, Valid: true}
}

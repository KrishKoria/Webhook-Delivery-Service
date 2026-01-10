package types

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"
)

// LibSQLTime is a custom time type that handles libsql-client-go's string timestamps.
// libsql-client-go returns timestamps as strings, not time.Time, so we need custom scanning.
type LibSQLTime struct {
	Time  time.Time
	Valid bool
}

// Common timestamp formats used by SQLite/libSQL
var timeFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// NewLibSQLTime creates a new LibSQLTime from a time.Time
func NewLibSQLTime(t time.Time) LibSQLTime {
	return LibSQLTime{Time: t, Valid: true}
}

// Scan implements sql.Scanner interface
func (lt *LibSQLTime) Scan(value interface{}) error {
	if value == nil {
		lt.Time, lt.Valid = time.Time{}, false
		return nil
	}

	switch v := value.(type) {
	case time.Time:
		lt.Time, lt.Valid = v, true
		return nil
	case string:
		if v == "" {
			lt.Time, lt.Valid = time.Time{}, false
			return nil
		}
		// Try parsing with various formats
		for _, format := range timeFormats {
			if t, err := time.Parse(format, v); err == nil {
				lt.Time, lt.Valid = t, true
				return nil
			}
		}
		return fmt.Errorf("LibSQLTime: cannot parse %q as time", v)
	case []byte:
		return lt.Scan(string(v))
	default:
		return fmt.Errorf("LibSQLTime: unsupported type %T", value)
	}
}

// Value implements driver.Valuer interface
func (lt LibSQLTime) Value() (driver.Value, error) {
	if !lt.Valid {
		return nil, nil
	}
	return lt.Time.Format(time.RFC3339Nano), nil
}

// Format returns the time formatted according to the layout
func (lt LibSQLTime) Format(layout string) string {
	if !lt.Valid {
		return ""
	}
	return lt.Time.Format(layout)
}

// ToTime returns the underlying time.Time value
func (lt LibSQLTime) ToTime() time.Time {
	return lt.Time
}

// IsZero reports whether the time is zero
func (lt LibSQLTime) IsZero() bool {
	return !lt.Valid || lt.Time.IsZero()
}

// LibSQLNullTime is similar to sql.NullTime but handles string timestamps from libsql-client-go
type LibSQLNullTime struct {
	Time  time.Time
	Valid bool
}

// NewLibSQLNullTime creates a new LibSQLNullTime from a time.Time
func NewLibSQLNullTime(t time.Time) LibSQLNullTime {
	return LibSQLNullTime{Time: t, Valid: true}
}

// NewLibSQLNullTimeFromNullTime creates a LibSQLNullTime from sql.NullTime
func NewLibSQLNullTimeFromNullTime(nt sql.NullTime) LibSQLNullTime {
	return LibSQLNullTime{Time: nt.Time, Valid: nt.Valid}
}

// NullLibSQLNullTime returns an invalid/null LibSQLNullTime
func NullLibSQLNullTime() LibSQLNullTime {
	return LibSQLNullTime{Valid: false}
}

// Scan implements sql.Scanner interface
func (nt *LibSQLNullTime) Scan(value interface{}) error {
	if value == nil {
		nt.Time, nt.Valid = time.Time{}, false
		return nil
	}

	switch v := value.(type) {
	case time.Time:
		nt.Time, nt.Valid = v, true
		return nil
	case string:
		if v == "" {
			nt.Time, nt.Valid = time.Time{}, false
			return nil
		}
		// Try parsing with various formats
		for _, format := range timeFormats {
			if t, err := time.Parse(format, v); err == nil {
				nt.Time, nt.Valid = t, true
				return nil
			}
		}
		return fmt.Errorf("LibSQLNullTime: cannot parse %q as time", v)
	case []byte:
		return nt.Scan(string(v))
	default:
		return fmt.Errorf("LibSQLNullTime: unsupported type %T", value)
	}
}

// Value implements driver.Valuer interface
func (nt LibSQLNullTime) Value() (driver.Value, error) {
	if !nt.Valid {
		return nil, nil
	}
	return nt.Time.Format(time.RFC3339Nano), nil
}

// Format returns the time formatted according to the layout, or empty string if invalid
func (nt LibSQLNullTime) Format(layout string) string {
	if !nt.Valid {
		return ""
	}
	return nt.Time.Format(layout)
}

// ToTime returns the underlying time.Time value
func (nt LibSQLNullTime) ToTime() time.Time {
	return nt.Time
}

// ToNullTime converts to sql.NullTime
func (nt LibSQLNullTime) ToNullTime() sql.NullTime {
	return sql.NullTime{Time: nt.Time, Valid: nt.Valid}
}
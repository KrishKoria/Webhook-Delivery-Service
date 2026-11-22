// Package schema embeds the goose SQL migrations so the binary can migrate itself.
package schema

import "embed"

// FS holds every *.sql migration in this directory.
//
//go:embed *.sql
var FS embed.FS

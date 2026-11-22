// Package web embeds the admin UI templates and static assets into the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates static
var files embed.FS

// Templates is the templates directory as a filesystem rooted at its contents.
var Templates = mustSub("templates")

// Static is the static asset directory as a filesystem rooted at its contents.
var Static = mustSub("static")

func mustSub(dir string) fs.FS {
	sub, err := fs.Sub(files, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

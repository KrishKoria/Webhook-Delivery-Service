package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strings"

	"github.com/gin-gonic/gin"
)

// StaticAssets fingerprints the embedded static files so pages can link
// /static/<name>?v=<hash>. The URL changes whenever a file's bytes do, so
// browsers can cache each version for a year and still pick up a deploy.
type StaticAssets struct {
	hashes map[string]string
}

// NewStaticAssets hashes every file in fsys (paths relative to its root).
func NewStaticAssets(fsys fs.FS) (*StaticAssets, error) {
	hashes := make(map[string]string)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		hashes[p] = hex.EncodeToString(sum[:])[:16]
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("hash static assets: %w", err)
	}
	return &StaticAssets{hashes: hashes}, nil
}

// URL returns the versioned URL for a static file. An unknown name is an
// error, so a typo in a template fails the render instead of shipping a 404.
func (a *StaticAssets) URL(name string) (string, error) {
	hash, ok := a.hashes[name]
	if !ok {
		return "", fmt.Errorf("unknown static asset %q", name)
	}
	return "/static/" + name + "?v=" + hash, nil
}

// CacheHeaders sets caching headers for the /static/*filepath route.
// A request whose ?v= matches the file's current hash is cacheable for a year;
// anything else (no version, or a stale one from an older page) must
// revalidate. The ETag lets http.FileServer answer revalidations with 304.
func (a *StaticAssets) CacheHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		hash, ok := a.hashes[strings.TrimPrefix(c.Param("filepath"), "/")]
		if ok {
			c.Header("ETag", `"`+hash+`"`)
		}
		if ok && c.Query("v") == hash {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			c.Header("Cache-Control", "no-cache")
		}
		c.Next()
	}
}

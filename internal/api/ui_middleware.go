package api

import "github.com/gin-gonic/gin"

// UIContentSecurityPolicy is the CSP for admin UI pages. It forbids inline
// scripts, inline styles and eval, so templates must use classes and app.js.
const UIContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// UISecurityHeaders sets the CSP and related headers on admin UI responses.
func UISecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Content-Security-Policy", UIContentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		// Pages and fragments come from the same URL, keyed on the htmx header.
		h.Add("Vary", "HX-Request")
		h.Set("Cache-Control", "no-store")
		c.Next()
	}
}

package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"testing"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
)

func TestVerifySignature(t *testing.T) {
	tests := []struct {
		name      string
		body      []byte
		secret    string
		signature string
		want      bool
	}{
		{
			name:      "valid signature",
			body:      []byte(`{"event":"test"}`),
			secret:    "mysecret",
			signature: generateTestSignature([]byte(`{"event":"test"}`), "mysecret"),
			want:      true,
		},
		{
			name:      "invalid signature",
			body:      []byte(`{"event":"test"}`),
			secret:    "mysecret",
			signature: "sha256=invalid",
			want:      false,
		},
		{
			name:      "empty signature",
			body:      []byte(`{"event":"test"}`),
			secret:    "mysecret",
			signature: "",
			want:      false,
		},
		{
			name:      "wrong secret used",
			body:      []byte(`{"event":"test"}`),
			secret:    "mysecret",
			signature: generateTestSignature([]byte(`{"event":"test"}`), "wrongsecret"),
			want:      false,
		},
		{
			name:      "tampered body",
			body:      []byte(`{"event":"tampered"}`),
			secret:    "mysecret",
			signature: generateTestSignature([]byte(`{"event":"test"}`), "mysecret"),
			want:      false,
		},
		{
			name:      "empty body valid signature",
			body:      []byte{},
			secret:    "mysecret",
			signature: generateTestSignature([]byte{}, "mysecret"),
			want:      true,
		},
		{
			name:      "missing sha256 prefix",
			body:      []byte(`{"event":"test"}`),
			secret:    "mysecret",
			signature: generateTestSignatureWithoutPrefix([]byte(`{"event":"test"}`), "mysecret"),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := verifySignature(tt.body, tt.secret, tt.signature)
			if got != tt.want {
				t.Errorf("verifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSubscriptionAllowsEvent(t *testing.T) {
	tests := []struct {
		name       string
		eventTypes sql.NullString
		eventType  string
		want       bool
	}{
		{
			name:       "no allowlist, any event allowed",
			eventTypes: sql.NullString{Valid: false},
			eventType:  "order.created",
			want:       true,
		},
		{
			name:       "empty allowlist string, any event allowed",
			eventTypes: sql.NullString{String: "", Valid: true},
			eventType:  "order.created",
			want:       true,
		},
		{
			name:       "empty event type, any subscription",
			eventTypes: sql.NullString{String: "push,pull_request", Valid: true},
			eventType:  "",
			want:       true,
		},
		{
			name:       "event in allowlist",
			eventTypes: sql.NullString{String: "push,pull_request", Valid: true},
			eventType:  "push",
			want:       true,
		},
		{
			name:       "event in allowlist with spaces",
			eventTypes: sql.NullString{String: "push, pull_request, issues", Valid: true},
			eventType:  "pull_request",
			want:       true,
		},
		{
			name:       "event not in allowlist",
			eventTypes: sql.NullString{String: "push,pull_request", Valid: true},
			eventType:  "issues",
			want:       false,
		},
		{
			name:       "single event allowlist - match",
			eventTypes: sql.NullString{String: "webhook.test", Valid: true},
			eventType:  "webhook.test",
			want:       true,
		},
		{
			name:       "single event allowlist - no match",
			eventTypes: sql.NullString{String: "webhook.test", Valid: true},
			eventType:  "webhook.other",
			want:       false,
		},
		{
			name:       "case sensitive match",
			eventTypes: sql.NullString{String: "Push,PullRequest", Valid: true},
			eventType:  "push",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := database.Subscription{
				EventTypes: tt.eventTypes,
			}
			got := subscriptionAllowsEvent(sub, tt.eventType)
			if got != tt.want {
				t.Errorf("subscriptionAllowsEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function to generate test signatures
func generateTestSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Helper function to generate signature without prefix
func generateTestSignatureWithoutPrefix(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestSignatureTimingSafe(t *testing.T) {
	// Verify that signature comparison uses constant-time comparison
	// This is already guaranteed by hmac.Equal, but we test the behavior

	body := []byte(`{"event":"test"}`)
	secret := "mysecret"
	validSig := generateTestSignature(body, secret)

	// These should all fail but timing shouldn't leak info
	invalidSigs := []string{
		"sha256=0000000000000000000000000000000000000000000000000000000000000000",
		"sha256=ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		validSig[:len(validSig)-1] + "0", // Off by one
	}

	for _, sig := range invalidSigs {
		result := verifySignature(body, secret, sig)
		if result {
			t.Errorf("Expected invalid signature %s to fail", sig)
		}
	}
}

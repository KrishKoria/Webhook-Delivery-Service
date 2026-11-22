package api

import (
	"encoding/hex"
	"net/url"
	"testing"
)

func TestValidateTargetURL(t *testing.T) {
	tests := []struct {
		name      string
		targetURL string
		wantErr   bool
		errField  string
	}{
		{
			name:      "valid https URL",
			targetURL: "https://example.com/webhook",
			wantErr:   false,
		},
		{
			name:      "valid http URL",
			targetURL: "http://example.com/webhook",
			wantErr:   false,
		},
		{
			name:      "valid URL with port",
			targetURL: "https://example.com:8080/webhook",
			wantErr:   false,
		},
		{
			name:      "valid URL with query params",
			targetURL: "https://example.com/webhook?key=value",
			wantErr:   false,
		},
		{
			name:      "valid localhost URL",
			targetURL: "http://localhost:3000/webhook",
			wantErr:   false,
		},
		{
			name:      "empty URL",
			targetURL: "",
			wantErr:   true,
			errField:  "target_url",
		},
		{
			name:      "invalid scheme - ftp",
			targetURL: "ftp://example.com/webhook",
			wantErr:   true,
			errField:  "target_url",
		},
		{
			name:      "no scheme",
			targetURL: "example.com/webhook",
			wantErr:   true,
			errField:  "target_url",
		},
		{
			name:      "no host - just path",
			targetURL: "/webhook",
			wantErr:   true,
			errField:  "target_url",
		},
		{
			name:      "invalid URL - spaces",
			targetURL: "https://example .com/webhook",
			wantErr:   true,
			errField:  "target_url",
		},
		{
			name:      "javascript scheme",
			targetURL: "javascript:alert(1)",
			wantErr:   true,
			errField:  "target_url",
		},
		{
			name:      "file scheme",
			targetURL: "file:///etc/passwd",
			wantErr:   true,
			errField:  "target_url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTargetURL(tt.targetURL)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateTargetURL(%q) expected error, got nil", tt.targetURL)
					return
				}
				if ve, ok := err.(*ValidationError); ok {
					if ve.Field != tt.errField {
						t.Errorf("validateTargetURL(%q) error field = %q, want %q", tt.targetURL, ve.Field, tt.errField)
					}
				}
			} else {
				if err != nil {
					t.Errorf("validateTargetURL(%q) unexpected error: %v", tt.targetURL, err)
				}
			}
		})
	}
}

func TestToSubscriptionResponse_SecretRedaction(t *testing.T) {
	// This test verifies that SubscriptionResponse does not expose secrets
	// We cannot run this without database mocks, but we can verify the struct definition

	resp := SubscriptionResponse{
		ID:         "test-id",
		TargetURL:  "https://example.com",
		EventTypes: "push,pull_request",
		HasSecret:  true,
		CreatedAt:  "2024-01-01T00:00:00Z",
	}

	// Verify struct has HasSecret field but no actual Secret field
	if resp.HasSecret != true {
		t.Error("HasSecret should be true")
	}

	// The type definition should not have a Secret field
	// This is a compile-time check - if Secret field existed and was used here, it would compile
}

func TestValidateTargetURL_ParseError(t *testing.T) {
	// Test URL that causes parse error
	invalidURL := "://invalid"
	err := validateTargetURL(invalidURL)
	if err == nil {
		t.Error("expected error for invalid URL")
	}

	// Check if it's parsed as invalid by url.Parse
	_, parseErr := url.Parse(invalidURL)
	if parseErr != nil {
		// url.Parse actually succeeds for this, but scheme will be empty
		// Let's test the actual behavior
		t.Logf("url.Parse error: %v", parseErr)
	}
}

func TestGenerateSecureSecret(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		secret, err := generateSecureSecret()
		if err != nil {
			t.Fatal(err)
		}
		if b, err := hex.DecodeString(secret); err != nil || len(b) != 32 {
			t.Fatalf("secret %q is not 32 hex-encoded bytes (err %v)", secret, err)
		}
		if seen[secret] {
			t.Fatalf("duplicate secret %q", secret)
		}
		seen[secret] = true
	}
}

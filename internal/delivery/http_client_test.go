package delivery

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestDefaultHTTPClientConfig verifies default configuration values
func TestDefaultHTTPClientConfig(t *testing.T) {
	config := DefaultHTTPClientConfig()

	if config.Timeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got %v", config.Timeout)
	}
	if config.DialTimeout != 10*time.Second {
		t.Errorf("Expected default dial timeout 10s, got %v", config.DialTimeout)
	}
	if config.TLSHandshakeTimeout != 10*time.Second {
		t.Errorf("Expected default TLS timeout 10s, got %v", config.TLSHandshakeTimeout)
	}
	if config.DisableSSRFProtection {
		t.Error("SSRF protection should be enabled by default")
	}
	if config.MaxRedirects != 0 {
		t.Errorf("Expected no redirects by default, got %d", config.MaxRedirects)
	}
	if config.AllowPrivateIPs {
		t.Error("Private IPs should be blocked by default")
	}
}

// TestNewSecureHTTPClient verifies client creation
func TestNewSecureHTTPClient(t *testing.T) {
	config := DefaultHTTPClientConfig()
	client := NewSecureHTTPClient(config)

	if client == nil {
		t.Fatal("NewSecureHTTPClient returned nil")
	}
	if client.Timeout != config.Timeout {
		t.Errorf("Expected timeout %v, got %v", config.Timeout, client.Timeout)
	}
}

// TestValidateIP tests IP validation for SSRF protection
func TestValidateIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		wantErr bool
		errMsg  string
	}{
		// Loopback addresses
		{"loopback IPv4", "127.0.0.1", true, "loopback"},
		{"loopback IPv4 non-standard", "127.0.0.2", true, "loopback"},
		{"loopback IPv6", "::1", true, "loopback"},

		// Private ranges IPv4
		{"private 10.x", "10.0.0.1", true, "private"},
		{"private 10.255", "10.255.255.255", true, "private"},
		{"private 172.16", "172.16.0.1", true, "private"},
		{"private 172.31", "172.31.255.255", true, "private"},
		{"private 192.168", "192.168.1.1", true, "private"},

		// Link-local
		{"link-local IPv4", "169.254.1.1", true, "link-local"},
		{"link-local IPv6", "fe80::1", true, "link-local"},

		// Carrier-grade NAT
		{"CGNAT", "100.64.0.1", true, "shared address"},
		{"CGNAT boundary low", "100.64.0.0", true, "shared address"},
		{"CGNAT boundary high", "100.127.255.255", true, "shared address"},
		{"not CGNAT", "100.128.0.1", false, ""},

		// Test networks
		{"TEST-NET-1", "192.0.2.1", true, "TEST-NET-1"},
		{"TEST-NET-2", "198.51.100.1", true, "TEST-NET-2"},
		{"TEST-NET-3", "203.0.113.1", true, "TEST-NET-3"},

		// IETF Protocol Assignments
		{"IETF protocol", "192.0.0.1", true, "IETF"},

		// Reserved ranges
		{"reserved 240+", "240.0.0.1", true, "reserved"},
		{"reserved 255", "255.0.0.1", true, "reserved"},

		// Unspecified
		{"unspecified IPv4", "0.0.0.0", true, "unspecified"},
		{"unspecified IPv6", "::", true, "unspecified"},
		{"this host prefix", "0.1.2.3", true, "this host"},

		// Broadcast
		{"broadcast", "255.255.255.255", true, "reserved"},

		// Multicast
		{"multicast IPv4", "224.0.0.1", true, "multicast"},
		{"multicast IPv6", "ff02::1", true, "multicast"},

		// IPv6 specific
		{"unique local", "fd00::1", true, "private"},
		{"unique local fc", "fc00::1", true, "private"},
		{"documentation IPv6", "2001:db8::1", true, "documentation"},

		// Public IPs (should be allowed)
		{"public 8.8.8.8", "8.8.8.8", false, ""},
		{"public 1.1.1.1", "1.1.1.1", false, ""},
		{"public cloudflare", "104.16.0.1", false, ""},
		{"public google", "142.250.80.46", false, ""},
		{"public IPv6", "2607:f8b0:4004:800::200e", false, ""},

		// Edge cases for private range boundaries
		{"not private 172.15", "172.15.255.255", false, ""},
		{"not private 172.32", "172.32.0.1", false, ""},
		{"not private 192.167", "192.167.1.1", false, ""},
		{"not private 192.169", "192.169.1.1", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("Failed to parse IP: %s", tt.ip)
			}

			err := validateIP(ip)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error for IP %s, got nil", tt.ip)
				} else if tt.errMsg != "" {
					// Check that error contains expected message
					if !containsIgnoreCase(err.Error(), tt.errMsg) {
						t.Errorf("Error %q should contain %q", err.Error(), tt.errMsg)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for IP %s: %v", tt.ip, err)
				}
			}
		})
	}
}

// TestValidateAddress tests address validation including hostname resolution
func TestValidateAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		// IP addresses with ports
		{"loopback with port", "127.0.0.1:8080", true},
		{"private with port", "192.168.1.1:80", true},
		{"public with port", "8.8.8.8:53", false},

		// IP addresses without ports
		{"loopback no port", "127.0.0.1", true},
		{"public no port", "8.8.8.8", false},

		// Hostnames that resolve to public IPs
		// Note: These tests depend on DNS resolution
		{"google.com", "google.com:80", false},

		// Hostnames that resolve to localhost
		{"localhost", "localhost:80", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAddress(tt.addr)
			if tt.wantErr && err == nil {
				t.Errorf("Expected error for address %s", tt.addr)
			}
			if !tt.wantErr && err != nil {
				// Allow DNS resolution failures for external hostnames
				if tt.addr != "google.com:80" {
					t.Errorf("Unexpected error for address %s: %v", tt.addr, err)
				}
			}
		})
	}
}

// TestValidateURL tests URL validation
func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Valid URLs
		{"https public", "https://example.com/webhook", false},
		{"http public", "http://example.com:8080/hook", false},

		// Invalid schemes
		{"file scheme", "file:///etc/passwd", true},
		{"ftp scheme", "ftp://example.com", true},
		{"gopher scheme", "gopher://example.com", true},
		{"javascript scheme", "javascript:alert(1)", true},

		// SSRF targets
		{"localhost http", "http://localhost/hook", true},
		{"127.0.0.1 http", "http://127.0.0.1/hook", true},
		{"private IP", "http://192.168.1.1/hook", true},
		{"internal DNS", "http://internal.local/hook", true}, // Will fail DNS

		// Malformed URLs
		{"empty scheme", "://example.com", true},
		{"no host", "http:///path", true},

		// IPv6
		{"IPv6 loopback", "http://[::1]/hook", true},
		{"IPv6 public", "http://[2607:f8b0:4004:800::200e]/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.url)
			if tt.wantErr && err == nil {
				t.Errorf("Expected error for URL %s", tt.url)
			}
			// Note: some tests may pass or fail based on DNS resolution
		})
	}
}

// TestSecureHTTPClientNoRedirects tests that redirects are blocked by default
func TestSecureHTTPClientNoRedirects(t *testing.T) {
	// Create a server that redirects
	redirectCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := HTTPClientConfig{
		Timeout:               5 * time.Second,
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		DisableSSRFProtection: true, // Allow localhost for test
		MaxRedirects:          0,    // No redirects
		AllowPrivateIPs:       true,
	}

	client := NewSecureHTTPClient(config)
	_, err := client.Get(server.URL + "/redirect")

	if err == nil {
		t.Error("Expected redirect to be blocked")
	}
}

// TestSecureHTTPClientLimitedRedirects tests limited redirect following
func TestSecureHTTPClientLimitedRedirects(t *testing.T) {
	redirectCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		if redirectCount <= 3 {
			http.Redirect(w, r, "/redirect"+string(rune('0'+redirectCount)), http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tests := []struct {
		name         string
		maxRedirects int
		wantErr      bool
	}{
		{"allow 1 redirect", 1, true},  // Will hit the limit
		{"allow 5 redirects", 5, false}, // Enough for 3 redirects
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redirectCount = 0 // Reset

			config := HTTPClientConfig{
				Timeout:               5 * time.Second,
				DialTimeout:           5 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				DisableSSRFProtection: true,
				MaxRedirects:          tt.maxRedirects,
				AllowPrivateIPs:       true,
			}

			client := NewSecureHTTPClient(config)
			_, err := client.Get(server.URL + "/redirect")

			if tt.wantErr && err == nil {
				t.Error("Expected redirect limit error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestSecureHTTPClientTimeout tests request timeout
func TestSecureHTTPClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Delay response
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := HTTPClientConfig{
		Timeout:               500 * time.Millisecond, // Short timeout
		DialTimeout:           500 * time.Millisecond,
		TLSHandshakeTimeout:   500 * time.Millisecond,
		DisableSSRFProtection: true,
		AllowPrivateIPs:       true,
	}

	client := NewSecureHTTPClient(config)
	_, err := client.Get(server.URL)

	if err == nil {
		t.Error("Expected timeout error")
	}
}

// TestValidateTargetURL tests the public validation function
func TestValidateTargetURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Valid URLs - using real public domains
		{"valid https google", "https://www.google.com/webhook", false},
		{"valid http cloudflare", "http://1.1.1.1/webhook", false},

		// SSRF targets - blocked
		{"localhost blocked", "http://localhost:3000/hook", true},
		{"private IP blocked", "http://10.0.0.1/internal", true},
		{"loopback blocked", "http://127.0.0.1/hook", true},
		
		// Invalid schemes
		{"invalid scheme", "ftp://example.com/file", true},
		{"file scheme", "file:///etc/passwd", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTargetURL(tt.url)
			if tt.wantErr && err == nil {
				t.Errorf("Expected error for URL %s", tt.url)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error for URL %s: %v", tt.url, err)
			}
		})
	}
}

// TestIPv4MappedIPv6 tests validation of IPv4-mapped IPv6 addresses
func TestIPv4MappedIPv6(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		wantErr bool
	}{
		// IPv4-mapped IPv6 addresses that map to blocked IPs
		{"mapped loopback", "::ffff:127.0.0.1", true},
		{"mapped private", "::ffff:192.168.1.1", true},
		{"mapped public", "::ffff:8.8.8.8", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("Failed to parse IP: %s", tt.ip)
			}

			err := validateIP(ip)
			if tt.wantErr && err == nil {
				t.Errorf("Expected error for IP %s", tt.ip)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error for IP %s: %v", tt.ip, err)
			}
		})
	}
}

// Helper function for case-insensitive string contains
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > 0 && len(substr) > 0 &&
				containsIgnoreCaseImpl(s, substr))
}

func containsIgnoreCaseImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if matchIgnoreCase(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func matchIgnoreCase(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

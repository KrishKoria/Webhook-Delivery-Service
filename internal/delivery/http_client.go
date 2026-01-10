package delivery

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// HTTPClientConfig holds configuration for the secure HTTP client
type HTTPClientConfig struct {
	// Timeout is the total timeout for the request including dialing, TLS, etc.
	Timeout time.Duration
	// DialTimeout is the timeout for establishing the TCP connection
	DialTimeout time.Duration
	// TLSHandshakeTimeout is the timeout for TLS handshake
	TLSHandshakeTimeout time.Duration
	// DisableSSRFProtection disables SSRF protection (for testing only)
	DisableSSRFProtection bool
	// MaxRedirects is the maximum number of redirects to follow (0 = no redirects)
	MaxRedirects int
	// AllowPrivateIPs allows requests to private IP ranges (for testing only)
	AllowPrivateIPs bool
}

// DefaultHTTPClientConfig returns the default secure configuration
func DefaultHTTPClientConfig() HTTPClientConfig {
	return HTTPClientConfig{
		Timeout:               30 * time.Second,
		DialTimeout:           10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		DisableSSRFProtection: false,
		MaxRedirects:          0, // No redirects by default for webhooks
		AllowPrivateIPs:       false,
	}
}

// LoadHTTPClientConfigFromEnv loads configuration from environment variables
func LoadHTTPClientConfigFromEnv() HTTPClientConfig {
	config := DefaultHTTPClientConfig()

	if v := os.Getenv("WEBHOOK_TIMEOUT_SECONDS"); v != "" {
		if timeout, err := strconv.Atoi(v); err == nil && timeout > 0 {
			config.Timeout = time.Duration(timeout) * time.Second
		}
	}

	if v := os.Getenv("WEBHOOK_DIAL_TIMEOUT_SECONDS"); v != "" {
		if timeout, err := strconv.Atoi(v); err == nil && timeout > 0 {
			config.DialTimeout = time.Duration(timeout) * time.Second
		}
	}

	if v := os.Getenv("WEBHOOK_TLS_TIMEOUT_SECONDS"); v != "" {
		if timeout, err := strconv.Atoi(v); err == nil && timeout > 0 {
			config.TLSHandshakeTimeout = time.Duration(timeout) * time.Second
		}
	}

	if v := os.Getenv("WEBHOOK_MAX_REDIRECTS"); v != "" {
		if redirects, err := strconv.Atoi(v); err == nil && redirects >= 0 {
			config.MaxRedirects = redirects
		}
	}

	if v := os.Getenv("WEBHOOK_ALLOW_PRIVATE_IPS"); strings.ToLower(v) == "true" {
		config.AllowPrivateIPs = true
	}

	return config
}

// NewSecureHTTPClient creates an HTTP client with SSRF protection
func NewSecureHTTPClient(config HTTPClientConfig) *http.Client {
	dialer := &net.Dialer{
		Timeout: config.DialTimeout,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if !config.DisableSSRFProtection && !config.AllowPrivateIPs {
				if err := validateAddress(addr); err != nil {
					return nil, err
				}
			}
			return dialer.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: config.Timeout,
		DisableKeepAlives:     true, // Don't reuse connections for webhooks
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
	}

	// Configure redirect policy
	if config.MaxRedirects == 0 {
		// No redirects allowed
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("redirects not allowed for webhook delivery")
		}
	} else {
		// Limited redirects with SSRF check on each hop
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= config.MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", config.MaxRedirects)
			}
			// Validate redirect target if SSRF protection is enabled
			if !config.DisableSSRFProtection && !config.AllowPrivateIPs {
				if err := validateURL(req.URL.String()); err != nil {
					return fmt.Errorf("redirect blocked: %w", err)
				}
			}
			return nil
		}
	}

	return client
}

// validateAddress checks if the address is safe (not a private/internal IP)
func validateAddress(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Try without port
		host = addr
	}

	// Check if it's an IP address or hostname
	ip := net.ParseIP(host)
	if ip == nil {
		// It's a hostname, resolve it
		ips, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("failed to resolve hostname %s: %w", host, err)
		}
		// Check all resolved IPs
		for _, resolvedIP := range ips {
			if err := validateIP(resolvedIP); err != nil {
				return err
			}
		}
		return nil
	}

	return validateIP(ip)
}

// validateIP checks if the IP is safe (not private, loopback, etc.)
func validateIP(ip net.IP) error {
	// Check loopback
	if ip.IsLoopback() {
		return fmt.Errorf("loopback addresses not allowed: %s", ip)
	}

	// Check private ranges
	if ip.IsPrivate() {
		return fmt.Errorf("private IP addresses not allowed: %s", ip)
	}

	// Check link-local unicast
	if ip.IsLinkLocalUnicast() {
		return fmt.Errorf("link-local addresses not allowed: %s", ip)
	}

	// Check link-local multicast
	if ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local multicast addresses not allowed: %s", ip)
	}

	// Check interface-local multicast
	if ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("interface-local multicast addresses not allowed: %s", ip)
	}

	// Check multicast
	if ip.IsMulticast() {
		return fmt.Errorf("multicast addresses not allowed: %s", ip)
	}

	// Check unspecified (0.0.0.0 or ::)
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified addresses not allowed: %s", ip)
	}

	// Check IPv4 specific ranges
	if ip4 := ip.To4(); ip4 != nil {
		// Check for 169.254.0.0/16 (link-local) - already covered by IsLinkLocalUnicast
		// Check for 100.64.0.0/10 (Carrier-grade NAT / Shared Address Space)
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return fmt.Errorf("shared address space (100.64.0.0/10) not allowed: %s", ip)
		}

		// Check for 192.0.0.0/24 (IETF Protocol Assignments)
		if ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 0 {
			return fmt.Errorf("IETF protocol assignments address not allowed: %s", ip)
		}

		// Check for 192.0.2.0/24 (TEST-NET-1)
		if ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2 {
			return fmt.Errorf("TEST-NET-1 address not allowed: %s", ip)
		}

		// Check for 198.51.100.0/24 (TEST-NET-2)
		if ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100 {
			return fmt.Errorf("TEST-NET-2 address not allowed: %s", ip)
		}

		// Check for 203.0.113.0/24 (TEST-NET-3)
		if ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113 {
			return fmt.Errorf("TEST-NET-3 address not allowed: %s", ip)
		}

		// Check for 0.0.0.0/8 - already covered by IsUnspecified for 0.0.0.0
		if ip4[0] == 0 {
			return fmt.Errorf("this host address (0.0.0.0/8) not allowed: %s", ip)
		}

		// Check for 240.0.0.0/4 (reserved for future use)
		if ip4[0] >= 240 {
			return fmt.Errorf("reserved address (240.0.0.0/4) not allowed: %s", ip)
		}

		// Check for broadcast (255.255.255.255)
		if ip4[0] == 255 && ip4[1] == 255 && ip4[2] == 255 && ip4[3] == 255 {
			return fmt.Errorf("broadcast address not allowed: %s", ip)
		}
	}

	// Check IPv6 specific ranges
	if ip6 := ip.To16(); ip6 != nil && ip.To4() == nil {
		// Check for ::1/128 - already covered by IsLoopback
		// Check for ::/128 - already covered by IsUnspecified

		// Check for ::ffff:0:0/96 (IPv4-mapped IPv6)
		if ip6[0] == 0 && ip6[1] == 0 && ip6[2] == 0 && ip6[3] == 0 &&
			ip6[4] == 0 && ip6[5] == 0 && ip6[6] == 0 && ip6[7] == 0 &&
			ip6[8] == 0 && ip6[9] == 0 && ip6[10] == 0xff && ip6[11] == 0xff {
			// Extract IPv4 and validate it
			ip4 := net.IPv4(ip6[12], ip6[13], ip6[14], ip6[15])
			return validateIP(ip4)
		}

		// Check for 64:ff9b::/96 (IPv4/IPv6 translation)
		if ip6[0] == 0x00 && ip6[1] == 0x64 && ip6[2] == 0xff && ip6[3] == 0x9b {
			return fmt.Errorf("IPv4/IPv6 translation address not allowed: %s", ip)
		}

		// Check for fc00::/7 (Unique local address) - similar to private IPv4
		if ip6[0]&0xfe == 0xfc {
			return fmt.Errorf("unique local address not allowed: %s", ip)
		}

		// Check for 2001:db8::/32 (documentation)
		if ip6[0] == 0x20 && ip6[1] == 0x01 && ip6[2] == 0x0d && ip6[3] == 0xb8 {
			return fmt.Errorf("documentation address not allowed: %s", ip)
		}
	}

	return nil
}

// validateURL validates a URL for SSRF protection
func validateURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow http and https schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("invalid scheme %s, only http and https allowed", parsedURL.Scheme)
	}

	// Don't allow URLs without hostname
	if parsedURL.Hostname() == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	// Validate the hostname
	return validateAddress(parsedURL.Host)
}

// ValidateTargetURL is a public function to validate webhook target URLs
// This can be used during subscription creation to validate the target URL
func ValidateTargetURL(targetURL string) error {
	return validateURL(targetURL)
}

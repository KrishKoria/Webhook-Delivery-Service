package delivery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// HTTPClientConfig holds configuration for the outbound webhook HTTP client.
//
// Environment variables (see LoadHTTPClientConfigFromEnv):
//   - WEBHOOK_TIMEOUT_SECONDS      total request timeout (default 30)
//   - WEBHOOK_DIAL_TIMEOUT_SECONDS TCP connect timeout (default 10)
//   - WEBHOOK_TLS_TIMEOUT_SECONDS  TLS handshake timeout (default 10)
//   - WEBHOOK_MAX_REDIRECTS        redirects to follow (default 0)
//   - WEBHOOK_ALLOW_PRIVATE_IPS    "true" disables the SSRF IP filter (local dev only)
type HTTPClientConfig struct {
	// Timeout is the total timeout for the request including dialing, TLS, etc.
	Timeout time.Duration
	// DialTimeout is the timeout for establishing the TCP connection.
	DialTimeout time.Duration
	// TLSHandshakeTimeout is the timeout for the TLS handshake.
	TLSHandshakeTimeout time.Duration
	// DisableSSRFProtection disables SSRF protection (for testing only).
	DisableSSRFProtection bool
	// MaxRedirects is the maximum number of redirects to follow (0 = no redirects).
	MaxRedirects int
	// AllowPrivateIPs allows requests to private IP ranges (for testing only).
	AllowPrivateIPs bool
	// Resolver resolves host names before dialing. Nil means net.DefaultResolver.
	Resolver Resolver
}

// Resolver is the subset of *net.Resolver used for SSRF checks; tests inject fakes.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// ErrBlockedAddress is wrapped by every error caused by the SSRF IP filter.
var ErrBlockedAddress = errors.New("blocked address")

// DefaultHTTPClientConfig returns the default secure configuration.
func DefaultHTTPClientConfig() HTTPClientConfig {
	return HTTPClientConfig{
		Timeout:             30 * time.Second,
		DialTimeout:         10 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxRedirects:        0, // webhooks are not redirected by default
	}
}

// LoadHTTPClientConfigFromEnv loads configuration from environment variables.
func LoadHTTPClientConfigFromEnv() HTTPClientConfig {
	config := DefaultHTTPClientConfig()
	if v, ok := envPositiveInt("WEBHOOK_TIMEOUT_SECONDS"); ok {
		config.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := envPositiveInt("WEBHOOK_DIAL_TIMEOUT_SECONDS"); ok {
		config.DialTimeout = time.Duration(v) * time.Second
	}
	if v, ok := envPositiveInt("WEBHOOK_TLS_TIMEOUT_SECONDS"); ok {
		config.TLSHandshakeTimeout = time.Duration(v) * time.Second
	}
	if v := os.Getenv("WEBHOOK_MAX_REDIRECTS"); v != "" {
		if redirects, err := strconv.Atoi(v); err == nil && redirects >= 0 {
			config.MaxRedirects = redirects
		}
	}
	if strings.EqualFold(os.Getenv("WEBHOOK_ALLOW_PRIVATE_IPS"), "true") {
		config.AllowPrivateIPs = true
	}
	return config
}

func (c HTTPClientConfig) ssrfEnabled() bool {
	return !c.DisableSSRFProtection && !c.AllowPrivateIPs
}

func (c HTTPClientConfig) resolver() Resolver {
	if c.Resolver != nil {
		return c.Resolver
	}
	return net.DefaultResolver
}

// NewSecureHTTPClient creates an HTTP client with keep-alives and SSRF protection.
//
// With SSRF protection on, the dialer resolves the host itself, validates every
// returned IP, and connects to a validated IP literal. Because the address that
// was checked is the address that is dialed, DNS rebinding cannot swap it.
// TLS still verifies against (and sends SNI for) the URL's host name, because
// http.Transport derives ServerName from the request, not from the dialed address.
func NewSecureHTTPClient(config HTTPClientConfig) *http.Client {
	dialer := &net.Dialer{Timeout: config.DialTimeout, KeepAlive: 30 * time.Second}
	dial := dialer.DialContext
	if config.ssrfEnabled() {
		dial = (&ssrfDialer{dialer: dialer, resolver: config.resolver(), allow: checkIP}).DialContext
	}

	transport := &http.Transport{
		Proxy:                 nil, // a proxy would bypass the IP filter
		DialContext:           dial,
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: config.Timeout,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	client := &http.Client{Transport: transport, Timeout: config.Timeout}
	maxRedirects := config.MaxRedirects
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// Every hop is dialed through the same validating dialer, so only the count needs checking here.
		if maxRedirects == 0 {
			return errors.New("redirects not allowed for webhook delivery")
		}
		if len(via) > maxRedirects { // via holds the original request plus each hop so far
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	}
	return client
}

// ssrfDialer resolves, validates and dials a validated IP literal.
type ssrfDialer struct {
	dialer   *net.Dialer
	resolver Resolver
	allow    func(netip.Addr) error
}

func (d *ssrfDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid dial address %q: %w", addr, err)
	}
	ips, err := resolveAndCheck(ctx, d.resolver, host, d.allow)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := d.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// resolveAndCheck returns host's IPs, failing if any of them is not allowed.
// IP literals are checked without a lookup.
func resolveAndCheck(ctx context.Context, r Resolver, host string, allow func(netip.Addr) error) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if ip, err := netip.ParseAddr(host); err == nil {
		if err := allow(ip); err != nil {
			return nil, err
		}
		return []netip.Addr{ip.Unmap()}, nil
	}
	addrs, err := r.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	ips := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		ip, ok := netip.AddrFromSlice(a.IP)
		if !ok {
			return nil, fmt.Errorf("resolve %s: invalid address %v", host, a.IP)
		}
		if err := allow(ip); err != nil {
			return nil, fmt.Errorf("%s resolves to a disallowed address: %w", host, err)
		}
		ips = append(ips, ip.Unmap())
	}
	return ips, nil
}

// blockedRanges are ranges a webhook may never target: loopback, private,
// link-local, CGNAT, documentation/benchmark, multicast, reserved, and the IPv6
// transition ranges that can tunnel to arbitrary IPv4 hosts.
var blockedRanges = []struct {
	prefix netip.Prefix
	why    string
}{
	{netip.MustParsePrefix("0.0.0.0/8"), "this-network"},
	{netip.MustParsePrefix("10.0.0.0/8"), "private"},
	{netip.MustParsePrefix("100.64.0.0/10"), "shared address space"},
	{netip.MustParsePrefix("127.0.0.0/8"), "loopback"},
	{netip.MustParsePrefix("169.254.0.0/16"), "link-local"},
	{netip.MustParsePrefix("172.16.0.0/12"), "private"},
	{netip.MustParsePrefix("192.0.0.0/24"), "IETF protocol assignments"},
	{netip.MustParsePrefix("192.0.2.0/24"), "documentation (TEST-NET-1)"},
	{netip.MustParsePrefix("192.88.99.0/24"), "6to4 relay"},
	{netip.MustParsePrefix("192.168.0.0/16"), "private"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmarking"},
	{netip.MustParsePrefix("198.51.100.0/24"), "documentation (TEST-NET-2)"},
	{netip.MustParsePrefix("203.0.113.0/24"), "documentation (TEST-NET-3)"},
	{netip.MustParsePrefix("224.0.0.0/4"), "multicast"},
	{netip.MustParsePrefix("240.0.0.0/4"), "reserved"},
	{netip.MustParsePrefix("::/96"), "IPv4-compatible / unspecified / loopback"},
	{netip.MustParsePrefix("64:ff9b::/96"), "NAT64"},
	{netip.MustParsePrefix("64:ff9b:1::/48"), "local NAT64"},
	{netip.MustParsePrefix("100::/64"), "discard-only"},
	{netip.MustParsePrefix("2001::/32"), "Teredo"},
	{netip.MustParsePrefix("2001:db8::/32"), "documentation"},
	{netip.MustParsePrefix("2002::/16"), "6to4"},
	{netip.MustParsePrefix("fc00::/7"), "unique local"},
	{netip.MustParsePrefix("fe80::/10"), "link-local"},
	{netip.MustParsePrefix("ff00::/8"), "multicast"},
}

// checkIP reports whether ip may be dialed for a webhook.
func checkIP(ip netip.Addr) error {
	// Prefix.Contains is always false for a zoned address ("::1%lo"), so drop
	// the zone before matching or it would slip past every blocked range.
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() {
		return fmt.Errorf("%w: invalid IP", ErrBlockedAddress)
	}
	for _, r := range blockedRanges {
		if r.prefix.Contains(ip) {
			return fmt.Errorf("%w: %s is %s", ErrBlockedAddress, ip, r.why)
		}
	}
	return nil
}

// validateIP is the net.IP form of checkIP.
func validateIP(ip net.IP) error {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return fmt.Errorf("%w: invalid IP %v", ErrBlockedAddress, ip)
	}
	return checkIP(addr)
}

// ValidateTargetURL checks that targetURL is an http(s) URL whose host resolves
// only to public addresses. Use it when a subscription is created or updated.
// It honours WEBHOOK_ALLOW_PRIVATE_IPS like the delivery client does.
func ValidateTargetURL(targetURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ValidateTargetURLContext(ctx, targetURL)
}

// ValidateTargetURLContext is ValidateTargetURL with a caller-supplied context
// bounding the DNS lookup.
func ValidateTargetURLContext(ctx context.Context, targetURL string) error {
	return ValidateTargetURLWithConfig(ctx, targetURL, LoadHTTPClientConfigFromEnv())
}

// ValidateTargetURLWithConfig validates targetURL with an explicit policy and
// resolver (config.Resolver). With SSRF protection off only the syntax is checked.
func ValidateTargetURLWithConfig(ctx context.Context, targetURL string, config HTTPClientConfig) error {
	u, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid scheme %q, only http and https allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("URL must have a hostname")
	}
	if !config.ssrfEnabled() {
		return nil
	}
	_, err = resolveAndCheck(ctx, config.resolver(), host, checkIP)
	return err
}

func envPositiveInt(key string) (int, bool) {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

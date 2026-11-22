package delivery

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeResolver answers from a map and never touches real DNS.
type fakeResolver struct {
	hosts map[string][]string
	calls atomic.Int32
}

func (f *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	f.calls.Add(1)
	ips, ok := f.hosts[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	out := make([]net.IPAddr, len(ips))
	for i, s := range ips {
		out[i] = net.IPAddr{IP: net.ParseIP(s)}
	}
	return out, nil
}

func TestCheckIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.9.9.9", "::1", "0.0.0.0", "::", "0.1.2.3",
		"10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1", "fc00::1", "fd12::1",
		"169.254.169.254", "fe80::1", "100.64.0.1", "100.127.255.255",
		"192.0.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "2001:db8::1",
		"198.18.0.1", "198.19.255.255", // benchmarking
		"2002:7f00:1::", "2001:0:4136:e378::1", // 6to4, Teredo
		"64:ff9b::7f00:1", "224.0.0.1", "ff02::1", "240.0.0.1", "255.255.255.255",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::127.0.0.1",
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "198.20.0.1", "2606:4700:4700::1111", "::ffff:8.8.8.8", "2001:4860:4860::8888"}

	for _, s := range blocked {
		if err := checkIP(netip.MustParseAddr(s)); !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("%s: want blocked, got %v", s, err)
		}
		if err := validateIP(net.ParseIP(s)); err == nil {
			t.Errorf("validateIP(%s): want error", s)
		}
	}
	for _, s := range allowed {
		if err := checkIP(netip.MustParseAddr(s)); err != nil {
			t.Errorf("%s: want allowed, got %v", s, err)
		}
	}
}

func TestValidateTargetURLWithResolver(t *testing.T) {
	r := &fakeResolver{hosts: map[string][]string{
		"public.test":   {"93.184.216.34", "2606:4700:4700::1111"},
		"mixed.test":    {"93.184.216.34", "10.0.0.5"},
		"metadata.test": {"169.254.169.254"},
	}}
	cfg := DefaultHTTPClientConfig()
	cfg.Resolver = r

	cases := []struct {
		url     string
		wantErr bool
	}{
		{"https://public.test/hook", false},
		{"http://public.test:8080/hook", false},
		{"http://8.8.8.8/hook", false},
		{"http://[2606:4700:4700::1111]/hook", false},
		{"https://mixed.test/hook", true}, // any private answer rejects the host
		{"https://metadata.test/latest", true},
		{"http://169.254.169.254/latest/meta-data", true},
		{"http://127.0.0.1:3000/hook", true},
		{"http://[::1]/hook", true},
		{"http://unknown.test/hook", true}, // NXDOMAIN
		{"ftp://public.test/file", true},
		{"file:///etc/passwd", true},
		{"http:///path", true},
		{"://bad", true},
	}
	for _, c := range cases {
		err := ValidateTargetURLWithConfig(context.Background(), c.url, cfg)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", c.url, err, c.wantErr)
		}
	}

	// With private IPs allowed only the syntax is checked, and DNS is not consulted.
	cfg.AllowPrivateIPs = true
	before := r.calls.Load()
	if err := ValidateTargetURLWithConfig(context.Background(), "http://unknown.test/", cfg); err != nil {
		t.Errorf("allow-private: unexpected error %v", err)
	}
	if err := ValidateTargetURLWithConfig(context.Background(), "ftp://x.test/", cfg); err == nil {
		t.Error("allow-private: scheme must still be checked")
	}
	if r.calls.Load() != before {
		t.Error("allow-private: resolver should not be called")
	}
}

func TestValidateTargetURLExported(t *testing.T) {
	t.Setenv("WEBHOOK_ALLOW_PRIVATE_IPS", "false")
	// IP literals only, so no live DNS is needed.
	if err := ValidateTargetURL("http://127.0.0.1/hook"); err == nil {
		t.Error("loopback should be rejected")
	}
	if err := ValidateTargetURLContext(context.Background(), "http://10.1.2.3/hook"); err == nil {
		t.Error("private IP should be rejected")
	}
	if err := ValidateTargetURL("https://1.1.1.1/hook"); err != nil {
		t.Errorf("public IP rejected: %v", err)
	}
	if err := ValidateTargetURL("gopher://1.1.1.1/"); err == nil {
		t.Error("gopher scheme should be rejected")
	}
}

func TestSecureClientBlocksHostResolvingToPrivateIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request must not reach the server")
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))

	cfg := DefaultHTTPClientConfig()
	cfg.Resolver = &fakeResolver{hosts: map[string][]string{"evil.test": {"127.0.0.1"}}}
	client := NewSecureHTTPClient(cfg)

	_, err := client.Get("http://evil.test:" + port + "/")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("want ErrBlockedAddress, got %v", err)
	}
	if _, err := client.Get(srv.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("IP literal: want ErrBlockedAddress, got %v", err)
	}
}

// rebindResolver returns the safe answer once and a dangerous one afterwards,
// like a DNS-rebinding attacker with a zero TTL.
type rebindResolver struct{ calls atomic.Int32 }

func (r *rebindResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	if r.calls.Add(1) == 1 {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	return []net.IPAddr{{IP: net.ParseIP("10.9.9.9")}}, nil
}

func TestSSRFDialerDialsTheValidatedIP(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))

	res := &rebindResolver{}
	var checked []netip.Addr
	d := &ssrfDialer{
		dialer:   &net.Dialer{Timeout: time.Second},
		resolver: res,
		// Treat only the test server's loopback address as public for this test.
		allow: func(ip netip.Addr) error {
			checked = append(checked, ip)
			if ip.Unmap() != netip.MustParseAddr("127.0.0.1") {
				return ErrBlockedAddress
			}
			return nil
		},
	}
	client := &http.Client{Transport: &http.Transport{DialContext: d.DialContext, DisableKeepAlives: true}}

	resp, err := client.Get("http://hook.test:" + port + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if res.calls.Load() != 1 {
		t.Fatalf("resolver called %d times, want exactly 1 (no second lookup at connect time)", res.calls.Load())
	}
	if len(checked) != 1 || checked[0].Unmap().String() != "127.0.0.1" {
		t.Fatalf("checked %v", checked)
	}
	if gotHost != "hook.test:"+port {
		t.Fatalf("Host header = %q, want the URL host", gotHost)
	}

	// The next connection sees the rebound (private) answer and is refused.
	if _, err := client.Get("http://hook.test:" + port + "/"); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("rebound answer: want ErrBlockedAddress, got %v", err)
	}
}

func TestSecureClientKeepAliveAndRedirects(t *testing.T) {
	cfg := DefaultHTTPClientConfig()
	client := NewSecureHTTPClient(cfg)
	tr := client.Transport.(*http.Transport)
	if tr.DisableKeepAlives || tr.MaxIdleConnsPerHost != 10 || tr.IdleConnTimeout != 90*time.Second {
		t.Fatalf("keep-alive settings: disable=%v perHost=%d idle=%v", tr.DisableKeepAlives, tr.MaxIdleConnsPerHost, tr.IdleConnTimeout)
	}
	if tr.Proxy != nil {
		t.Fatal("proxy must be disabled so it cannot bypass the IP filter")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusFound)
		}
	}))
	defer srv.Close()

	cfg.AllowPrivateIPs = true
	if _, err := NewSecureHTTPClient(cfg).Get(srv.URL + "/redirect"); err == nil {
		t.Fatal("redirect should be refused by default")
	}
	cfg.MaxRedirects = 1
	resp, err := NewSecureHTTPClient(cfg).Get(srv.URL + "/redirect")
	if err != nil {
		t.Fatalf("one redirect allowed: %v", err)
	}
	resp.Body.Close()
	if u, _ := url.Parse(resp.Request.URL.String()); u.Path != "/final" {
		t.Fatalf("final path = %s", u.Path)
	}
}

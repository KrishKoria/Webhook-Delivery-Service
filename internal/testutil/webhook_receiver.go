package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// WebhookReceiver is a mock HTTP server for testing webhook deliveries.
// It records all received requests for later verification.
type WebhookReceiver struct {
	server       *httptest.Server
	mu           sync.Mutex
	requests     []ReceivedRequest
	responseCode int
	responseBody string
	delay        time.Duration
	failAfter    int // Return error after N requests (0 = never fail)
	failUntil    int // Return error until N requests (0 = always succeed)
	requestCount int
}

// ReceivedRequest represents a webhook request that was received.
type ReceivedRequest struct {
	Method    string
	Path      string
	Headers   http.Header
	Body      []byte
	Timestamp time.Time
}

// NewWebhookReceiver creates a new mock webhook receiver server.
func NewWebhookReceiver() *WebhookReceiver {
	wr := &WebhookReceiver{
		requests:     make([]ReceivedRequest, 0),
		responseCode: http.StatusOK,
	}

	wr.server = httptest.NewServer(http.HandlerFunc(wr.handler))
	return wr
}

// URL returns the URL of the mock server.
func (wr *WebhookReceiver) URL() string {
	return wr.server.URL
}

// Close shuts down the mock server.
func (wr *WebhookReceiver) Close() {
	wr.server.Close()
}

// SetResponseCode sets the HTTP status code to return.
func (wr *WebhookReceiver) SetResponseCode(code int) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.responseCode = code
}

// SetResponseBody sets the response body to return.
func (wr *WebhookReceiver) SetResponseBody(body string) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.responseBody = body
}

// SetDelay sets an artificial delay before responding.
func (wr *WebhookReceiver) SetDelay(d time.Duration) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.delay = d
}

// FailAfter configures the server to fail after N successful requests.
// Set to 0 to disable.
func (wr *WebhookReceiver) FailAfter(n int) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.failAfter = n
}

// FailUntil configures the server to fail until N requests are received.
// After N requests, it starts returning success.
// Set to 0 to disable.
func (wr *WebhookReceiver) FailUntil(n int) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.failUntil = n
}

// Reset clears all recorded requests and resets counters.
func (wr *WebhookReceiver) Reset() {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.requests = make([]ReceivedRequest, 0)
	wr.requestCount = 0
	wr.responseCode = http.StatusOK
	wr.responseBody = ""
	wr.delay = 0
	wr.failAfter = 0
	wr.failUntil = 0
}

// Requests returns all received requests.
func (wr *WebhookReceiver) Requests() []ReceivedRequest {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	result := make([]ReceivedRequest, len(wr.requests))
	copy(result, wr.requests)
	return result
}

// RequestCount returns the number of requests received.
func (wr *WebhookReceiver) RequestCount() int {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	return wr.requestCount
}

// LastRequest returns the most recent request, or nil if none.
func (wr *WebhookReceiver) LastRequest() *ReceivedRequest {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	if len(wr.requests) == 0 {
		return nil
	}
	r := wr.requests[len(wr.requests)-1]
	return &r
}

// WaitForRequest waits for at least n requests to be received.
// Returns true if the condition was met, false if timeout occurred.
func (wr *WebhookReceiver) WaitForRequest(n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if wr.RequestCount() >= n {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// handler is the HTTP handler for the mock server.
func (wr *WebhookReceiver) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	wr.mu.Lock()
	wr.requestCount++
	count := wr.requestCount
	delay := wr.delay
	code := wr.responseCode
	respBody := wr.responseBody

	// Check fail conditions
	if wr.failUntil > 0 && count <= wr.failUntil {
		code = http.StatusInternalServerError
	}
	if wr.failAfter > 0 && count > wr.failAfter {
		code = http.StatusInternalServerError
	}

	wr.requests = append(wr.requests, ReceivedRequest{
		Method:    r.Method,
		Path:      r.URL.Path,
		Headers:   r.Header.Clone(),
		Body:      body,
		Timestamp: time.Now(),
	})
	wr.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}

	w.WriteHeader(code)
	if respBody != "" {
		w.Write([]byte(respBody))
	}
}

// ParseJSONBody parses the request body as JSON into the target.
func (r *ReceivedRequest) ParseJSONBody(target interface{}) error {
	return json.Unmarshal(r.Body, target)
}

// GetHeader returns the value of a specific header.
func (r *ReceivedRequest) GetHeader(key string) string {
	return r.Headers.Get(key)
}

// HasHeader checks if a header is present.
func (r *ReceivedRequest) HasHeader(key string) bool {
	return r.Headers.Get(key) != ""
}

// BodyString returns the request body as a string.
func (r *ReceivedRequest) BodyString() string {
	return string(r.Body)
}

// WebhookReceiverPool manages multiple mock receivers for testing
// scenarios with multiple subscriptions.
type WebhookReceiverPool struct {
	receivers map[string]*WebhookReceiver
	mu        sync.Mutex
}

// NewWebhookReceiverPool creates a new pool of mock receivers.
func NewWebhookReceiverPool() *WebhookReceiverPool {
	return &WebhookReceiverPool{
		receivers: make(map[string]*WebhookReceiver),
	}
}

// Get returns or creates a receiver with the given name.
func (p *WebhookReceiverPool) Get(name string) *WebhookReceiver {
	p.mu.Lock()
	defer p.mu.Unlock()

	if r, ok := p.receivers[name]; ok {
		return r
	}

	r := NewWebhookReceiver()
	p.receivers[name] = r
	return r
}

// CloseAll closes all receivers in the pool.
func (p *WebhookReceiverPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, r := range p.receivers {
		r.Close()
	}
	p.receivers = make(map[string]*WebhookReceiver)
}

// ResetAll resets all receivers in the pool.
func (p *WebhookReceiverPool) ResetAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, r := range p.receivers {
		r.Reset()
	}
}

// TotalRequestCount returns the sum of requests across all receivers.
func (p *WebhookReceiverPool) TotalRequestCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	total := 0
	for _, r := range p.receivers {
		total += r.RequestCount()
	}
	return total
}

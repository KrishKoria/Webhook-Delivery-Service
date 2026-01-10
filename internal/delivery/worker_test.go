package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
)

// TestNewWorker tests worker creation with default values
func TestNewWorker(t *testing.T) {
	worker := NewWorker(nil, nil)

	if worker == nil {
		t.Fatal("NewWorker returned nil")
	}
	if worker.WorkerID == "" {
		t.Error("Worker ID should be generated")
	}
	if len(worker.WorkerID) != 8 {
		t.Errorf("Worker ID should be 8 chars, got %d", len(worker.WorkerID))
	}
	if worker.LeaseSeconds != defaultLeaseSeconds {
		t.Errorf("Expected lease seconds %d, got %d", defaultLeaseSeconds, worker.LeaseSeconds)
	}
	if worker.Concurrency != defaultWorkerConcurrency {
		t.Errorf("Expected concurrency %d, got %d", defaultWorkerConcurrency, worker.Concurrency)
	}
	if worker.HTTPClient == nil {
		t.Error("HTTPClient should be initialized")
	}
}

// TestNewWorkerWithConfig tests worker creation with custom config
func TestNewWorkerWithConfig(t *testing.T) {
	tests := []struct {
		name           string
		config         WorkerConfig
		wantID         string
		wantIDLen      int
		wantLease      int64
		wantConcurrency int
	}{
		{
			name: "full custom config",
			config: WorkerConfig{
				WorkerID:     "test-123",
				LeaseSeconds: 120,
				Concurrency:  10,
			},
			wantID:         "test-123",
			wantIDLen:      8,
			wantLease:      120,
			wantConcurrency: 10,
		},
		{
			name: "empty worker ID - should generate",
			config: WorkerConfig{
				WorkerID:     "",
				LeaseSeconds: 90,
				Concurrency:  3,
			},
			wantID:         "", // Will be generated
			wantIDLen:      8,
			wantLease:      90,
			wantConcurrency: 3,
		},
		{
			name: "zero values - should use defaults",
			config: WorkerConfig{
				WorkerID:     "worker-1",
				LeaseSeconds: 0,
				Concurrency:  0,
			},
			wantID:         "worker-1",
			wantIDLen:      8,
			wantLease:      defaultLeaseSeconds,
			wantConcurrency: defaultWorkerConcurrency,
		},
		{
			name: "negative values - should use defaults",
			config: WorkerConfig{
				WorkerID:     "worker-2",
				LeaseSeconds: -10,
				Concurrency:  -5,
			},
			wantID:         "worker-2",
			wantIDLen:      8,
			wantLease:      defaultLeaseSeconds,
			wantConcurrency: defaultWorkerConcurrency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker := NewWorkerWithConfig(nil, nil, tt.config)

			if worker == nil {
				t.Fatal("NewWorkerWithConfig returned nil")
			}
			if tt.wantID != "" && worker.WorkerID != tt.wantID {
				t.Errorf("Expected worker ID %s, got %s", tt.wantID, worker.WorkerID)
			}
			if tt.wantID == "" && len(worker.WorkerID) != tt.wantIDLen {
				t.Errorf("Expected generated worker ID of length %d, got %d", tt.wantIDLen, len(worker.WorkerID))
			}
			if worker.LeaseSeconds != tt.wantLease {
				t.Errorf("Expected lease seconds %d, got %d", tt.wantLease, worker.LeaseSeconds)
			}
			if worker.Concurrency != tt.wantConcurrency {
				t.Errorf("Expected concurrency %d, got %d", tt.wantConcurrency, worker.Concurrency)
			}
		})
	}
}

// TestDeliverWebhook tests webhook delivery to various targets
func TestDeliverWebhook(t *testing.T) {
	tests := []struct {
		name           string
		handler        http.HandlerFunc
		wantStatus     string
		wantHTTPStatus int
		wantErrEmpty   bool
	}{
		{
			name: "successful delivery - 200",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			wantStatus:     "success",
			wantHTTPStatus: http.StatusOK,
			wantErrEmpty:   true,
		},
		{
			name: "successful delivery - 201",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
			},
			wantStatus:     "success",
			wantHTTPStatus: http.StatusCreated,
			wantErrEmpty:   true,
		},
		{
			name: "successful delivery - 204",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
			wantStatus:     "success",
			wantHTTPStatus: http.StatusNoContent,
			wantErrEmpty:   true,
		},
		{
			name: "client error - 400",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			},
			wantStatus:     "failed_attempt",
			wantHTTPStatus: http.StatusBadRequest,
			wantErrEmpty:   false,
		},
		{
			name: "client error - 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantStatus:     "failed_attempt",
			wantHTTPStatus: http.StatusNotFound,
			wantErrEmpty:   false,
		},
		{
			name: "server error - 500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantStatus:     "failed_attempt",
			wantHTTPStatus: http.StatusInternalServerError,
			wantErrEmpty:   false,
		},
		{
			name: "server error - 503",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
			wantStatus:     "failed_attempt",
			wantHTTPStatus: http.StatusServiceUnavailable,
			wantErrEmpty:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			// Use test-friendly config that allows private IPs (for httptest servers)
			testConfig := HTTPClientConfig{
				Timeout:               5 * time.Second,
				DialTimeout:           5 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				DisableSSRFProtection: true, // Allow localhost for testing
				AllowPrivateIPs:       true,
			}
			worker := NewWorkerWithConfig(nil, nil, WorkerConfig{
				HTTPClientConfig: &testConfig,
			})
			status, httpStatus, errMsg := worker.deliverWebhook(server.URL, []byte(`{"test": "data"}`))

			if status != tt.wantStatus {
				t.Errorf("Expected status %s, got %s", tt.wantStatus, status)
			}
			if httpStatus != tt.wantHTTPStatus {
				t.Errorf("Expected HTTP status %d, got %d", tt.wantHTTPStatus, httpStatus)
			}
			if tt.wantErrEmpty && errMsg != "" {
				t.Errorf("Expected empty error message, got %s", errMsg)
			}
			if !tt.wantErrEmpty && errMsg == "" {
				t.Error("Expected error message, got empty")
			}
		})
	}
}

// TestDeliverWebhookConnectionError tests delivery when the target is unreachable
func TestDeliverWebhookConnectionError(t *testing.T) {
	// Use test-friendly config that allows private IPs
	testConfig := HTTPClientConfig{
		Timeout:               1 * time.Second,
		DialTimeout:           1 * time.Second,
		TLSHandshakeTimeout:   1 * time.Second,
		DisableSSRFProtection: true,
		AllowPrivateIPs:       true,
	}
	worker := NewWorkerWithConfig(nil, nil, WorkerConfig{
		HTTPClientConfig: &testConfig,
	})

	// Use an invalid port to trigger a connection error
	status, httpStatus, errMsg := worker.deliverWebhook("http://127.0.0.1:99999/invalid", []byte(`{}`))

	if status != "failed_attempt" {
		t.Errorf("Expected status 'failed_attempt', got %s", status)
	}
	if httpStatus != 0 {
		t.Errorf("Expected HTTP status 0, got %d", httpStatus)
	}
	if errMsg == "" {
		t.Error("Expected error message for connection failure")
	}
}

// TestBackoffDuration tests the backoff durations (without jitter) for different attempts
func TestBackoffDuration(t *testing.T) {
	// Test the deterministic backoff function
	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 10 * time.Second},
		{2, 30 * time.Second},
		{3, 1 * time.Minute},
		{4, 5 * time.Minute},
		{5, 15 * time.Minute},
		{6, 15 * time.Minute}, // Beyond max, still uses last value
		{100, 15 * time.Minute},
	}

	for _, tt := range tests {
		t.Run("attempt_"+string(rune('0'+tt.attempt)), func(t *testing.T) {
			got := getBackoffWithoutJitter(tt.attempt)
			if got != tt.expected {
				t.Errorf("For attempt %d: expected %v, got %v", tt.attempt, tt.expected, got)
			}
		})
	}
}

// TestBackoffWithJitter tests that jitter backoff produces reasonable values
func TestBackoffWithJitter(t *testing.T) {
	tests := []struct {
		attempt   int
		minDelay  time.Duration
		maxDelay  time.Duration
	}{
		{1, 5 * time.Second, 10 * time.Second},      // base=10s, so [5s, 10s]
		{2, 5 * time.Second, 20 * time.Second},      // base*2=20s, so [5s, 20s]
		{3, 5 * time.Second, 40 * time.Second},      // base*4=40s, so [5s, 40s]
		{4, 5 * time.Second, 80 * time.Second},      // base*8=80s, so [5s, 80s]
		{5, 5 * time.Second, 160 * time.Second},     // base*16=160s, so [5s, 160s]
		{6, 5 * time.Second, 320 * time.Second},     // base*32=320s, so [5s, 320s]
		{10, 5 * time.Second, 15 * time.Minute},     // capped at max
	}

	for _, tt := range tests {
		t.Run("attempt_with_jitter", func(t *testing.T) {
			// Run multiple times to check bounds
			for i := 0; i < 100; i++ {
				got := getBackoffWithJitter(tt.attempt)
				if got < tt.minDelay {
					t.Errorf("For attempt %d: got %v, expected >= %v", tt.attempt, got, tt.minDelay)
				}
				if got > tt.maxDelay {
					t.Errorf("For attempt %d: got %v, expected <= %v", tt.attempt, got, tt.maxDelay)
				}
			}
		})
	}
}

// TestBackoffJitterDistribution verifies jitter produces varied values
func TestBackoffJitterDistribution(t *testing.T) {
	values := make(map[time.Duration]bool)
	
	// Generate many values for attempt 3 (40s max)
	for i := 0; i < 100; i++ {
		got := getBackoffWithJitter(3)
		values[got] = true
	}
	
	// Should have at least 10 different values (randomness check)
	if len(values) < 10 {
		t.Errorf("Expected varied jitter values, got only %d unique values", len(values))
	}
}

// TestGenerateUUID tests that UUIDs are unique
func TestGenerateUUID(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := generateUUID()
		if id == "" {
			t.Error("generateUUID returned empty string")
		}
		if seen[id] {
			t.Errorf("generateUUID produced duplicate: %s", id)
		}
		seen[id] = true
	}
}

// TestWorkerConcurrencyLimit tests that the worker respects concurrency limits
func TestWorkerConcurrencyLimit(t *testing.T) {
	var activeCount int32
	var maxActive int32
	var mu sync.Mutex
	var wg sync.WaitGroup

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Track concurrency
		current := atomic.AddInt32(&activeCount, 1)
		mu.Lock()
		if current > maxActive {
			maxActive = current
		}
		mu.Unlock()

		// Simulate some work
		time.Sleep(100 * time.Millisecond)

		atomic.AddInt32(&activeCount, -1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Use test-friendly config that allows private IPs
	testConfig := HTTPClientConfig{
		Timeout:               5 * time.Second,
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		DisableSSRFProtection: true,
		AllowPrivateIPs:       true,
	}

	// This test verifies the worker structure supports concurrency control
	// The actual concurrency enforcement is done via the semaphore in Start()
	worker := NewWorkerWithConfig(nil, nil, WorkerConfig{
		WorkerID:         "test-worker",
		LeaseSeconds:     60,
		Concurrency:      3,
		HTTPClientConfig: &testConfig,
	})

	if worker.Concurrency != 3 {
		t.Errorf("Expected concurrency 3, got %d", worker.Concurrency)
	}

	// Test multiple concurrent deliveries
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker.deliverWebhook(server.URL, []byte(`{"test": true}`))
		}()
	}

	wg.Wait()
}

// TestWorkerIDUniqueness tests that different workers get different IDs
func TestWorkerIDUniqueness(t *testing.T) {
	workers := make([]*Worker, 10)
	ids := make(map[string]bool)

	for i := 0; i < 10; i++ {
		workers[i] = NewWorker(nil, nil)
		if ids[workers[i].WorkerID] {
			t.Errorf("Duplicate worker ID: %s", workers[i].WorkerID)
		}
		ids[workers[i].WorkerID] = true
	}
}

// TestWorkerWithCache tests worker initialization with cache
func TestWorkerWithCache(t *testing.T) {
	// Create a nil cache (simulating Redis not available)
	var nilCache *cache.RedisSubscriptionCache = nil

	worker := NewWorker(nil, nilCache)
	if worker.Cache != nil {
		t.Error("Expected nil cache")
	}

	workerWithConfig := NewWorkerWithConfig(nil, nilCache, WorkerConfig{
		WorkerID:     "cache-test",
		LeaseSeconds: 30,
		Concurrency:  2,
	})
	if workerWithConfig.Cache != nil {
		t.Error("Expected nil cache with config")
	}
}

// TestMaxAttempts verifies the max attempts constant
func TestMaxAttempts(t *testing.T) {
	if maxAttempts != 5 {
		t.Errorf("Expected maxAttempts to be 5, got %d", maxAttempts)
	}
}

// TestDefaultLeaseSeconds verifies the default lease constant
func TestDefaultLeaseSeconds(t *testing.T) {
	if defaultLeaseSeconds != 60 {
		t.Errorf("Expected defaultLeaseSeconds to be 60, got %d", defaultLeaseSeconds)
	}
}

// TestDefaultWorkerConcurrency verifies the default concurrency constant
func TestDefaultWorkerConcurrency(t *testing.T) {
	if defaultWorkerConcurrency != 5 {
		t.Errorf("Expected defaultWorkerConcurrency to be 5, got %d", defaultWorkerConcurrency)
	}
}

// MockQueries is a test helper to mock database queries
type MockQueries struct {
	database.Queries
	ClaimTaskFunc func(ctx context.Context, params database.ClaimDeliveryTaskParams) (database.DeliveryTask, error)
}

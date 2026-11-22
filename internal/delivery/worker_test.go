package delivery

import (
	"net/http"
	"testing"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
)

// Reference vector computed independently of this package:
//
//	printf '%s' 'msg_2Kx7Q.1700000000.{"type":"order.created","id":42}' \
//	  | openssl dgst -sha256 -hmac 'whsec-test-secret' -binary | base64
func TestSignReferenceVector(t *testing.T) {
	got := Sign("whsec-test-secret", "msg_2Kx7Q", 1700000000, []byte(`{"type":"order.created","id":42}`))
	const want = "v1,/4KJClrQ1QZ8ipB9LiWQ2FKNvkDix9XLlGEwSXDajig="
	if got != want {
		t.Fatalf("Sign = %q, want %q", got, want)
	}
}

// Test vector published with the Standard Webhooks specification; also
// reproduced with openssl using the base64-decoded key.
func TestSignStandardWebhooksVector(t *testing.T) {
	got := Sign("whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw", "msg_p5jXN8AQM9LWM0D4loKWxJek", 1614265330, []byte(`{"test": 2432232314}`))
	const want = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
	if got != want {
		t.Fatalf("Sign = %q, want %q", got, want)
	}
	// A malformed whsec_ value falls back to raw bytes instead of failing.
	if Sign("whsec_!!", "m", 1, nil) != Sign("whsec_!!", "m", 1, nil) || string(SigningKey("whsec_!!")) != "whsec_!!" {
		t.Fatal("malformed whsec_ secret should be used as raw bytes")
	}
}

func TestBackoffDelayIsTotalAndMatchesSchedule(t *testing.T) {
	identity := func(d time.Duration) time.Duration { return d }
	want := []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute}
	for i, w := range want {
		if got := backoffDelay(DefaultRetrySchedule, int64(i+1), identity); got != w {
			t.Errorf("attempt %d: got %v want %v", i+1, got, w)
		}
	}
	for _, attempt := range []int64{-5, 0} {
		if got := backoffDelay(DefaultRetrySchedule, attempt, identity); got != 10*time.Second {
			t.Errorf("attempt %d: got %v want first entry", attempt, got)
		}
	}
	if got := backoffDelay(DefaultRetrySchedule, 1<<62, identity); got != 15*time.Minute {
		t.Errorf("huge attempt: got %v want last entry", got)
	}
	if got := backoffDelay(nil, 2, identity); got != 30*time.Second {
		t.Errorf("empty schedule should fall back to default: got %v", got)
	}
}

func TestJitterWithinTwentyPercent(t *testing.T) {
	base := 10 * time.Second
	for range 1000 {
		d := jitter20(base)
		if d < 8*time.Second || d > 12*time.Second {
			t.Fatalf("jitter %v outside ±20%%", d)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"":                              0,
		"120":                           120 * time.Second,
		"0":                             0,
		"-5":                            0,
		"999999":                        time.Hour,
		"garbage":                       0,
		"Thu, 01 Jan 2026 12:05:00 GMT": 5 * time.Minute,
		"Thu, 01 Jan 2026 11:00:00 GMT": 0, // in the past
		"Fri, 02 Jan 2026 12:00:00 GMT": time.Hour,
	}
	for h, want := range cases {
		if got := parseRetryAfter(h, now); got != want {
			t.Errorf("Retry-After %q: got %v want %v", h, got, want)
		}
	}
}

func TestDecide(t *testing.T) {
	w := NewWorker(nil, nil, WorkerConfig{
		RetrySchedule: []time.Duration{10 * time.Second},
		DisableJitter: true,
		MaxAttempts:   6,
	})
	task := func(attempts int64) database.DeliveryTask {
		return database.DeliveryTask{AttemptCount: attempts}
	}
	fail := func(status int, retryAfter time.Duration) attemptResult {
		return attemptResult{Outcome: OutcomeFailedAttempt, HTTPStatus: status, RetryAfter: retryAfter}
	}

	cases := []struct {
		name string
		task database.DeliveryTask
		res  attemptResult
		want decision
	}{
		{"success", task(0), attemptResult{Outcome: OutcomeSuccess, HTTPStatus: 200}, decision{Status: StatusDelivered}},
		{"500 retries", task(0), fail(500, 0), decision{Status: StatusPending, Delay: 10 * time.Second}},
		{"network error retries", task(2), fail(0, 0), decision{Status: StatusPending, Delay: 10 * time.Second}},
		{"410 dead-letters immediately", task(0), fail(http.StatusGone, 0), decision{Status: StatusFailed, DLQReason: ReasonGone}},
		{"6th attempt is final", task(5), fail(500, 0), decision{Status: StatusFailed, DLQReason: ReasonMaxAttempts}},
		{"retry-after longer than schedule wins", task(0), fail(503, 2*time.Minute), decision{Status: StatusPending, Delay: 2 * time.Minute}},
		{"retry-after shorter than schedule is ignored", task(0), fail(429, time.Second), decision{Status: StatusPending, Delay: 10 * time.Second}},
		{"retry-after capped at 1h", task(0), fail(429, 5*time.Hour), decision{Status: StatusPending, Delay: time.Hour}},
	}
	for _, c := range cases {
		if got := w.decide(c.task, c.res); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
}

func TestWorkerConfigDefaults(t *testing.T) {
	cfg := WorkerConfig{}.withDefaults()
	if cfg.Concurrency != 10 || cfg.MaxAttempts != 6 || cfg.IdleMax != 2*time.Second || cfg.IdleMin != 250*time.Millisecond {
		t.Fatalf("defaults: %+v", cfg)
	}
	if cfg.Lease != 60*time.Second {
		t.Fatalf("lease with 30s timeout = %v, want 60s", cfg.Lease)
	}
	cfg = WorkerConfig{HTTP: HTTPClientConfig{Timeout: 45 * time.Second}}.withDefaults()
	if cfg.Lease != 90*time.Second {
		t.Fatalf("lease with 45s timeout = %v, want 90s", cfg.Lease)
	}
	if len(cfg.WorkerID) == 0 {
		t.Fatal("worker id should be generated")
	}
}

func TestLoadWorkerConfigFromEnv(t *testing.T) {
	t.Setenv("WORKER_CONCURRENCY", "3")
	t.Setenv("WORKER_IDLE_MAX", "500ms")
	t.Setenv("WORKER_MAX_ATTEMPTS", "4")
	t.Setenv("WORKER_ID", "w-env")
	cfg := LoadWorkerConfigFromEnv()
	if cfg.Concurrency != 3 || cfg.IdleMax != 500*time.Millisecond || cfg.MaxAttempts != 4 || cfg.WorkerID != "w-env" {
		t.Fatalf("env config: %+v", cfg)
	}
	t.Setenv("WORKER_CONCURRENCY", "-1")
	if cfg := LoadWorkerConfigFromEnv().withDefaults(); cfg.Concurrency != 10 {
		t.Fatalf("invalid concurrency should default, got %d", cfg.Concurrency)
	}
}

func TestCeilSeconds(t *testing.T) {
	cases := map[time.Duration]int64{-time.Second: 0, 0: 0, time.Millisecond: 1, time.Second: 1, 1500 * time.Millisecond: 2}
	for d, want := range cases {
		if got := ceilSeconds(d); got != want {
			t.Errorf("ceilSeconds(%v)=%d want %d", d, got, want)
		}
	}
}

package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// seedUI inserts one of everything the admin pages display.
func seedUI(t *testing.T, h *harness) (subID string) {
	t.Helper()
	subID = h.createSub(`{"target_url":"https://example.com/hooks","secret":"s","event_types":"order.created"}`)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := h.conn.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO delivery_tasks (id, subscription_id, payload, status, attempt_count, last_attempt_at, next_attempt_at, event_type)
		VALUES ('task-delivered', ?, '{"a":1}', 'delivered', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'order.created')`, subID)
	exec(`INSERT INTO delivery_tasks (id, subscription_id, payload, status, attempt_count, last_attempt_at, next_attempt_at)
		VALUES ('task-retrying', ?, '{"a":2}', 'pending', 2, CURRENT_TIMESTAMP, datetime('now', '+5 minutes'))`, subID)
	exec(`INSERT INTO delivery_tasks (id, subscription_id, payload, status, attempt_count, claimed_by, claimed_at, lease_until, next_attempt_at)
		VALUES ('task-flight', ?, '{"a":3}', 'in_flight', 0, 'worker-1', CURRENT_TIMESTAMP, datetime('now', '+1 minute'), CURRENT_TIMESTAMP)`, subID)
	exec(`INSERT INTO delivery_logs (id, delivery_task_id, subscription_id, target_url, timestamp, attempt_number, outcome, http_status)
		VALUES ('log-ok', 'task-delivered', ?, 'https://example.com/hooks', CURRENT_TIMESTAMP, 1, 'success', 200)`, subID)
	exec(`INSERT INTO delivery_logs (id, delivery_task_id, subscription_id, target_url, timestamp, attempt_number, outcome, http_status, error_details)
		VALUES ('log-bad', 'task-retrying', ?, 'https://example.com/hooks', CURRENT_TIMESTAMP, 2, 'failed_attempt', 503, '<script>alert(1)</script>')`, subID)
	exec(`INSERT INTO dead_letter_tasks (id, original_task_id, subscription_id, payload, reason, status, attempt_count, last_attempt_at, target_url, event_type, error_details)
		VALUES ('dlq-1', 'task-x', ?, '{"order":42}', 'max attempts exhausted', 'dead', 6, CURRENT_TIMESTAMP, 'https://example.com/hooks', 'order.created', 'HTTP 500')`, subID)
	exec(`INSERT INTO scheduled_webhooks (id, subscription_id, payload, scheduled_for, recurrence)
		VALUES ('sched-1', ?, '{"daily":true}', datetime('now', '+1 day'), 'daily')`, subID)
	return subID
}

func TestUIPagesRender(t *testing.T) {
	h := newHarness(t)
	subID := seedUI(t, h)
	sub := "/ui/subscriptions/" + subID

	pages := map[string]string{
		"/ui/dashboard":               "of delivery attempts succeeded",
		"/ui/backlog":                 "Waiting by subscription",
		"/ui/subscriptions":           "https://example.com/hooks",
		"/ui/subscriptions/new":       "Create subscription",
		sub + "/edit":                 "A secret is set",
		sub + "/test":                 "Queue test event",
		sub + "/logs":                 "failed attempt",
		sub + "/logs?outcome=success": "success",
		sub + "/analytics?hours=168":  "7 days",
		sub + "/scheduled":            "daily",
		sub + "/scheduled/new":        "Schedule webhook",
		"/ui/dlq":                     "max attempts exhausted",
		"/ui/dlq?status=retried":      "No entries match this filter",
		"/ui/dlq/dlq-1":               "&#34;order&#34;: 42",
		"/ui/scheduled":               "Scheduled webhooks",
	}
	for path, want := range pages {
		t.Run(path, func(t *testing.T) {
			w := h.do(http.MethodGet, path, "", withAuth)
			if w.Code != http.StatusOK {
				t.Fatalf("GET %s = %d\n%s", path, w.Code, w.Body.String())
			}
			body := w.Body.String()
			if !strings.Contains(body, "<!doctype html>") || !strings.Contains(body, want) {
				t.Fatalf("GET %s: missing %q", path, want)
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
				t.Fatal("missing CSP header")
			}
			if strings.Contains(body, "<script>alert(1)</script>") {
				t.Fatal("unescaped error details (stored XSS)")
			}
			if strings.Contains(body, "style=") || strings.Contains(body, "onclick") {
				t.Fatal("inline style or handler violates the CSP")
			}
		})
	}
}

func TestUIRequiresAuth(t *testing.T) {
	h := newHarness(t)
	if w := h.do(http.MethodGet, "/ui/dashboard", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no auth = %d, want 401", w.Code)
	}
}

func TestUIPollingReturnsFragments(t *testing.T) {
	h := newHarness(t)
	subID := seedUI(t, h)
	hx := withHeader("HX-Request", "true")
	for path, id := range map[string]string{
		"/ui/dashboard":                        `id="dashboard-stats"`,
		"/ui/backlog":                          `id="backlog-body"`,
		"/ui/subscriptions/" + subID + "/logs": `id="logs"`,
	} {
		w := h.do(http.MethodGet, path, "", withAuth, hx)
		body := w.Body.String()
		if w.Code != http.StatusOK || strings.Contains(body, "<html") || !strings.Contains(body, id) {
			t.Fatalf("GET %s fragment: %d\n%s", path, w.Code, body)
		}
		if !strings.Contains(body, `id="last-updated" hx-swap-oob="true"`) {
			t.Fatalf("GET %s fragment lacks the last-updated OOB stamp", path)
		}
	}
	// A full page must not carry the OOB copy (duplicate id).
	if body := h.do(http.MethodGet, "/ui/dashboard", "", withAuth).Body.String(); strings.Count(body, `id="last-updated"`) != 1 {
		t.Fatal("full page renders #last-updated more than once")
	}
}

func form(vals url.Values) (string, reqOpt) {
	return vals.Encode(), withHeader("Content-Type", "application/x-www-form-urlencoded")
}

func TestUIMutations(t *testing.T) {
	h := newHarness(t)
	subID := seedUI(t, h)
	hx := withHeader("HX-Request", "true")

	// Invalid form re-renders as a 422 fragment with the error.
	body, ct := form(url.Values{"target_url": {"not a url"}})
	w := h.do(http.MethodPost, "/ui/subscriptions", body, withAuth, ct, hx)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `id="sub-form"`) || !strings.Contains(w.Body.String(), `aria-invalid="true"`) {
		t.Fatalf("invalid create = %d\n%s", w.Code, w.Body.String())
	}

	// Valid form without htmx redirects with a flash.
	body, ct = form(url.Values{"target_url": {"https://example.com/new"}})
	w = h.do(http.MethodPost, "/ui/subscriptions", body, withAuth, ct)
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "flash=sub_created") {
		t.Fatalf("create = %d %v", w.Code, w.Header())
	}

	// Pause from the list swaps the row and adds a toast.
	body, ct = form(url.Values{"return": {"list"}})
	w = h.do(http.MethodPost, "/ui/subscriptions/"+subID+"/pause", body, withAuth, ct, hx)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="sub-`+subID+`"`) || !strings.Contains(w.Body.String(), "paused") || !strings.Contains(w.Body.String(), "hx-swap-oob") {
		t.Fatalf("pause = %d\n%s", w.Code, w.Body.String())
	}

	// DLQ retry swaps the row (now retried); a second retry is a 409 row with an info toast.
	body, ct = form(url.Values{"return": {"/ui/dlq"}})
	w = h.do(http.MethodPost, "/ui/dlq/dlq-1/retry", body, withAuth, ct, hx)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "retried") {
		t.Fatalf("dlq retry = %d\n%s", w.Code, w.Body.String())
	}
	w = h.do(http.MethodPost, "/ui/dlq/dlq-1/retry", body, withAuth, ct, hx)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "already retried") {
		t.Fatalf("second dlq retry = %d\n%s", w.Code, w.Body.String())
	}

	// Cancel a schedule from the global list.
	body, ct = form(url.Values{"return": {"/ui/scheduled"}, "global": {"1"}})
	w = h.do(http.MethodPost, "/ui/scheduled/sched-1/cancel", body, withAuth, ct, hx)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "cancelled") {
		t.Fatalf("cancel schedule = %d\n%s", w.Code, w.Body.String())
	}

	// Schedule form: past time is a 422 with a field error.
	body, ct = form(url.Values{"scheduled_for_local": {"2001-01-01T00:00"}, "tz_offset": {"0"}, "recurrence": {"none"}, "payload": {`{}`}})
	w = h.do(http.MethodPost, "/ui/subscriptions/"+subID+"/scheduled", body, withAuth, ct, hx)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "Pick a time in the future") {
		t.Fatalf("past schedule = %d\n%s", w.Code, w.Body.String())
	}

	// Unknown entity on an htmx request: toast only, so htmx leaves the page alone.
	w = h.do(http.MethodPost, "/ui/dlq/nope/delete", body, withAuth, ct, hx)
	if w.Code != http.StatusNotFound || !strings.HasPrefix(strings.TrimSpace(w.Body.String()), `<div hx-swap-oob`) {
		t.Fatalf("missing dlq delete = %d\n%s", w.Code, w.Body.String())
	}
	// ... and a styled page without htmx.
	if w := h.do(http.MethodGet, "/ui/dlq/nope", "", withAuth); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "Not found") {
		t.Fatalf("missing dlq page = %d", w.Code)
	}
}

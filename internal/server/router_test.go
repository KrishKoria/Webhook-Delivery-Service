package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/testdb"
	"github.com/gin-gonic/gin"
)

const (
	testUser = "admin"
	testPass = "s3cret-pass"
)

type harness struct {
	t    *testing.T
	conn *sql.DB
	h    http.Handler
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("ADMIN_USERNAME", testUser)
	t.Setenv("ADMIN_PASSWORD", testPass)
	t.Setenv("WEBHOOK_ALLOW_PRIVATE_IPS", "true") // no DNS lookups for example.com targets
	t.Setenv("INGEST_MAX_BODY_SIZE", "1024")
	conn, _ := testdb.New(t)
	r, err := NewRouter(Deps{Context: t.Context(), DB: conn, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return &harness{t: t, conn: conn, h: r}
}

type reqOpt func(*http.Request)

func withAuth(r *http.Request)      { r.SetBasicAuth(testUser, testPass) }
func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func withJSON(r *http.Request)      { r.Header.Set("Content-Type", "application/json") }
func withCrossSite(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }

func (h *harness) do(method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	h.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	for _, o := range opts {
		o(req)
	}
	w := httptest.NewRecorder()
	h.h.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not JSON (%d): %s", w.Code, w.Body.String())
	}
	return m
}

func dataField(t *testing.T, w *httptest.ResponseRecorder, key string) any {
	t.Helper()
	data, ok := decode(t, w)["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data object in %s", w.Body.String())
	}
	return data[key]
}

func (h *harness) createSub(body string) string {
	h.t.Helper()
	w := h.do(http.MethodPost, "/api/subscriptions", body, withAuth, withJSON)
	if w.Code != http.StatusCreated {
		h.t.Fatalf("create subscription: %d %s", w.Code, w.Body.String())
	}
	id, _ := dataField(h.t, w, "id").(string)
	if id == "" {
		h.t.Fatalf("no id in %s", w.Body.String())
	}
	return id
}

// --- wiring ------------------------------------------------------------------

// NewRouter panics on duplicate routes, so building it is itself the wiring test.
func TestUnknownRoutesMatchTheirSurface(t *testing.T) {
	h := newHarness(t)
	if w := h.do(http.MethodGet, "/ui/nope", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown UI path without auth = %d, want 401", w.Code)
	}
	w := h.do(http.MethodGet, "/ui/nope", "", withAuth)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "<!doctype html>") || !strings.Contains(w.Body.String(), "Not found") {
		t.Fatalf("unknown UI path = %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("unknown UI path is missing the UI security headers")
	}
	w = h.do(http.MethodGet, "/api/nope", "")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"code":"NOT_FOUND"`) {
		t.Fatalf("unknown API path = %d %s", w.Code, w.Body.String())
	}
}

func TestRouterBuildsWithoutRouteConflicts(t *testing.T) {
	newHarness(t)
}

func TestRouterIsOpenWithoutAdminCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("ADMIN_USERNAME", "")
	t.Setenv("ADMIN_PASSWORD", "")
	conn, _ := testdb.New(t)
	r, err := NewRouter(Deps{Context: t.Context(), DB: conn, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("NewRouter without admin credentials: %v", err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ui/nope", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown UI path without credentials configured = %d, want 404 (no login)", w.Code)
	}
}

func TestRouterRejectsHalfConfiguredAdminCredentials(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "")
	conn, _ := testdb.New(t)
	if _, err := NewRouter(Deps{DB: conn, Logger: slog.New(slog.DiscardHandler)}); err == nil {
		t.Fatal("NewRouter succeeded with only ADMIN_USERNAME set")
	}
}

func TestProbes(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/healthz", "/livez", "/readyz"} {
		if w := h.do(http.MethodGet, p, ""); w.Code != http.StatusOK {
			t.Errorf("GET %s = %d %s", p, w.Code, w.Body.String())
		}
	}
	if w := h.do(http.MethodGet, "/api/health/system", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("/api/health/system without auth = %d, want 401", w.Code)
	}
}

func TestReadyzUnavailableWhenDatabaseIsDown(t *testing.T) {
	h := newHarness(t)
	_ = h.conn.Close()
	w := h.do(http.MethodGet, "/readyz", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with closed DB = %d, want 503", w.Code)
	}
	if strings.Contains(w.Body.String(), "closed") {
		t.Fatalf("readyz leaked the driver error: %s", w.Body.String())
	}
}

// --- admin API guards ----------------------------------------------------------

func TestAdminAPIRequiresAuthAndAnswersJSON(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodGet, "/api/subscriptions", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no auth = %d, want 401", w.Code)
	}
	if _, ok := decode(t, w)["error"]; !ok {
		t.Fatalf("401 is not a JSON error envelope: %s", w.Body.String())
	}
	if w := h.do(http.MethodGet, "/api/subscriptions", "", withAuth); w.Code != http.StatusOK {
		t.Fatalf("with auth = %d %s", w.Code, w.Body.String())
	}
}

func TestAdminAPIRejectsCrossOriginWrites(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/api/subscriptions", `{"target_url":"https://example.com/h"}`, withAuth, withJSON, withCrossSite)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST = %d, want 403", w.Code)
	}
	// Reads are not state-changing and pass.
	if w := h.do(http.MethodGet, "/api/subscriptions", "", withAuth, withCrossSite); w.Code != http.StatusOK {
		t.Fatalf("cross-site GET = %d, want 200", w.Code)
	}
}

func TestAdminUIRejectsCrossOriginWrites(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/ui/subscriptions", "target_url=https://example.com/h", withAuth, withCrossSite,
		withHeader("Content-Type", "application/x-www-form-urlencoded"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site UI form POST = %d, want 403", w.Code)
	}
}

func TestAdminAPIRequiresJSONBodies(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/api/subscriptions", "target_url=x", withAuth, withHeader("Content-Type", "application/x-www-form-urlencoded"))
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form body = %d, want 415", w.Code)
	}
}

// --- subscriptions ---------------------------------------------------------------

func TestUpdatePreservesSecretUnlessCleared(t *testing.T) {
	h := newHarness(t)
	id := h.createSub(`{"target_url":"https://example.com/a","secret":"keep-me"}`)

	secret := func() sql.NullString {
		var s sql.NullString
		if err := h.conn.QueryRow(`SELECT secret FROM subscriptions WHERE id = ?`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	w := h.do(http.MethodPut, "/api/subscriptions/"+id, `{"target_url":"https://example.com/b"}`, withAuth, withJSON)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if s := secret(); s.String != "keep-me" {
		t.Fatalf("secret after update without secret = %+v, want keep-me", s)
	}
	if strings.Contains(w.Body.String(), "keep-me") {
		t.Fatal("update response leaked the secret")
	}

	w = h.do(http.MethodPut, "/api/subscriptions/"+id, `{"target_url":"https://example.com/b","clear_secret":true}`, withAuth, withJSON)
	if w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	if s := secret(); s.Valid && s.String != "" {
		t.Fatalf("secret after clear_secret = %+v, want empty", s)
	}
}

func TestCreateRejectsPrivateTargetsByDefault(t *testing.T) {
	h := newHarness(t)
	t.Setenv("WEBHOOK_ALLOW_PRIVATE_IPS", "")
	conn, _ := testdb.New(t)
	r, err := NewRouter(Deps{Context: t.Context(), DB: conn, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	h.h = r
	w := h.do(http.MethodPost, "/api/subscriptions", `{"target_url":"http://169.254.169.254/latest"}`, withAuth, withJSON)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("metadata-IP target = %d, want 400", w.Code)
	}
}

// --- ingest ---------------------------------------------------------------------

func TestIngestIdempotencyKey(t *testing.T) {
	h := newHarness(t)
	id := h.createSub(`{"target_url":"https://example.com/a"}`)
	key := withHeader("Idempotency-Key", "evt-1")

	w1 := h.do(http.MethodPost, "/ingest/"+id, `{"n":1}`, withJSON, key)
	if w1.Code != http.StatusAccepted {
		t.Fatalf("first ingest = %d %s", w1.Code, w1.Body.String())
	}
	w2 := h.do(http.MethodPost, "/ingest/"+id, `{"n":1}`, withJSON, key)
	if w2.Code != http.StatusOK {
		t.Fatalf("repeat ingest = %d, want 200", w2.Code)
	}
	if a, b := dataField(t, w1, "task_id"), dataField(t, w2, "task_id"); a != b {
		t.Fatalf("repeat returned task %v, want %v", b, a)
	}
	var n int
	_ = h.conn.QueryRow(`SELECT COUNT(*) FROM delivery_tasks WHERE subscription_id = ?`, id).Scan(&n)
	if n != 1 {
		t.Fatalf("%d tasks created, want 1", n)
	}
}

func TestIngestStatusCodes(t *testing.T) {
	h := newHarness(t)
	open := h.createSub(`{"target_url":"https://example.com/a","event_types":"order.created"}`)
	signed := h.createSub(`{"target_url":"https://example.com/b","secret":"shh"}`)

	sign := func(body string) reqOpt {
		mac := hmac.New(sha256.New, []byte("shh"))
		mac.Write([]byte(body))
		return withHeader("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	tests := []struct {
		name string
		path string
		body string
		opts []reqOpt
		want int
	}{
		{"non-JSON content type", "/ingest/" + open, `{}`, []reqOpt{withHeader("Content-Type", "text/plain")}, http.StatusUnsupportedMediaType},
		{"unknown subscription", "/ingest/nope", `{}`, []reqOpt{withJSON}, http.StatusNotFound},
		{"invalid JSON", "/ingest/" + open, `{nope`, []reqOpt{withJSON}, http.StatusBadRequest},
		{"too large", "/ingest/" + open, `{"x":"` + strings.Repeat("a", 2048) + `"}`, []reqOpt{withJSON}, http.StatusRequestEntityTooLarge},
		{"event not in allowlist", "/ingest/" + open, `{}`, []reqOpt{withJSON, withHeader("X-Event-Type", "order.deleted")}, http.StatusNoContent},
		{"allowed event", "/ingest/" + open, `{}`, []reqOpt{withJSON, withHeader("X-Event-Type", "order.created")}, http.StatusAccepted},
		{"missing signature", "/ingest/" + signed, `{}`, []reqOpt{withJSON}, http.StatusUnauthorized},
		{"valid signature", "/ingest/" + signed, `{"ok":1}`, []reqOpt{withJSON, sign(`{"ok":1}`)}, http.StatusAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w := h.do(http.MethodPost, tt.path, tt.body, tt.opts...); w.Code != tt.want {
				t.Fatalf("= %d, want %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestIngestUnavailableOnDatabaseError(t *testing.T) {
	h := newHarness(t)
	id := h.createSub(`{"target_url":"https://example.com/a"}`)
	_ = h.conn.Close()
	w := h.do(http.MethodPost, "/ingest/"+id, `{}`, withJSON)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("ingest with DB down = %d, want 503 (not 404)", w.Code)
	}
	if w.Header().Get("Retry-After") != "5" {
		t.Fatalf("Retry-After = %q, want 5", w.Header().Get("Retry-After"))
	}
}

package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/ops"
	"github.com/gin-gonic/gin"
)

// UI serves the server-rendered admin interface. Reads go through Queries;
// every mutation goes through ops so the UI and JSON API share one code path.
type UI struct {
	Q     *database.Queries
	Ops   *ops.Ops
	Cache *cache.RedisSubscriptionCache
	Log   *slog.Logger
	// AllowPrivateTargets skips the SSRF check on target URLs (WEBHOOK_ALLOW_PRIVATE_IPS, local dev only).
	AllowPrivateTargets bool
}

// NewUI builds the admin UI handlers on conn.
func NewUI(conn *sql.DB, c *cache.RedisSubscriptionCache, logger *slog.Logger) *UI {
	if logger == nil {
		logger = slog.Default()
	}
	invalidate := func(id string) {
		if c != nil {
			c.Del(id)
		}
	}
	return &UI{Q: database.New(conn), Ops: ops.New(conn, invalidate), Cache: c, Log: logger}
}

// RegisterAdminUI registers every /ui route (plus the "/" redirect) on rg.
func RegisterAdminUI(rg *gin.RouterGroup, u *UI) {
	toDashboard := func(c *gin.Context) { c.Redirect(http.StatusFound, "/ui/dashboard") }
	rg.GET("/", toDashboard)
	rg.GET("/ui", toDashboard)
	rg.GET("/ui/dashboard", u.Dashboard)
	rg.GET("/ui/backlog", u.Backlog)

	rg.GET("/ui/subscriptions", u.ListSubscriptions)
	rg.POST("/ui/subscriptions", u.CreateSubscription)
	rg.GET("/ui/subscriptions/new", u.NewSubscription)
	rg.GET("/ui/subscriptions/:id", u.SubscriptionHome)
	rg.GET("/ui/subscriptions/:id/edit", u.EditSubscription)
	rg.POST("/ui/subscriptions/:id/edit", u.UpdateSubscription)
	rg.POST("/ui/subscriptions/:id/delete", u.DeleteSubscription)
	rg.POST("/ui/subscriptions/:id/pause", u.PauseSubscription)
	rg.POST("/ui/subscriptions/:id/resume", u.ResumeSubscription)
	rg.GET("/ui/subscriptions/:id/test", u.TestForm)
	rg.POST("/ui/subscriptions/:id/test", u.SendTest)
	rg.GET("/ui/subscriptions/:id/logs", u.Logs)
	rg.GET("/ui/subscriptions/:id/analytics", u.Analytics)
	rg.GET("/ui/subscriptions/:id/dlq", u.SubscriptionDLQ)
	rg.GET("/ui/subscriptions/:id/scheduled", u.SubscriptionScheduled)
	rg.POST("/ui/subscriptions/:id/scheduled", u.CreateScheduled)
	rg.GET("/ui/subscriptions/:id/scheduled/new", u.NewScheduled)
	rg.GET("/ui/subscriptions/:id/scheduled/list", u.legacyScheduledList)

	rg.POST("/ui/tasks/:task_id/redeliver", u.Redeliver)

	rg.GET("/ui/dlq", u.ListDLQ)
	rg.POST("/ui/dlq/bulk-retry", u.BulkRetryDLQ)
	rg.POST("/ui/dlq/bulk-delete", u.BulkDeleteDLQ)
	rg.GET("/ui/dlq/:dlq_id", u.DLQDetail)
	rg.POST("/ui/dlq/:dlq_id/retry", u.RetryDLQ)
	rg.POST("/ui/dlq/:dlq_id/delete", u.DeleteDLQ)

	rg.GET("/ui/scheduled", u.ListScheduled)
	rg.POST("/ui/scheduled/:id/cancel", u.CancelScheduled)
	rg.POST("/ui/scheduled/:id/delete", u.DeleteScheduled)
}

// ViewPage is the data every full page receives; Data holds the page-specific view.
type ViewPage struct {
	Title    string
	Nav      string // which primary nav item is current
	Flash    *Flash
	Degraded []string // sections that failed to load
	Live     bool     // the page polls; show the live status and pause toggle
	Now      time.Time
	Data     any
	// Fragment is set when only a partial is rendered (htmx request), so
	// polled regions can add out-of-band updates a full page must not repeat.
	Fragment bool
}

// Flash is a one-off status message.
type Flash struct {
	Kind string // ok | error | info
	Msg  string
}

// flashMessages are the only messages a ?flash= query can show, so the
// parameter cannot be used to inject arbitrary text.
var flashMessages = map[string]Flash{
	"sub_created":      {"ok", "Subscription created."},
	"sub_updated":      {"ok", "Subscription saved."},
	"sub_deleted":      {"ok", "Subscription deleted, along with its logs, tasks and schedules."},
	"sub_paused":       {"ok", "Deliveries paused. New events still queue up."},
	"sub_resumed":      {"ok", "Deliveries resumed."},
	"test_sent":        {"ok", "Test event queued. It appears below once the worker picks it up."},
	"redelivered":      {"ok", "Redelivery queued as a new task."},
	"dlq_retried":      {"ok", "Entry moved back to the delivery queue."},
	"dlq_conflict":     {"info", "That entry was already retried."},
	"dlq_deleted":      {"ok", "Entry deleted."},
	"dlq_bulk_retried": {"ok", "%d %s moved back to the delivery queue."},
	"dlq_bulk_partial": {"info", "%d %s retried; the rest were already retried or gone."},
	"dlq_bulk_deleted": {"ok", "%d %s deleted."},
	"dlq_none":         {"info", "Select at least one entry first."},
	"sched_created":    {"ok", "Webhook scheduled."},
	"sched_cancelled":  {"ok", "Scheduled webhook cancelled."},
	"sched_deleted":    {"ok", "Scheduled webhook deleted."},
	"sched_conflict":   {"info", "Only pending schedules can be cancelled."},
}

func flashFromQuery(c *gin.Context) *Flash {
	f, ok := flashMessages[c.Query("flash")]
	if !ok {
		return nil
	}
	if n, err := strconv.Atoi(c.Query("n")); err == nil && n >= 0 {
		f.Msg = fmt.Sprintf(f.Msg, n, map[bool]string{true: "entry", false: "entries"}[n == 1])
	}
	return &f
}

func (u *UI) page(c *gin.Context, title, nav string, data any) *ViewPage {
	return &ViewPage{Title: title, Nav: nav, Flash: flashFromQuery(c), Now: time.Now().UTC(), Data: data}
}

// degrade records a failed read so the page can show a banner instead of
// silently rendering zeros.
func (u *UI) degrade(c *gin.Context, p *ViewPage, what string, err error) {
	if err == nil {
		return
	}
	u.Log.ErrorContext(c, "ui query failed", "section", what, "err", err)
	p.Degraded = append(p.Degraded, what)
}

// isHX reports whether the request came from htmx and wants a fragment.
func isHX(c *gin.Context) bool { return c.GetHeader("HX-Request") == "true" }

// show renders the full page, or only the named fragment for htmx requests.
func (u *UI) show(c *gin.Context, page, fragment string, p *ViewPage) {
	if fragment != "" && isHX(c) {
		p.Fragment = true
		c.HTML(http.StatusOK, partialPrefix+fragment, p)
		return
	}
	c.HTML(http.StatusOK, page, p)
}

// fail renders a styled error. htmx requests get an out-of-band toast only, so
// the main swap is skipped and the current content stays in place.
// NotFound renders the styled 404 page (a toast for htmx requests).
func (u *UI) NotFound(c *gin.Context) {
	u.fail(c, http.StatusNotFound, "There is no page at this address. It may have been deleted, or the link is mistyped.")
}

func (u *UI) fail(c *gin.Context, status int, msg string) {
	if isHX(c) {
		c.HTML(status, partialPrefix+"toast_oob", Flash{Kind: "error", Msg: msg})
		return
	}
	p := u.page(c, http.StatusText(status), "", errorView{Status: status, Message: msg})
	c.HTML(status, "error", p)
}

type errorView struct {
	Status  int
	Message string
}

// failErr maps common errors to a status and logs unexpected ones.
func (u *UI) failErr(c *gin.Context, what string, err error) {
	switch {
	case errors.Is(err, ops.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		u.fail(c, http.StatusNotFound, what+" not found. It may have been deleted.")
	default:
		u.Log.ErrorContext(c, "ui request failed", "what", what, "err", err)
		u.fail(c, http.StatusInternalServerError, "Something went wrong loading "+what+". Try again in a moment.")
	}
}

// done finishes a successful mutation: htmx requests get a toast (plus any
// fragment the caller already wrote), plain form posts get a 303 redirect.
func (u *UI) redirect(c *gin.Context, target, flash string, n int) {
	q := url.Values{}
	if flash != "" {
		q.Set("flash", flash)
	}
	if n >= 0 && flash != "" {
		q.Set("n", strconv.Itoa(n))
	}
	if len(q) > 0 {
		sep := "?"
		if u, err := url.Parse(target); err == nil && u.RawQuery != "" {
			sep = "&"
		}
		target += sep + q.Encode()
	}
	if isHX(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusSeeOther, target)
}

// toastFor renders a fragment plus an out-of-band toast for htmx row actions.
type rowResult struct {
	Row   any
	Toast Flash
}

func pageParam(c *gin.Context) int {
	p, err := strconv.Atoi(c.Query("page"))
	if err != nil || p < 1 {
		return 1
	}
	return p
}

// pager describes offset pagination for templates.
type pager struct {
	Page    int
	HasPrev bool
	HasNext bool
	PrevURL string
	NextURL string
}

func newPager(base url.Values, path string, page int, hasNext bool) pager {
	link := func(p int) string {
		q := url.Values{}
		for k, v := range base {
			if len(v) > 0 && v[0] != "" {
				q.Set(k, v[0])
			}
		}
		if p > 1 {
			q.Set("page", strconv.Itoa(p))
		}
		if len(q) == 0 {
			return path
		}
		return path + "?" + q.Encode()
	}
	return pager{Page: page, HasPrev: page > 1, HasNext: hasNext, PrevURL: link(page - 1), NextURL: link(page + 1)}
}

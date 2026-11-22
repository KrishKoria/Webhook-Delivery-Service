package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/delivery"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/ops"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const subsPerPage = 50

// subCtx is shared by every page scoped to one subscription (header + tabs).
type subCtx struct {
	Sub database.Subscription
	Tab string
}

// loadSub fetches the :id subscription or renders a 404 and returns false.
func (u *UI) loadSub(c *gin.Context) (database.Subscription, bool) {
	sub, err := u.Q.UIGetSubscription(c, c.Param("id"))
	if err != nil {
		u.failErr(c, "Subscription", err)
		return sub, false
	}
	return sub, true
}

type subsView struct {
	Subs  []database.Subscription
	Pager pager
}

// ListSubscriptions handles GET /ui/subscriptions.
func (u *UI) ListSubscriptions(c *gin.Context) {
	page := pageParam(c)
	subs, err := u.Q.UIListSubscriptions(c, database.UIListSubscriptionsParams{
		Lim: subsPerPage + 1, Off: int64((page - 1) * subsPerPage),
	})
	if err != nil {
		u.failErr(c, "subscriptions", err)
		return
	}
	hasNext := len(subs) > subsPerPage
	if hasNext {
		subs = subs[:subsPerPage]
	}
	p := u.page(c, "Subscriptions", "subscriptions", subsView{
		Subs: subs, Pager: newPager(nil, "/ui/subscriptions", page, hasNext),
	})
	c.HTML(http.StatusOK, "subscriptions", p)
}

// SubscriptionHome handles GET /ui/subscriptions/:id; activity is the landing tab.
func (u *UI) SubscriptionHome(c *gin.Context) {
	c.Redirect(http.StatusFound, "/ui/subscriptions/"+url.PathEscape(c.Param("id"))+"/logs")
}

// subForm is the create/edit form state, echoed back on validation errors.
type subForm struct {
	subCtx
	Editing     bool
	Action      string
	TargetURL   string
	EventTypes  string
	HasSecret   bool
	ClearSecret bool
	Errors      map[string]string
}

func (f *subForm) bind(c *gin.Context, allowPrivate bool) (secret string) {
	f.TargetURL = strings.TrimSpace(c.PostForm("target_url"))
	f.EventTypes = normalizeEventTypes(c.PostForm("event_types"))
	f.ClearSecret = c.PostForm("clear_secret") == "on"
	secret = c.PostForm("secret")
	f.Errors = map[string]string{}
	if msg := uiCheckTargetURL(f.TargetURL, allowPrivate); msg != "" {
		f.Errors["target_url"] = msg
	}
	if strings.TrimSpace(secret) != secret {
		f.Errors["secret"] = "Remove leading or trailing spaces from the secret."
	}
	if f.ClearSecret && secret != "" {
		f.Errors["secret"] = "Either enter a new secret or remove it, not both."
	}
	return secret
}

// uiCheckTargetURL returns a user-facing message when raw is not a usable
// target: malformed, or (unless allowPrivate) resolving to a private address.
func uiCheckTargetURL(raw string, allowPrivate bool) string {
	if raw == "" {
		return "Enter the URL that should receive webhooks."
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "Use a full http:// or https:// URL, for example https://example.com/hooks."
	}
	if !allowPrivate {
		if err := delivery.ValidateTargetURL(raw); err != nil {
			return "This address points to a private or internal network. Use a public URL."
		}
	}
	return ""
}

// showForm renders a form page, or just the form fragment for htmx (422 swaps it in place).
func (u *UI) showForm(c *gin.Context, status int, page, fragment string, p *ViewPage) {
	if isHX(c) {
		c.HTML(status, partialPrefix+fragment, p)
		return
	}
	c.HTML(status, page, p)
}

// NewSubscription handles GET /ui/subscriptions/new.
func (u *UI) NewSubscription(c *gin.Context) {
	f := subForm{Action: "/ui/subscriptions"}
	c.HTML(http.StatusOK, "subscription_form", u.page(c, "New subscription", "subscriptions", &f))
}

// CreateSubscription handles POST /ui/subscriptions.
func (u *UI) CreateSubscription(c *gin.Context) {
	f := subForm{Action: "/ui/subscriptions"}
	secret := f.bind(c, u.AllowPrivateTargets)
	if len(f.Errors) > 0 {
		u.showForm(c, http.StatusUnprocessableEntity, "subscription_form", "sub_form",
			u.page(c, "New subscription", "subscriptions", &f))
		return
	}
	err := u.Q.UICreateSubscription(c, database.UICreateSubscriptionParams{
		ID:         uuid.NewString(),
		TargetUrl:  f.TargetURL,
		Secret:     sql.NullString{String: secret, Valid: secret != ""},
		EventTypes: sql.NullString{String: f.EventTypes, Valid: f.EventTypes != ""},
	})
	if err != nil {
		u.failErr(c, "the new subscription", err)
		return
	}
	u.redirect(c, "/ui/subscriptions", "sub_created", -1)
}

// EditSubscription handles GET /ui/subscriptions/:id/edit.
func (u *UI) EditSubscription(c *gin.Context) {
	sub, ok := u.loadSub(c)
	if !ok {
		return
	}
	f := subForm{
		subCtx: subCtx{Sub: sub, Tab: "settings"}, Editing: true,
		Action:    "/ui/subscriptions/" + sub.ID + "/edit",
		TargetURL: sub.TargetUrl, EventTypes: sub.EventTypes.String, HasSecret: sub.Secret.Valid && sub.Secret.String != "",
	}
	c.HTML(http.StatusOK, "subscription_form", u.page(c, "Settings", "subscriptions", &f))
}

// UpdateSubscription handles POST /ui/subscriptions/:id/edit.
// A blank secret keeps the current one; "Remove secret" clears it.
func (u *UI) UpdateSubscription(c *gin.Context) {
	sub, ok := u.loadSub(c)
	if !ok {
		return
	}
	f := subForm{
		subCtx: subCtx{Sub: sub, Tab: "settings"}, Editing: true,
		Action: "/ui/subscriptions/" + sub.ID + "/edit", HasSecret: sub.Secret.Valid && sub.Secret.String != "",
	}
	secret := f.bind(c, u.AllowPrivateTargets)
	if len(f.Errors) > 0 {
		u.showForm(c, http.StatusUnprocessableEntity, "subscription_form", "sub_form", u.page(c, "Settings", "subscriptions", &f))
		return
	}
	in := ops.UpdateSubscriptionInput{ID: sub.ID, TargetURL: f.TargetURL, EventTypes: f.EventTypes, ClearSecret: f.ClearSecret}
	if secret != "" {
		in.Secret = &secret
	}
	if err := u.Ops.UpdateSubscription(c, in); err != nil {
		u.failErr(c, "Subscription", err)
		return
	}
	u.redirect(c, "/ui/subscriptions", "sub_updated", -1)
}

// DeleteSubscription handles POST /ui/subscriptions/:id/delete.
func (u *UI) DeleteSubscription(c *gin.Context) {
	if err := u.Ops.DeleteSubscription(c, c.Param("id")); err != nil {
		u.failErr(c, "Subscription", err)
		return
	}
	if isHX(c) && c.PostForm("return") == "list" {
		// The list removes the row itself (hx-swap="delete"); just confirm it.
		c.HTML(http.StatusOK, partialPrefix+"toast_oob", flashMessages["sub_deleted"])
		return
	}
	u.redirect(c, "/ui/subscriptions", "sub_deleted", -1)
}

// PauseSubscription handles POST /ui/subscriptions/:id/pause.
func (u *UI) PauseSubscription(c *gin.Context) { u.setPaused(c, true) }

// ResumeSubscription handles POST /ui/subscriptions/:id/resume.
func (u *UI) ResumeSubscription(c *gin.Context) { u.setPaused(c, false) }

func (u *UI) setPaused(c *gin.Context, paused bool) {
	id := c.Param("id")
	if err := u.Ops.SetPaused(c, id, paused); err != nil {
		u.failErr(c, "Subscription", err)
		return
	}
	flash := map[bool]string{true: "sub_paused", false: "sub_resumed"}[paused]
	ret := c.PostForm("return")
	if isHX(c) && ret == "list" {
		sub, err := u.Q.UIGetSubscription(c, id)
		if err != nil {
			u.failErr(c, "Subscription", err)
			return
		}
		c.HTML(http.StatusOK, partialPrefix+"sub_item_result", rowResult{Row: sub, Toast: flashMessages[flash]})
		return
	}
	target := "/ui/subscriptions"
	if tab := subTabPath(ret); tab != "" {
		target = "/ui/subscriptions/" + url.PathEscape(id) + "/" + tab
	}
	u.redirect(c, target, flash, -1)
}

// subTabPath whitelists the per-subscription pages a form may return to.
func subTabPath(ret string) string {
	switch ret {
	case "logs", "analytics", "scheduled", "test", "edit":
		return ret
	}
	return ""
}

type testForm struct {
	subCtx
	EventType string
	Payload   string
	Errors    map[string]string
}

const uiTestPayload = "{\n  \"event\": \"test\",\n  \"source\": \"admin_ui\"\n}"

// TestForm handles GET /ui/subscriptions/:id/test.
func (u *UI) TestForm(c *gin.Context) {
	sub, ok := u.loadSub(c)
	if !ok {
		return
	}
	f := testForm{subCtx: subCtx{Sub: sub, Tab: "test"}, EventType: firstEventType(sub), Payload: uiTestPayload}
	c.HTML(http.StatusOK, "send_test", u.page(c, "Send test", "subscriptions", &f))
}

func firstEventType(sub database.Subscription) string {
	first, _, _ := strings.Cut(sub.EventTypes.String, ",")
	return strings.TrimSpace(first)
}

// SendTest handles POST /ui/subscriptions/:id/test.
func (u *UI) SendTest(c *gin.Context) {
	sub, ok := u.loadSub(c)
	if !ok {
		return
	}
	f := testForm{
		subCtx:    subCtx{Sub: sub, Tab: "test"},
		EventType: strings.TrimSpace(c.PostForm("event_type")),
		Payload:   strings.TrimSpace(c.PostForm("payload")),
		Errors:    map[string]string{},
	}
	if f.Payload == "" {
		f.Errors["payload"] = "Enter a JSON payload to send."
	} else if !json.Valid([]byte(f.Payload)) {
		f.Errors["payload"] = "The payload is not valid JSON. Check for trailing commas and unquoted keys."
	}
	if len(f.EventType) > 200 {
		f.Errors["event_type"] = "Keep the event type under 200 characters."
	}
	if len(f.Errors) > 0 {
		u.showForm(c, http.StatusUnprocessableEntity, "send_test", "test_form", u.page(c, "Send test", "subscriptions", &f))
		return
	}
	if _, _, err := u.Ops.Enqueue(c, ops.EnqueueInput{SubscriptionID: sub.ID, Payload: f.Payload, EventType: f.EventType}); err != nil {
		u.failErr(c, "the test event", err)
		return
	}
	u.redirect(c, "/ui/subscriptions/"+url.PathEscape(sub.ID)+"/logs", "test_sent", -1)
}

const logsPerPage = 50

var (
	logOutcomes = []string{"success", "failed_attempt"}
	taskStates  = []string{"pending", "in_flight", "delivered", "failed"}
)

type logsView struct {
	subCtx
	Rows       []database.UIListDeliveryLogsRow
	Outcome    string
	Status     string
	Outcomes   []string
	Statuses   []string
	Older      bool // viewing a page behind the cursor
	OlderURL   string
	LatestURL  string
	PollURL    string
	IsFiltered bool
}

func oneOf(v string, allowed []string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return ""
}

// Logs handles GET /ui/subscriptions/:id/logs with outcome/status filters and a
// keyset cursor (?before=<timestamp>~<log id>). htmx polls get only the table.
func (u *UI) Logs(c *gin.Context) {
	sub, ok := u.loadSub(c)
	if !ok {
		return
	}
	v := logsView{
		subCtx:   subCtx{Sub: sub, Tab: "logs"},
		Outcome:  oneOf(c.Query("outcome"), logOutcomes),
		Status:   oneOf(c.Query("status"), taskStates),
		Outcomes: logOutcomes, Statuses: taskStates,
	}
	v.IsFiltered = v.Outcome != "" || v.Status != ""
	params := database.UIListDeliveryLogsParams{
		SubscriptionID: sub.ID, Outcome: v.Outcome, TaskStatus: v.Status, Lim: logsPerPage + 1,
	}
	if ts, id, ok := parseLogCursor(c.Query("before")); ok {
		params.BeforeTs, params.BeforeID, v.Older = ts, id, true
	}
	rows, err := u.Q.UIListDeliveryLogs(c, params)
	if err != nil {
		u.failErr(c, "delivery logs", err)
		return
	}
	filters := url.Values{"outcome": {v.Outcome}, "status": {v.Status}}
	base := "/ui/subscriptions/" + url.PathEscape(sub.ID) + "/logs"
	v.LatestURL = withQuery(base, filters)
	if len(rows) > logsPerPage {
		rows = rows[:logsPerPage]
		last := rows[len(rows)-1]
		older := url.Values{"outcome": {v.Outcome}, "status": {v.Status},
			"before": {last.Timestamp.UTC().Format(time.DateTime) + "~" + last.ID}}
		v.OlderURL = withQuery(base, older)
	}
	v.Rows = rows
	v.PollURL = withQuery(base, url.Values{"outcome": {v.Outcome}, "status": {v.Status}, "before": {c.Query("before")}})

	p := u.page(c, "Activity", "subscriptions", v)
	p.Live = !v.Older
	u.show(c, "logs", "logs_table", p)
}

func parseLogCursor(s string) (ts, id string, ok bool) {
	ts, id, found := strings.Cut(s, "~")
	if !found || id == "" {
		return "", "", false
	}
	if _, err := time.Parse(time.DateTime, ts); err != nil {
		return "", "", false
	}
	return ts, id, true
}

// withQuery appends the non-empty values to path.
func withQuery(path string, vals url.Values) string {
	q := url.Values{}
	for k, v := range vals {
		if len(v) > 0 && v[0] != "" {
			q.Set(k, v[0])
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// Redeliver handles POST /ui/tasks/:task_id/redeliver.
func (u *UI) Redeliver(c *gin.Context) {
	newID, err := u.Ops.Redeliver(c, c.Param("task_id"))
	if err != nil {
		u.failErr(c, "Delivery task", err)
		return
	}
	if isHX(c) {
		c.HTML(http.StatusOK, partialPrefix+"toast_oob", flashMessages["redelivered"])
		return
	}
	target := "/ui/subscriptions"
	if t, err := u.Q.UIGetDeliveryTask(c, newID); err == nil {
		target = "/ui/subscriptions/" + url.PathEscape(t.SubscriptionID) + "/logs"
	}
	u.redirect(c, target, "redelivered", -1)
}

type window struct {
	Hours  int64
	Label  string
	URL    string
	Active bool
}

type analyticsView struct {
	subCtx
	Hours       int64
	WindowLabel string
	Windows     []window
	Attempts    database.UIAttemptStatsForSubscriptionRow
	Total       int64
	SuccessRate float64
	Tasks       database.UITaskStatusCountsForSubscriptionRow
	TaskTotal   int64
	DLQ         database.UIDLQCountsForSubscriptionRow
	Failures    []database.UIRecentFailuresForSubscriptionRow
}

// Analytics handles GET /ui/subscriptions/:id/analytics.
func (u *UI) Analytics(c *gin.Context) {
	sub, ok := u.loadSub(c)
	if !ok {
		return
	}
	p := u.page(c, "Analytics", "subscriptions", nil)
	hours, _ := strconv.ParseInt(c.Query("hours"), 10, 64)
	v := analyticsView{subCtx: subCtx{Sub: sub, Tab: "analytics"}, Hours: 24}
	base := "/ui/subscriptions/" + url.PathEscape(sub.ID) + "/analytics"
	for _, w := range []window{{Hours: 24, Label: "24 hours"}, {Hours: 168, Label: "7 days"}, {Hours: 720, Label: "30 days"}} {
		if w.Hours == hours {
			v.Hours = hours
		}
		w.URL = base + "?hours=" + strconv.FormatInt(w.Hours, 10)
		v.Windows = append(v.Windows, w)
	}
	for i := range v.Windows {
		if v.Windows[i].Active = v.Windows[i].Hours == v.Hours; v.Windows[i].Active {
			v.WindowLabel = v.Windows[i].Label
		}
	}

	var err error
	v.Attempts, err = u.Q.UIAttemptStatsForSubscription(c, database.UIAttemptStatsForSubscriptionParams{SubscriptionID: sub.ID, Hours: v.Hours})
	u.degrade(c, p, "delivery attempts", err)
	v.Total = v.Attempts.Successful + v.Attempts.Failed
	if v.Total > 0 {
		v.SuccessRate = float64(v.Attempts.Successful) / float64(v.Total) * 100
	}
	v.Tasks, err = u.Q.UITaskStatusCountsForSubscription(c, sub.ID)
	u.degrade(c, p, "task counts", err)
	v.TaskTotal = v.Tasks.Pending + v.Tasks.InFlight + v.Tasks.Delivered + v.Tasks.Failed
	v.DLQ, err = u.Q.UIDLQCountsForSubscription(c, sub.ID)
	u.degrade(c, p, "dead letter counts", err)
	v.Failures, err = u.Q.UIRecentFailuresForSubscription(c, database.UIRecentFailuresForSubscriptionParams{SubscriptionID: sub.ID, Lim: 15})
	u.degrade(c, p, "recent failures", err)

	p.Data = v
	c.HTML(http.StatusOK, "analytics", p)
}

// SubscriptionDLQ handles GET /ui/subscriptions/:id/dlq; the DLQ page filters by subscription.
func (u *UI) SubscriptionDLQ(c *gin.Context) {
	c.Redirect(http.StatusFound, "/ui/dlq?"+url.Values{"subscription": {c.Param("id")}}.Encode())
}

// legacyScheduledList keeps old bookmarks to /scheduled/list working.
func (u *UI) legacyScheduledList(c *gin.Context) {
	c.Redirect(http.StatusMovedPermanently, "/ui/subscriptions/"+url.PathEscape(c.Param("id"))+"/scheduled")
}

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
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/ops"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const scheduledPerPage = 50

var recurrences = []string{"none", "daily", "weekly", "monthly"}

type scheduledView struct {
	subCtx
	Global bool
	Rows   []database.ScheduledWebhook
	Counts database.UIScheduledCountsRow
	Pager  pager
	Return string
}

// ListScheduled handles GET /ui/scheduled (all subscriptions, paginated).
func (u *UI) ListScheduled(c *gin.Context) {
	p := u.page(c, "Scheduled", "scheduled", nil)
	v, ok := u.loadScheduled(c, p, "", "/ui/scheduled")
	if !ok {
		return
	}
	v.Global = true
	v.Counts, _ = u.Q.UIScheduledCounts(c)
	p.Data = v
	c.HTML(http.StatusOK, "scheduled", p)
}

// SubscriptionScheduled handles GET /ui/subscriptions/:id/scheduled.
func (u *UI) SubscriptionScheduled(c *gin.Context) {
	sub, ok := u.loadSub(c)
	if !ok {
		return
	}
	p := u.page(c, "Scheduled", "subscriptions", nil)
	v, ok := u.loadScheduled(c, p, sub.ID, "/ui/subscriptions/"+url.PathEscape(sub.ID)+"/scheduled")
	if !ok {
		return
	}
	v.subCtx = subCtx{Sub: sub, Tab: "scheduled"}
	p.Data = v
	c.HTML(http.StatusOK, "scheduled", p)
}

func (u *UI) loadScheduled(c *gin.Context, p *Page, subID, path string) (scheduledView, bool) {
	page := pageParam(c)
	rows, err := u.Q.UIListScheduled(c, database.UIListScheduledParams{
		SubscriptionID: subID, Lim: scheduledPerPage + 1, Off: int64((page - 1) * scheduledPerPage),
	})
	if err != nil {
		u.failErr(c, "scheduled webhooks", err)
		return scheduledView{}, false
	}
	hasNext := len(rows) > scheduledPerPage
	if hasNext {
		rows = rows[:scheduledPerPage]
	}
	ret := path
	if page > 1 {
		ret += "?page=" + strconv.Itoa(page)
	}
	return scheduledView{Rows: rows, Pager: newPager(nil, path, page, hasNext), Return: ret}, true
}

// scheduledReturn accepts only the global or a per-subscription scheduled list.
func scheduledReturn(c *gin.Context) string {
	r := c.PostForm("return")
	parsed, err := url.Parse(r)
	if err != nil || r == "" || parsed.Scheme != "" || parsed.Host != "" {
		return "/ui/scheduled"
	}
	p := parsed.Path
	if p == "/ui/scheduled" {
		return parsed.RequestURI()
	}
	if rest, ok := strings.CutPrefix(p, "/ui/subscriptions/"); ok {
		if id, tail, _ := strings.Cut(rest, "/"); id != "" && tail == "scheduled" && !strings.ContainsAny(id, ".%") {
			return parsed.RequestURI()
		}
	}
	return "/ui/scheduled"
}

// CancelScheduled handles POST /ui/scheduled/:id/cancel. Only pending rows change.
func (u *UI) CancelScheduled(c *gin.Context) {
	id := c.Param("id")
	n, err := u.Q.UICancelScheduled(c, id)
	if err != nil {
		u.failErr(c, "Scheduled webhook", err)
		return
	}
	status, flash := http.StatusOK, "sched_cancelled"
	if n == 0 {
		if _, err := u.Q.UIGetScheduled(c, id); err != nil {
			u.failErr(c, "Scheduled webhook", err)
			return
		}
		status, flash = http.StatusConflict, "sched_conflict"
	}
	if isHX(c) {
		row, err := u.Q.UIGetScheduled(c, id)
		if err != nil {
			u.failErr(c, "Scheduled webhook", err)
			return
		}
		c.HTML(status, partialPrefix+"sched_row_result", rowResult{
			Row:   schedRow{Row: row, Global: c.PostForm("global") == "1", Return: scheduledReturn(c)},
			Toast: flashMessages[flash],
		})
		return
	}
	u.redirect(c, scheduledReturn(c), flash, -1)
}

// DeleteScheduled handles POST /ui/scheduled/:id/delete.
func (u *UI) DeleteScheduled(c *gin.Context) {
	n, err := u.Q.UIDeleteScheduled(c, c.Param("id"))
	if err == nil && n == 0 {
		err = ops.ErrNotFound
	}
	if err != nil {
		u.failErr(c, "Scheduled webhook", err)
		return
	}
	if isHX(c) {
		c.HTML(http.StatusOK, partialPrefix+"toast_oob", flashMessages["sched_deleted"])
		return
	}
	u.redirect(c, scheduledReturn(c), "sched_deleted", -1)
}

// schedRow is one table row; Global adds the subscription column.
type schedRow struct {
	Row    database.ScheduledWebhook
	Global bool
	Return string
}

type scheduleForm struct {
	subCtx
	Payload     string
	Local       string // datetime-local value as typed
	TZOffset    string // minutes, as reported by Date.getTimezoneOffset()
	Recurrence  string
	Recurrences []string
	MinLocal    string
	Errors      map[string]string
}

// NewScheduled handles GET /ui/subscriptions/:id/scheduled/new.
func (u *UI) NewScheduled(c *gin.Context) {
	sub, ok := u.loadSub(c)
	if !ok {
		return
	}
	f := scheduleForm{
		subCtx:  subCtx{Sub: sub, Tab: "scheduled"},
		Payload: defaultTestPayload, Recurrence: "none", Recurrences: recurrences, TZOffset: "0",
		Local:    time.Now().UTC().Add(time.Hour).Truncate(time.Minute).Format("2006-01-02T15:04"),
		MinLocal: time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02T15:04"),
	}
	c.HTML(http.StatusOK, "schedule_form", u.page(c, "Schedule a webhook", "subscriptions", &f))
}

// parseLocalSchedule converts a datetime-local value plus the browser's
// getTimezoneOffset() minutes into UTC. Without JS the offset is 0 (UTC).
func parseLocalSchedule(local, offset string) (time.Time, bool) {
	var t time.Time
	var err error
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05"} {
		if t, err = time.ParseInLocation(layout, local, time.UTC); err == nil {
			break
		}
	}
	if err != nil {
		return time.Time{}, false
	}
	mins := 0
	if offset != "" {
		if mins, err = strconv.Atoi(offset); err != nil || mins < -14*60 || mins > 14*60 {
			return time.Time{}, false
		}
	}
	return t.Add(time.Duration(mins) * time.Minute), true
}

// CreateScheduled handles POST /ui/subscriptions/:id/scheduled.
func (u *UI) CreateScheduled(c *gin.Context) {
	sub, ok := u.loadSub(c)
	if !ok {
		return
	}
	f := scheduleForm{
		subCtx:      subCtx{Sub: sub, Tab: "scheduled"},
		Payload:     strings.TrimSpace(c.PostForm("payload")),
		Local:       c.PostForm("scheduled_for_local"),
		TZOffset:    c.PostForm("tz_offset"),
		Recurrence:  oneOf(c.PostForm("recurrence"), recurrences),
		Recurrences: recurrences,
		Errors:      map[string]string{},
	}
	if f.Recurrence == "" {
		f.Errors["recurrence"] = "Pick one of the listed repeat options."
	}
	if f.Payload == "" {
		f.Errors["payload"] = "Enter a JSON payload to send."
	} else if !json.Valid([]byte(f.Payload)) {
		f.Errors["payload"] = "The payload is not valid JSON. Check for trailing commas and unquoted keys."
	}
	at, ok := parseLocalSchedule(f.Local, f.TZOffset)
	switch {
	case f.Local == "":
		f.Errors["scheduled_for_local"] = "Choose when the webhook should fire."
	case !ok:
		f.Errors["scheduled_for_local"] = "Enter a date and time like 2026-12-01 09:30."
	case !at.After(time.Now()):
		f.Errors["scheduled_for_local"] = "Pick a time in the future."
	}
	if len(f.Errors) > 0 {
		u.showForm(c, http.StatusUnprocessableEntity, "schedule_form", "schedule_form_body",
			u.page(c, "Schedule a webhook", "subscriptions", &f))
		return
	}
	rec := sql.NullString{String: f.Recurrence, Valid: f.Recurrence != "none"}
	err := u.Q.UICreateScheduled(c, database.UICreateScheduledParams{
		ID: uuid.NewString(), SubscriptionID: sub.ID, Payload: f.Payload,
		ScheduledFor: at.UTC().Format(time.RFC3339), Recurrence: rec,
	})
	if err != nil {
		u.failErr(c, "the schedule", err)
		return
	}
	u.redirect(c, "/ui/subscriptions/"+url.PathEscape(sub.ID)+"/scheduled", "sched_created", -1)
}

package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/ops"
	"github.com/gin-gonic/gin"
)

const (
	dlqPerPage  = 50
	dlqMaxBulk  = 100
	dlqUINote   = "Retried from admin UI"
	dlqListPath = "/ui/dlq"
)

var dlqStatuses = []string{"dead", "retried"}

type dlqView struct {
	Rows         []database.DeadLetterTask
	Counts       database.UIDLQCountsRow
	Status       string
	Statuses     []string
	Subscription string
	Pager        pager
	Return       string // where row and bulk actions send the user back to
}

// ListDLQ handles GET /ui/dlq?status=&subscription=&page=.
func (u *UI) ListDLQ(c *gin.Context) {
	page := pageParam(c)
	p := u.page(c, "Dead letter queue", "dlq", nil)
	v := dlqView{
		Status:       oneOf(c.Query("status"), dlqStatuses),
		Statuses:     dlqStatuses,
		Subscription: c.Query("subscription"),
	}
	rows, err := u.Q.UIListDLQ(c.Request.Context(), database.UIListDLQParams{
		Status: v.Status, SubscriptionID: v.Subscription,
		Lim: dlqPerPage + 1, Off: int64((page - 1) * dlqPerPage),
	})
	if err != nil {
		u.failErr(c, "the dead letter queue", err)
		return
	}
	hasNext := len(rows) > dlqPerPage
	if hasNext {
		rows = rows[:dlqPerPage]
	}
	v.Rows = rows
	v.Counts, err = u.Q.UIDLQCounts(c.Request.Context())
	u.degrade(c, p, "dead letter totals", err)
	filters := url.Values{"status": {v.Status}, "subscription": {v.Subscription}}
	v.Pager = newPager(filters, dlqListPath, page, hasNext)
	if page > 1 {
		filters.Set("page", strconv.Itoa(page))
	}
	v.Return = withQuery(dlqListPath, filters)
	p.Data = v
	c.HTML(http.StatusOK, "dlq", p)
}

// dlqRow is one table row plus the list URL its actions return to.
type dlqRow struct {
	Entry  database.DeadLetterTask
	Return string
}

type dlqDetailView struct {
	Entry database.DeadLetterTask
}

// DLQDetail handles GET /ui/dlq/:dlq_id.
func (u *UI) DLQDetail(c *gin.Context) {
	e, err := u.Q.UIGetDLQEntry(c.Request.Context(), c.Param("dlq_id"))
	if err != nil {
		u.failErr(c, "Dead letter entry", err)
		return
	}
	c.HTML(http.StatusOK, "dlq_detail", u.page(c, "Dead letter entry", "dlq", dlqDetailView{Entry: e}))
}

// dlqReturn accepts only a /ui/dlq URL (list with filters, or an entry) as a
// post-action target, so the form field cannot redirect off-site.
func dlqReturn(c *gin.Context) string {
	r := c.PostForm("return")
	parsed, err := url.Parse(r)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || r == "" ||
		(parsed.Path != dlqListPath && !isDLQEntryPath(parsed.Path)) {
		return dlqListPath
	}
	return parsed.RequestURI()
}

func isDLQEntryPath(p string) bool {
	rest, ok := strings.CutPrefix(p, dlqListPath+"/")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// RetryDLQ handles POST /ui/dlq/:dlq_id/retry. A second retry of the same entry
// is a 409 that tells the user it was already retried.
func (u *UI) RetryDLQ(c *gin.Context) {
	id := c.Param("dlq_id")
	_, err := u.Ops.RetryDLQ(c.Request.Context(), id, dlqUINote)
	status, flash := http.StatusOK, "dlq_retried"
	switch {
	case errors.Is(err, ops.ErrConflict):
		status, flash = http.StatusConflict, "dlq_conflict"
	case err != nil:
		u.failErr(c, "Dead letter entry", err)
		return
	}
	if isHX(c) {
		e, getErr := u.Q.UIGetDLQEntry(c.Request.Context(), id)
		if getErr != nil {
			u.failErr(c, "Dead letter entry", getErr)
			return
		}
		c.HTML(status, partialPrefix+"dlq_row_result", rowResult{
			Row: dlqRow{Entry: e, Return: dlqReturn(c)}, Toast: flashMessages[flash],
		})
		return
	}
	u.redirect(c, dlqReturn(c), flash, -1)
}

// DeleteDLQ handles POST /ui/dlq/:dlq_id/delete.
func (u *UI) DeleteDLQ(c *gin.Context) {
	n, err := u.Q.UIDeleteDLQEntries(c.Request.Context(), []string{c.Param("dlq_id")})
	if err == nil && n == 0 {
		err = ops.ErrNotFound
	}
	if err != nil {
		u.failErr(c, "Dead letter entry", err)
		return
	}
	if isHX(c) {
		// The row removes itself (hx-swap="delete"); only the toast is sent.
		c.HTML(http.StatusOK, partialPrefix+"toast_oob", flashMessages["dlq_deleted"])
		return
	}
	ret := dlqReturn(c)
	if isDLQEntryPath(ret) {
		ret = dlqListPath // the entry is gone
	}
	u.redirect(c, ret, "dlq_deleted", -1)
}

func selectedIDs(c *gin.Context) []string {
	ids := c.PostFormArray("ids")
	if len(ids) > dlqMaxBulk {
		ids = ids[:dlqMaxBulk]
	}
	return ids
}

// BulkRetryDLQ handles POST /ui/dlq/bulk-retry (plain form post).
func (u *UI) BulkRetryDLQ(c *gin.Context) {
	ids := selectedIDs(c)
	if len(ids) == 0 {
		u.redirect(c, dlqReturn(c), "dlq_none", -1)
		return
	}
	retried := 0
	for _, r := range u.Ops.RetryDLQBulk(c.Request.Context(), ids, dlqUINote) {
		if r.Error == "" {
			retried++
		} else if r.Error == "internal_error" {
			u.Log.ErrorContext(c.Request.Context(), "bulk dlq retry failed", "dlq_id", r.DLQID)
		}
	}
	flash := "dlq_bulk_retried"
	if retried < len(ids) {
		flash = "dlq_bulk_partial"
	}
	u.redirect(c, dlqReturn(c), flash, retried)
}

// BulkDeleteDLQ handles POST /ui/dlq/bulk-delete (plain form post).
func (u *UI) BulkDeleteDLQ(c *gin.Context) {
	ids := selectedIDs(c)
	if len(ids) == 0 {
		u.redirect(c, dlqReturn(c), "dlq_none", -1)
		return
	}
	n, err := u.Q.UIDeleteDLQEntries(c.Request.Context(), ids)
	if err != nil {
		u.failErr(c, "dead letter entries", err)
		return
	}
	u.redirect(c, dlqReturn(c), "dlq_bulk_deleted", int(n))
}

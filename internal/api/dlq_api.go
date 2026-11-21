package api

import (
	"net/http"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/ops"
	"github.com/gin-gonic/gin"
)

// maxDLQBatch bounds bulk retry and bulk delete requests.
const maxDLQBatch = 100

// DLQAPIHandler serves the dead-letter queue JSON API.
type DLQAPIHandler struct {
	Queries *database.Queries
	Ops     *ops.Ops
}

// RegisterDLQAPIRoutesGroup registers the DLQ JSON API on the admin API group.
func RegisterDLQAPIRoutesGroup(rg *gin.RouterGroup, h *DLQAPIHandler) {
	rg.GET("/api/dlq", h.ListDLQAPI)
	rg.DELETE("/api/dlq", h.BulkDeleteDLQAPI)
	rg.POST("/api/dlq/bulk-retry", h.BulkRetryDLQAPI)
	rg.GET("/api/dlq/:dlq_id", h.GetDLQTaskAPI)
	rg.DELETE("/api/dlq/:dlq_id", h.DeleteDLQTaskAPI)
	rg.POST("/api/dlq/:dlq_id/retry", h.RetryDLQTaskAPI)
	rg.GET("/api/subscriptions/:id/dlq", h.ListDLQAPI)
}

// DLQTaskResponse is the API view of a dead-letter entry. Status is dead or retried.
type DLQTaskResponse struct {
	ID             string  `json:"id"`
	OriginalTaskID string  `json:"original_task_id"`
	SubscriptionID string  `json:"subscription_id"`
	TargetURL      *string `json:"target_url"`
	EventType      *string `json:"event_type"`
	Status         string  `json:"status"`
	Reason         string  `json:"reason"`
	AttemptCount   int64   `json:"attempt_count"`
	FailedAt       string  `json:"failed_at"`
	LastAttemptAt  *string `json:"last_attempt_at"`
	// Payload and ErrorDetails are only included on the single-entry endpoint.
	Payload      *string `json:"payload,omitempty"`
	ErrorDetails *string `json:"error_details,omitempty"`
}

func toDLQResponse(t database.DeadLetterTask, detail bool) DLQTaskResponse {
	r := DLQTaskResponse{
		ID: t.ID, OriginalTaskID: t.OriginalTaskID, SubscriptionID: t.SubscriptionID,
		TargetURL: nullStr(t.TargetUrl), EventType: nullStr(t.EventType),
		Status: t.Status, Reason: t.Reason, AttemptCount: t.AttemptCount,
		FailedAt: ts(t.FailedAt), LastAttemptAt: nullTS(t.LastAttemptAt),
	}
	if detail {
		r.Payload = &t.Payload
		r.ErrorDetails = nullStr(t.ErrorDetails)
	}
	return r
}

// ListDLQAPI handles GET /api/dlq?status=&subscription_id=&limit=&cursor= and
// GET /api/subscriptions/:id/dlq, newest failure first.
func (h *DLQAPIHandler) ListDLQAPI(c *gin.Context) {
	limit, cur, ok := pageParams(c)
	if !ok {
		return
	}
	subID := queryString(c, "subscription_id")
	if id := c.Param("id"); id != "" {
		subID.String, subID.Valid = id, true
	}
	curTS, curID := cur.params()
	rows, err := h.Queries.APIListDLQ(c.Request.Context(), database.APIListDLQParams{
		SubscriptionID: subID,
		Status:         queryString(c, "status"),
		CursorTs:       curTS,
		CursorID:       curID,
		Lim:            int64(limit + 1),
	})
	if err != nil {
		respondInternal(c, "list dlq", err)
		return
	}
	RespondSuccess(c, http.StatusOK, pageOf(rows, limit,
		func(t database.DeadLetterTask) (time.Time, string) { return t.FailedAt, t.ID },
		func(t database.DeadLetterTask) DLQTaskResponse { return toDLQResponse(t, false) }))
}

// GetDLQTaskAPI handles GET /api/dlq/:dlq_id, including payload and error details.
func (h *DLQAPIHandler) GetDLQTaskAPI(c *gin.Context) {
	t, err := h.Queries.GetDeadLetterTask(c.Request.Context(), c.Param("dlq_id"))
	if err != nil {
		respondDBError(c, "DLQ entry", err)
		return
	}
	RespondSuccess(c, http.StatusOK, toDLQResponse(t, true))
}

// RetryDLQTaskAPI handles POST /api/dlq/:dlq_id/retry. It atomically marks a
// dead entry retried and enqueues a new task; a non-dead entry is 409.
func (h *DLQAPIHandler) RetryDLQTaskAPI(c *gin.Context) {
	id := c.Param("dlq_id")
	taskID, err := h.Ops.RetryDLQ(c.Request.Context(), id, "retried via API")
	if !respondOpsErr(c, "DLQ entry", "retry dlq", err) {
		return
	}
	RespondSuccess(c, http.StatusOK, gin.H{"dlq_id": id, "new_task_id": taskID})
}

// dlqIDsRequest is the body of bulk DLQ endpoints. "ids" is canonical;
// "dlq_ids" is accepted for older clients.
type dlqIDsRequest struct {
	IDs    []string `json:"ids"`
	DLQIDs []string `json:"dlq_ids"`
}

// bindIDs parses and validates a bulk request. It answers errors itself.
func bindIDs(c *gin.Context) ([]string, bool) {
	var req dlqIDsRequest
	if !bindJSON(c, &req, false) {
		return nil, false
	}
	ids := append(req.IDs, req.DLQIDs...)
	if len(ids) == 0 {
		RespondErrorWithField(c, http.StatusBadRequest, "ids must contain at least one ID", "ids")
		return nil, false
	}
	if len(ids) > maxDLQBatch {
		RespondErrorWithField(c, http.StatusBadRequest, "ids may contain at most 100 IDs", "ids")
		return nil, false
	}
	return ids, true
}

// BulkRetryDLQAPI handles POST /api/dlq/bulk-retry {"ids": [...]} (max 100).
// Each ID is retried in its own transaction; per-ID results report
// not_found, not_dead or internal_error.
func (h *DLQAPIHandler) BulkRetryDLQAPI(c *gin.Context) {
	ids, ok := bindIDs(c)
	if !ok {
		return
	}
	results := h.Ops.RetryDLQBulk(c.Request.Context(), ids, "retried via bulk API")
	retried := 0
	for _, r := range results {
		if r.Error == "" {
			retried++
		}
	}
	RespondSuccess(c, http.StatusOK, gin.H{
		"retried": retried,
		"failed":  len(results) - retried,
		"results": results,
	})
}

// DeleteDLQTaskAPI handles DELETE /api/dlq/:dlq_id.
func (h *DLQAPIHandler) DeleteDLQTaskAPI(c *gin.Context) {
	n, err := h.Queries.APIDeleteDLQTask(c.Request.Context(), c.Param("dlq_id"))
	if err != nil {
		respondInternal(c, "delete dlq entry", err)
		return
	}
	if n == 0 {
		RespondNotFound(c, "DLQ entry")
		return
	}
	c.Status(http.StatusNoContent)
}

// BulkDeleteDLQAPI handles DELETE /api/dlq {"ids": [...]} (max 100).
func (h *DLQAPIHandler) BulkDeleteDLQAPI(c *gin.Context) {
	ids, ok := bindIDs(c)
	if !ok {
		return
	}
	n, err := h.Queries.APIBulkDeleteDLQ(c.Request.Context(), ids)
	if err != nil {
		respondInternal(c, "bulk delete dlq", err)
		return
	}
	RespondSuccess(c, http.StatusOK, gin.H{"deleted": n, "requested": len(ids)})
}

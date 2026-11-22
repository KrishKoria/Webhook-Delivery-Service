package api

import (
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
)

type dashboardView struct {
	DatabaseOK   bool
	CacheEnabled bool
	CacheOK      bool

	Subs     database.UISubscriptionCountsRow
	Tasks    database.UITaskStatusCountsRow
	Attempts database.UIAttemptStatsRow
	DLQ      database.UIDLQCountsRow
	Sched    database.UIScheduledCountsRow
	Backlog  database.UIBacklogStatsRow

	QueueDepth  int64 // pending + in flight
	AttemptsAll int64
	SuccessRate float64
}

// Dashboard handles GET /ui/dashboard. htmx polls receive only the stats region.
func (u *UI) Dashboard(c *gin.Context) {
	p := u.page(c, "Overview", "dashboard", nil)
	p.Live = true
	v := dashboardView{DatabaseOK: true, CacheEnabled: u.Cache != nil}

	var err error
	if v.Subs, err = u.Q.UISubscriptionCounts(c.Request.Context()); err != nil {
		v.DatabaseOK = false
		u.degrade(c, p, "subscriptions", err)
	}
	if v.Tasks, err = u.Q.UITaskStatusCounts(c.Request.Context()); err != nil {
		u.degrade(c, p, "queue depth", err)
	}
	if v.Attempts, err = u.Q.UIAttemptStats(c.Request.Context(), 24); err != nil {
		u.degrade(c, p, "delivery outcomes", err)
	}
	if v.DLQ, err = u.Q.UIDLQCounts(c.Request.Context()); err != nil {
		u.degrade(c, p, "dead letter queue", err)
	}
	if v.Sched, err = u.Q.UIScheduledCounts(c.Request.Context()); err != nil {
		u.degrade(c, p, "scheduled webhooks", err)
	}
	if v.Backlog, err = u.Q.UIBacklogStats(c.Request.Context()); err != nil {
		u.degrade(c, p, "backlog", err)
	}
	if u.Cache != nil {
		v.CacheOK = u.Cache.Ping(c.Request.Context()) == nil
	}

	v.QueueDepth = v.Tasks.Pending + v.Tasks.InFlight
	v.AttemptsAll = v.Attempts.Successful + v.Attempts.Failed
	if v.AttemptsAll > 0 {
		v.SuccessRate = float64(v.Attempts.Successful) / float64(v.AttemptsAll) * 100
	}
	p.Data = v
	u.show(c, "dashboard", "dashboard_stats", p)
}

type retryBucket struct {
	Attempts int64
	Count    int64
	Pct      int64
}

type backlogView struct {
	Stats          database.UIBacklogStatsRow
	InFlightCount  int64
	OldestAge      time.Duration
	HasOldest      bool
	BySubscription []database.UIBacklogBySubscriptionRow
	Distribution   []retryBucket
	DistTotal      int64
	InFlight       []database.UIListTasksByStatusRow
	Retrying       []database.UIListTasksByStatusRow
}

// Backlog handles GET /ui/backlog. htmx polls receive only the body region.
func (u *UI) Backlog(c *gin.Context) {
	p := u.page(c, "Backlog", "backlog", nil)
	p.Live = true
	var v backlogView
	var err error

	if v.Stats, err = u.Q.UIBacklogStats(c.Request.Context()); err != nil {
		u.degrade(c, p, "backlog totals", err)
	}
	if t, ok := asTime(v.Stats.OldestCreatedAt); ok {
		v.OldestAge, v.HasOldest = time.Since(t).Round(time.Second), true
	}
	if v.InFlightCount, err = u.Q.UIInFlightCount(c.Request.Context()); err != nil {
		u.degrade(c, p, "in-flight count", err)
	}
	if v.BySubscription, err = u.Q.UIBacklogBySubscription(c.Request.Context(), 20); err != nil {
		u.degrade(c, p, "backlog by subscription", err)
	}
	dist, err := u.Q.UIRetryDistribution(c.Request.Context())
	u.degrade(c, p, "retry distribution", err)
	for _, d := range dist {
		v.DistTotal += d.TaskCount
	}
	for _, d := range dist {
		v.Distribution = append(v.Distribution, retryBucket{Attempts: d.AttemptCount, Count: d.TaskCount, Pct: pct(d.TaskCount, v.DistTotal)})
	}
	if v.InFlight, err = u.Q.UIListTasksByStatus(c.Request.Context(), database.UIListTasksByStatusParams{Status: "in_flight", Lim: 20}); err != nil {
		u.degrade(c, p, "in-flight tasks", err)
	}
	if v.Retrying, err = u.Q.UIListTasksByStatus(c.Request.Context(), database.UIListTasksByStatusParams{Status: "pending", RetryingOnly: 1, Lim: 20}); err != nil {
		u.degrade(c, p, "retrying tasks", err)
	}
	p.Data = v
	u.show(c, "backlog", "backlog_body", p)
}

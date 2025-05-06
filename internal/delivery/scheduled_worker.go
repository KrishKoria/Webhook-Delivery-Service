package delivery

import (
	"context"
	"log"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/google/uuid"
)

type ScheduledWorker struct {
    Queries *database.Queries
}

func NewScheduledWorker(queries *database.Queries) *ScheduledWorker {
    return &ScheduledWorker{Queries: queries}
}

func (w *ScheduledWorker) Start(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    w.processDueScheduledWebhooks(ctx)
    for {
        select {
        case <-ticker.C:
            w.processDueScheduledWebhooks(ctx)
        case <-ctx.Done():
            return
        }
    }
}

func (w *ScheduledWorker) processDueScheduledWebhooks(ctx context.Context) {
    now := time.Now()
    tasks, err := w.Queries.GetDueScheduledWebhooks(ctx, now)
    if err != nil {
        log.Printf("Scheduled Worker: Error fetching due webhooks: %v", err)
        return
    }
    if len(tasks) > 0 {
        log.Printf("Scheduled Worker: Found %d due webhooks.", len(tasks))
    }
    for _, task := range tasks {
        deliveryTaskID := uuid.New().String()
        err := w.Queries.CreateDeliveryTask(ctx, database.CreateDeliveryTaskParams{
            ID:             deliveryTaskID,
            SubscriptionID: task.SubscriptionID,
            Payload:        task.Payload,
        })
        if err != nil {
            _ = w.Queries.UpdateScheduledWebhookStatus(ctx, database.UpdateScheduledWebhookStatusParams{
                Status: "failed", 
                ID:     task.ID,
            })
            continue
        }

        _ = w.Queries.UpdateScheduledWebhookStatus(ctx, database.UpdateScheduledWebhookStatusParams{
            Status: "delivered",
            ID:     task.ID,
        })

        next := nextOccurrence(task.ScheduledFor, task.Recurrence.String)
        if next.After(now) && task.Recurrence.String != "none" {
            _ = w.Queries.CreateScheduledWebhook(ctx, database.CreateScheduledWebhookParams{
                ID:             uuid.New().String(),
                SubscriptionID: task.SubscriptionID,
                Payload:        task.Payload,
                ScheduledFor:   next,
                Recurrence:     task.Recurrence,
            })
        }
    }
}

func nextOccurrence(t time.Time, recurrence string) time.Time {
    switch recurrence {
    case "daily":
        return t.AddDate(0, 0, 1)
    case "weekly":
        return t.AddDate(0, 0, 7)
    case "monthly":
        return t.AddDate(0, 1, 0)
    case "none":
        return t
    default:
        if recurrence != "" { 
            log.Printf("Warning: Unknown recurrence type '%s' encountered. Treating as 'none'.", recurrence)
        }
        return t
    }
}
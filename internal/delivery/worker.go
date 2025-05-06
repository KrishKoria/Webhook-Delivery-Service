package delivery

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/google/uuid"
)

const (
    maxAttempts = 5
)

type Worker struct {
    Queries *database.Queries
    Cache   *cache.RedisSubscriptionCache
}

func NewWorker(queries *database.Queries, cache *cache.RedisSubscriptionCache) *Worker {
    return &Worker{Queries: queries, Cache: cache}
}

func (w *Worker) Start(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            w.processPendingTasks(ctx)
        case <-ctx.Done():
            return
        }
    }
}

func (w *Worker) processPendingTasks(ctx context.Context) {
    tasks, err := w.Queries.ListPendingDeliveryTasks(ctx)
    if err != nil {
        log.Printf("error fetching pending tasks: %v", err)
        return
    }

    for _, task := range tasks {
        var sub database.Subscription
        var ok bool
        if sub, ok = w.Cache.Get(task.SubscriptionID); !ok {
            sub, err = w.Queries.GetSubscription(ctx, task.SubscriptionID)
            if err != nil {
                log.Printf("error fetching subscription for task %s: %v", task.ID, err)
                continue
            }
            w.Cache.Set(task.SubscriptionID, sub)
        }

        status, httpStatus, errMsg := deliverWebhook(sub.TargetUrl, []byte(task.Payload))
        attempt := task.AttemptCount + 1

        err = w.Queries.CreateDeliveryLog(ctx, database.CreateDeliveryLogParams{
            ID:             generateUUID(),
            DeliveryTaskID: task.ID,
            SubscriptionID: task.SubscriptionID,
            TargetUrl:      sub.TargetUrl,
            Timestamp:      time.Now(),
            AttemptNumber:  int64(attempt),
            Outcome:        status,
            HttpStatus: sql.NullInt64{
                Int64: int64(httpStatus),
                Valid: httpStatus != 0,
            },
            ErrorDetails: sql.NullString{
                String: errMsg,
                Valid:  errMsg != "",
            },
        })
        if err != nil {
            log.Printf("error logging delivery attempt for task %s: %v", task.ID, err)
        }

        newStatus := task.Status
        if status == "success" {
            newStatus = "delivered"
        } else if attempt >= maxAttempts {
            newStatus = "failed"
        }

        err = w.Queries.UpdateDeliveryTaskStatus(ctx, database.UpdateDeliveryTaskStatusParams{
            Status: newStatus,
            LastAttemptAt: sql.NullTime{
                Time:  time.Now(),
                Valid: true,
            },
            AttemptCount: int64(attempt),
            ID:           task.ID,
        })
        if err != nil {
            log.Printf("error updating task status for %s: %v", task.ID, err)
        }
        
        
        if status != "success" && attempt >= maxAttempts {
            dlqErr := w.Queries.InsertDeadLetterTask(ctx, database.InsertDeadLetterTaskParams{
                ID:              generateUUID(),
                OriginalTaskID:  task.ID,
                SubscriptionID:  task.SubscriptionID,
                Payload:         task.Payload,
                FailedAt:        time.Now(),
                Reason:          errMsg,
                LastAttemptAt:   sql.NullTime{
                    Time:  time.Now(),
                    Valid: true,
                },
                AttemptCount:    int64(attempt),
                Status:          "pending",
                TargetUrl:       sql.NullString{
                    String: sub.TargetUrl,
                    Valid:  sub.TargetUrl != "",
                },
                EventType:       sql.NullString{
                    String: "",
                    Valid:  false, 
                },
                ErrorDetails:    sql.NullString{String: errMsg, Valid: errMsg != ""},
            })
            if dlqErr != nil {
                log.Printf("error inserting into dead letter queue for task %s: %v", task.ID, dlqErr)
            } else {
                log.Printf("Task %s moved to dead letter queue after %d attempts", task.ID, attempt)
            }
        }

        if status != "success" && attempt < maxAttempts {
            backoff := getBackoffDuration(int(attempt))
            nextAttempt := time.Now().Add(backoff)
            
            err = w.Queries.UpdateDeliveryTaskNextAttemptAt(ctx, database.UpdateDeliveryTaskNextAttemptAtParams{
                ID: task.ID,
                NextAttemptAt: sql.NullTime{
                    Time: nextAttempt,
                    Valid: true,
                },
            })
            if err != nil {
                log.Printf("Error updating next attempt time: %v", err)
            }
        }
    }
}

func deliverWebhook(targetURL string, payload []byte) (status string, httpStatus int, errMsg string) {
    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Post(targetURL, "application/json", bytes.NewReader(payload))
    if err != nil {
        return "failed_attempt", 0, err.Error()
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 200 && resp.StatusCode < 300 {
        return "success", resp.StatusCode, ""
    }
    return "failed_attempt", resp.StatusCode, resp.Status
}

func getBackoffDuration(attempt int) time.Duration {
    switch attempt {
    case 1:
        return 10 * time.Second
    case 2:
        return 30 * time.Second
    case 3:
        return 1 * time.Minute
    case 4:
        return 5 * time.Minute
    default:
        return 15 * time.Minute
    }
}

func generateUUID() string {
    return uuid.New().String()
}
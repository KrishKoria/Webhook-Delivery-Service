package delivery

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/google/uuid"
)

const (
    maxAttempts = 5
    defaultLeaseSeconds = 60 // 1 minute lease by default
    defaultWorkerConcurrency = 5
)

type Worker struct {
    Queries *database.Queries
    Cache   *cache.RedisSubscriptionCache
    HTTPClient *http.Client
    HTTPConfig HTTPClientConfig
    WorkerID string // Unique identifier for this worker instance
    LeaseSeconds int64
    Concurrency int
}

type WorkerConfig struct {
    WorkerID string
    LeaseSeconds int64
    Concurrency int
    HTTPClientConfig *HTTPClientConfig // Optional custom HTTP config
}

func NewWorker(queries *database.Queries, cache *cache.RedisSubscriptionCache) *Worker {
    workerID := os.Getenv("WORKER_ID")
    if workerID == "" {
        workerID = uuid.New().String()[:8] // Short unique ID
    }
    
    httpConfig := LoadHTTPClientConfigFromEnv()
    
    return &Worker{
        Queries:      queries,
        Cache:        cache,
        HTTPClient:   NewSecureHTTPClient(httpConfig),
        HTTPConfig:   httpConfig,
        WorkerID:     workerID,
        LeaseSeconds: defaultLeaseSeconds,
        Concurrency:  defaultWorkerConcurrency,
    }
}

func NewWorkerWithConfig(queries *database.Queries, cache *cache.RedisSubscriptionCache, config WorkerConfig) *Worker {
    if config.WorkerID == "" {
        config.WorkerID = uuid.New().String()[:8]
    }
    if config.LeaseSeconds <= 0 {
        config.LeaseSeconds = defaultLeaseSeconds
    }
    if config.Concurrency <= 0 {
        config.Concurrency = defaultWorkerConcurrency
    }
    
    // Use provided HTTP config or load from env
    httpConfig := LoadHTTPClientConfigFromEnv()
    if config.HTTPClientConfig != nil {
        httpConfig = *config.HTTPClientConfig
    }
    
    return &Worker{
        Queries:      queries,
        Cache:        cache,
        HTTPClient:   NewSecureHTTPClient(httpConfig),
        HTTPConfig:   httpConfig,
        WorkerID:     config.WorkerID,
        LeaseSeconds: config.LeaseSeconds,
        Concurrency:  config.Concurrency,
    }
}

func (w *Worker) Start(ctx context.Context) {
    log.Printf("Worker %s starting with concurrency=%d, lease=%ds", w.WorkerID, w.Concurrency, w.LeaseSeconds)
    
    // Create a semaphore channel to limit concurrency
    sem := make(chan struct{}, w.Concurrency)
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            // Try to claim and process tasks up to concurrency limit
            for i := 0; i < w.Concurrency; i++ {
                select {
                case sem <- struct{}{}: // Acquire semaphore
                    go func() {
                        defer func() { <-sem }() // Release semaphore
                        w.claimAndProcessTask(ctx)
                    }()
                default:
                    // All slots busy, wait for next tick
                    break
                }
            }
        case <-ctx.Done():
            log.Printf("Worker %s shutting down", w.WorkerID)
            return
        }
    }
}

func (w *Worker) claimAndProcessTask(ctx context.Context) {
    // Two-step claim for Turso/libSQL compatibility (doesn't support subqueries in WHERE)
    // Step 1: Get the ID of the next claimable task
    taskID, err := w.Queries.GetNextClaimableTaskId(ctx)
    if err != nil {
        if err == sql.ErrNoRows {
            // No tasks available, this is normal
            return
        }
        log.Printf("Worker %s: error getting next task: %v", w.WorkerID, err)
        return
    }

    // Step 2: Attempt to claim that specific task
    task, err := w.Queries.ClaimDeliveryTask(ctx, database.ClaimDeliveryTaskParams{
        WorkerID:     sql.NullString{String: w.WorkerID, Valid: true},
        LeaseSeconds: sql.NullString{String: fmt.Sprintf("%d", w.LeaseSeconds), Valid: true},
        TaskID:       taskID,
    })
    if err != nil {
        if err == sql.ErrNoRows {
            // Task was claimed by another worker, this is normal in concurrent scenarios
            return
        }
        log.Printf("Worker %s: error claiming task: %v", w.WorkerID, err)
        return
    }

    log.Printf("Worker %s: claimed task %s (attempt %d)", w.WorkerID, task.ID, task.AttemptCount+1)
    
    // Process the task
    w.processTask(ctx, task)
}

func (w *Worker) processTask(ctx context.Context, task database.DeliveryTask) {
    var sub database.Subscription
    var ok bool
    var err error
    
    if w.Cache != nil {
        sub, ok = w.Cache.Get(task.SubscriptionID)
    }
    if !ok {
        sub, err = w.Queries.GetSubscription(ctx, task.SubscriptionID)
        if err != nil {
            log.Printf("Worker %s: error fetching subscription for task %s: %v", w.WorkerID, task.ID, err)
            // Release lease so another worker can try
            _ = w.Queries.ReleaseTaskLease(ctx, task.ID)
            return
        }
        if w.Cache != nil {
            w.Cache.Set(task.SubscriptionID, sub)
        }
    }

    status, httpStatus, errMsg := w.deliverWebhook(sub.TargetUrl, []byte(task.Payload))
    attempt := task.AttemptCount + 1

    // Log the delivery attempt
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
        log.Printf("Worker %s: error logging delivery attempt for task %s: %v", w.WorkerID, task.ID, err)
    }

    // Determine new status
    newStatus := task.Status
    if status == "success" {
        newStatus = "delivered"
    } else if attempt >= maxAttempts {
        newStatus = "failed"
    }

    // Update task status and release lease
    err = w.Queries.UpdateDeliveryTaskStatusAndReleaseLease(ctx, database.UpdateDeliveryTaskStatusAndReleaseLeaseParams{
        Status: newStatus,
        LastAttemptAt: sql.NullTime{
            Time:  time.Now(),
            Valid: true,
        },
        AttemptCount: int64(attempt),
        ID:           task.ID,
    })
    if err != nil {
        log.Printf("Worker %s: error updating task status for %s: %v", w.WorkerID, task.ID, err)
    }

    // Handle max attempts reached - move to DLQ
    if status != "success" && attempt >= maxAttempts {
        dlqErr := w.Queries.InsertDeadLetterTask(ctx, database.InsertDeadLetterTaskParams{
            ID:             generateUUID(),
            OriginalTaskID: task.ID,
            SubscriptionID: task.SubscriptionID,
            Payload:        task.Payload,
            FailedAt:       time.Now(),
            Reason:         errMsg,
            LastAttemptAt: sql.NullTime{
                Time:  time.Now(),
                Valid: true,
            },
            AttemptCount: int64(attempt),
            Status:       "pending",
            TargetUrl: sql.NullString{
                String: sub.TargetUrl,
                Valid:  sub.TargetUrl != "",
            },
            EventType: sql.NullString{
                String: "",
                Valid:  false,
            },
            ErrorDetails: sql.NullString{String: errMsg, Valid: errMsg != ""},
        })
        if dlqErr != nil {
            log.Printf("Worker %s: error inserting into dead letter queue for task %s: %v", w.WorkerID, task.ID, dlqErr)
        } else {
            log.Printf("Worker %s: Task %s moved to dead letter queue after %d attempts", w.WorkerID, task.ID, attempt)
        }
    }

    // Schedule retry if needed
    if status != "success" && attempt < maxAttempts {
        backoff := getBackoffDuration(int(attempt))
        nextAttempt := time.Now().Add(backoff)

        err = w.Queries.UpdateDeliveryTaskNextAttemptAt(ctx, database.UpdateDeliveryTaskNextAttemptAtParams{
            ID: task.ID,
            NextAttemptAt: sql.NullTime{
                Time:  nextAttempt,
                Valid: true,
            },
        })
        if err != nil {
            log.Printf("Worker %s: Error updating next attempt time: %v", w.WorkerID, err)
        }
    }
}

func(w *Worker) deliverWebhook(targetURL string, payload []byte) (status string, httpStatus int, errMsg string) {
    resp, err := w.HTTPClient.Post(targetURL, "application/json", bytes.NewBuffer(payload))
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
    return getBackoffWithJitter(attempt)
}

// getBackoffWithJitter returns exponential backoff with full jitter
// Formula: random(0, min(cap, base * 2^attempt))
// This prevents thundering herd problem when many tasks fail simultaneously
func getBackoffWithJitter(attempt int) time.Duration {
    const (
        baseDelay = 10 * time.Second // Initial delay
        maxDelay  = 15 * time.Minute // Maximum delay cap
    )

    // Calculate exponential backoff: base * 2^(attempt-1)
    backoff := baseDelay * (1 << uint(attempt-1))
    
    // Cap at maximum
    if backoff > maxDelay {
        backoff = maxDelay
    }

    // Add full jitter: random value between 0 and backoff
    // This spreads out retry attempts to prevent thundering herd
    jitter := time.Duration(rand.Int63n(int64(backoff)))
    
    // Ensure minimum delay of 5 seconds
    if jitter < 5*time.Second {
        jitter = 5 * time.Second
    }

    return jitter
}

// getBackoffWithoutJitter returns deterministic backoff for testing
func getBackoffWithoutJitter(attempt int) time.Duration {
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
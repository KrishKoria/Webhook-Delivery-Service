package delivery

import (
	"context"
	"log"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
)

type CleanupWorker struct {
    Queries *database.Queries
}

func NewCleanupWorker(queries *database.Queries) *CleanupWorker {
    return &CleanupWorker{Queries: queries}
}

func (w *CleanupWorker) Start(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if err := w.cleanupOldLogs(ctx); err != nil {
                log.Printf("log cleanup error: %v", err)
            }
        case <-ctx.Done():
            return
        }
    }
}

func (w *CleanupWorker) cleanupOldLogs(ctx context.Context) error {
    err := w.Queries.DeleteOldDeliveryLogs(ctx)
    if err == nil {
        log.Println("Old delivery logs cleaned up")
    }
    return err
}
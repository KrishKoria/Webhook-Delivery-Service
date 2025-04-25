package models

import (
	"time"
)

// Subscription represents a webhook subscription.
type Subscription struct {
    ID        string    `json:"id" db:"id"` // UUID
    TargetURL string    `json:"target_url" db:"target_url"`
    Secret    string    `json:"secret,omitempty" db:"secret"` // Optional
    CreatedAt time.Time `json:"created_at" db:"created_at"`
    UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// DeliveryTask represents a queued webhook delivery.
type DeliveryTask struct {
    ID             string    `json:"id" db:"id"` // UUID
    SubscriptionID string    `json:"subscription_id" db:"subscription_id"`
    Payload        string    `json:"payload" db:"payload"` // JSON as string
    CreatedAt      time.Time `json:"created_at" db:"created_at"`
    Status         string    `json:"status" db:"status"` // pending, delivered, failed
    LastAttemptAt  time.Time `json:"last_attempt_at" db:"last_attempt_at"`
    AttemptCount   int       `json:"attempt_count" db:"attempt_count"`
}

// DeliveryLog represents an attempt to deliver a webhook.
type DeliveryLog struct {
    ID             string    `json:"id" db:"id"` // UUID
    DeliveryTaskID string    `json:"delivery_task_id" db:"delivery_task_id"`
    SubscriptionID string    `json:"subscription_id" db:"subscription_id"`
    TargetURL      string    `json:"target_url" db:"target_url"`
    Timestamp      time.Time `json:"timestamp" db:"timestamp"`
    AttemptNumber  int       `json:"attempt_number" db:"attempt_number"`
    Outcome        string    `json:"outcome" db:"outcome"` // success, failed_attempt, failure
    HTTPStatus     int       `json:"http_status" db:"http_status"`
    ErrorDetails   string    `json:"error_details,omitempty" db:"error_details"`
}
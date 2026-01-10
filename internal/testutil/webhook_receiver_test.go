package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestWebhookReceiverBasic(t *testing.T) {
	receiver := NewWebhookReceiver()
	defer receiver.Close()

	// Send a request
	payload := map[string]string{"test": "data"}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(receiver.URL()+"/webhook", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	// Verify request was recorded
	if receiver.RequestCount() != 1 {
		t.Errorf("Expected 1 request, got %d", receiver.RequestCount())
	}

	req := receiver.LastRequest()
	if req == nil {
		t.Fatal("Expected last request to exist")
	}
	if req.Method != "POST" {
		t.Errorf("Expected POST method, got %s", req.Method)
	}
	if req.Path != "/webhook" {
		t.Errorf("Expected /webhook path, got %s", req.Path)
	}

	var receivedPayload map[string]string
	if err := req.ParseJSONBody(&receivedPayload); err != nil {
		t.Fatalf("Failed to parse body: %v", err)
	}
	if receivedPayload["test"] != "data" {
		t.Errorf("Expected test=data, got %v", receivedPayload)
	}
}

func TestWebhookReceiverCustomResponse(t *testing.T) {
	receiver := NewWebhookReceiver()
	defer receiver.Close()

	receiver.SetResponseCode(http.StatusCreated)
	receiver.SetResponseBody(`{"status":"created"}`)

	resp, err := http.Post(receiver.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected 201, got %d", resp.StatusCode)
	}
}

func TestWebhookReceiverFailAfter(t *testing.T) {
	receiver := NewWebhookReceiver()
	defer receiver.Close()

	receiver.FailAfter(2) // Fail after 2 successful requests

	// First two should succeed
	for i := 0; i < 2; i++ {
		resp, err := http.Post(receiver.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
		if err != nil {
			t.Fatalf("Request %d failed: %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, resp.StatusCode)
		}
	}

	// Third should fail
	resp, err := http.Post(receiver.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		t.Fatalf("Request 3 failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Request 3: expected 500, got %d", resp.StatusCode)
	}
}

func TestWebhookReceiverFailUntil(t *testing.T) {
	receiver := NewWebhookReceiver()
	defer receiver.Close()

	receiver.FailUntil(2) // Fail until 2 requests received

	// First two should fail
	for i := 0; i < 2; i++ {
		resp, err := http.Post(receiver.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
		if err != nil {
			t.Fatalf("Request %d failed: %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("Request %d: expected 500, got %d", i+1, resp.StatusCode)
		}
	}

	// Third should succeed
	resp, err := http.Post(receiver.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		t.Fatalf("Request 3 failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Request 3: expected 200, got %d", resp.StatusCode)
	}
}

func TestWebhookReceiverDelay(t *testing.T) {
	receiver := NewWebhookReceiver()
	defer receiver.Close()

	receiver.SetDelay(100 * time.Millisecond)

	start := time.Now()
	resp, err := http.Post(receiver.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	resp.Body.Close()
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Errorf("Expected delay of at least 100ms, got %v", elapsed)
	}
}

func TestWebhookReceiverReset(t *testing.T) {
	receiver := NewWebhookReceiver()
	defer receiver.Close()

	receiver.SetResponseCode(http.StatusCreated)
	resp, _ := http.Post(receiver.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
	resp.Body.Close()

	if receiver.RequestCount() != 1 {
		t.Errorf("Expected 1 request before reset, got %d", receiver.RequestCount())
	}

	receiver.Reset()

	if receiver.RequestCount() != 0 {
		t.Errorf("Expected 0 requests after reset, got %d", receiver.RequestCount())
	}

	// Response code should be reset to 200
	resp, _ = http.Post(receiver.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 after reset, got %d", resp.StatusCode)
	}
}

func TestWebhookReceiverWaitForRequest(t *testing.T) {
	receiver := NewWebhookReceiver()
	defer receiver.Close()

	// Start goroutine to send request after delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		http.Post(receiver.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
	}()

	if !receiver.WaitForRequest(1, 200*time.Millisecond) {
		t.Error("WaitForRequest should have succeeded")
	}
}

func TestWebhookReceiverWaitForRequestTimeout(t *testing.T) {
	receiver := NewWebhookReceiver()
	defer receiver.Close()

	if receiver.WaitForRequest(1, 50*time.Millisecond) {
		t.Error("WaitForRequest should have timed out")
	}
}

func TestWebhookReceiverPool(t *testing.T) {
	pool := NewWebhookReceiverPool()
	defer pool.CloseAll()

	receiver1 := pool.Get("sub1")
	receiver2 := pool.Get("sub2")

	if receiver1.URL() == receiver2.URL() {
		t.Error("Different receivers should have different URLs")
	}

	// Same name should return same receiver
	receiver1Again := pool.Get("sub1")
	if receiver1.URL() != receiver1Again.URL() {
		t.Error("Same name should return same receiver")
	}

	// Send requests
	http.Post(receiver1.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
	http.Post(receiver2.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
	http.Post(receiver2.URL(), "application/json", bytes.NewBuffer([]byte("{}")))

	if pool.TotalRequestCount() != 3 {
		t.Errorf("Expected 3 total requests, got %d", pool.TotalRequestCount())
	}

	pool.ResetAll()

	if pool.TotalRequestCount() != 0 {
		t.Errorf("Expected 0 requests after reset, got %d", pool.TotalRequestCount())
	}
}

func TestReceivedRequestHelpers(t *testing.T) {
	receiver := NewWebhookReceiver()
	defer receiver.Close()

	req, _ := http.NewRequest("POST", receiver.URL(), bytes.NewBuffer([]byte(`{"key":"value"}`)))
	req.Header.Set("X-Custom-Header", "test-value")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	resp.Body.Close()

	received := receiver.LastRequest()
	if received == nil {
		t.Fatal("Expected request to be recorded")
	}

	// Test GetHeader
	if received.GetHeader("X-Custom-Header") != "test-value" {
		t.Errorf("Expected X-Custom-Header=test-value, got %s", received.GetHeader("X-Custom-Header"))
	}

	// Test HasHeader
	if !received.HasHeader("X-Custom-Header") {
		t.Error("Expected HasHeader to return true for X-Custom-Header")
	}
	if received.HasHeader("X-Missing-Header") {
		t.Error("Expected HasHeader to return false for X-Missing-Header")
	}

	// Test BodyString
	if received.BodyString() != `{"key":"value"}` {
		t.Errorf("Expected body to be {\"key\":\"value\"}, got %s", received.BodyString())
	}
}

func TestMultipleRequestsRecorded(t *testing.T) {
	receiver := NewWebhookReceiver()
	defer receiver.Close()

	// Send multiple requests
	for i := 0; i < 5; i++ {
		resp, _ := http.Post(receiver.URL(), "application/json", bytes.NewBuffer([]byte("{}")))
		resp.Body.Close()
	}

	requests := receiver.Requests()
	if len(requests) != 5 {
		t.Errorf("Expected 5 requests, got %d", len(requests))
	}

	// Verify requests are independent (modifying slice doesn't affect internal state)
	requests[0].Method = "MODIFIED"
	actualRequests := receiver.Requests()
	if actualRequests[0].Method == "MODIFIED" {
		t.Error("Requests() should return a copy, not the original slice")
	}
}

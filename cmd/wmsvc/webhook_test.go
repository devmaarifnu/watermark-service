package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendWebhookSuccessAndSignature(t *testing.T) {
	secret := "my-secret-key"
	received := false
	var receivedSig string
	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		receivedSig = r.Header.Get("X-Signature")
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &WebhookConfig{
		URL:         StringOrSlice{srv.URL},
		Secret:      secret,
		Timeout:     2 * time.Second,
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
	}

	payload := WebhookPayload{
		BHPNUID:    1,
		RequestID:  "req-123",
		Status:     "success",
		Out:        "/path/to/out.pdf",
		DurationMS: 100,
		FinishedAt: "2026-09-29T14:15:00Z",
	}

	err := SendWebhook(context.Background(), cfg, payload)
	if err != nil {
		t.Fatalf("SendWebhook failed: %v", err)
	}

	if !received {
		t.Errorf("webhook was not received")
	}

	expectedAuth := "Bearer my-secret-key"
	if receivedAuth != expectedAuth {
		t.Errorf("expected Authorization header %q, got %q", expectedAuth, receivedAuth)
	}

	expectedPrefix := "sha256="
	if len(receivedSig) <= len(expectedPrefix) || receivedSig[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("expected X-Signature with prefix sha256=, got %q", receivedSig)
	}
}

func TestSendWebhookMultipleURLs(t *testing.T) {
	var count atomic.Int32
	var authHeaders []string
	var mu sync.Mutex

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		mu.Lock()
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	srv1 := httptest.NewServer(handler)
	defer srv1.Close()
	srv2 := httptest.NewServer(handler)
	defer srv2.Close()

	secret := "secret-xyz"
	cfg := &WebhookConfig{
		URL:         StringOrSlice{srv1.URL, srv2.URL},
		Secret:      secret,
		Timeout:     2 * time.Second,
		MaxAttempts: 2,
		BackoffBase: 10 * time.Millisecond,
	}

	payload := WebhookPayload{
		BHPNUID:   99,
		RequestID: "req-multi",
		Status:    "success",
	}

	err := SendWebhook(context.Background(), cfg, payload)
	if err != nil {
		t.Fatalf("SendWebhook multiple failed: %v", err)
	}

	if count.Load() != 2 {
		t.Errorf("expected 2 webhook hits, got %d", count.Load())
	}
	for _, auth := range authHeaders {
		if auth != "Bearer secret-xyz" {
			t.Errorf("expected Authorization header Bearer secret-xyz, got %q", auth)
		}
	}
}

func TestSendWebhookRetryOn5xx(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := attempts.Add(1)
		if att < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &WebhookConfig{
		URL:         StringOrSlice{srv.URL},
		Timeout:     2 * time.Second,
		MaxAttempts: 4,
		BackoffBase: 10 * time.Millisecond,
	}

	payload := WebhookPayload{
		BHPNUID:   1,
		RequestID: "req-123",
		Status:    "success",
	}

	err := SendWebhook(context.Background(), cfg, payload)
	if err != nil {
		t.Fatalf("expected SendWebhook to succeed after retries, got: %v", err)
	}

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestSendWebhookNoRetryOn4xx(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest) // 400 Bad Request (non-retryable)
	}))
	defer srv.Close()

	cfg := &WebhookConfig{
		URL:         StringOrSlice{srv.URL},
		Timeout:     2 * time.Second,
		MaxAttempts: 4,
		BackoffBase: 10 * time.Millisecond,
	}

	payload := WebhookPayload{
		BHPNUID:   1,
		RequestID: "req-123",
		Status:    "success",
	}

	err := SendWebhook(context.Background(), cfg, payload)
	if err == nil {
		t.Fatalf("expected SendWebhook to return error on 400")
	}

	if attempts.Load() != 1 {
		t.Errorf("expected only 1 attempt for 4xx status, got %d", attempts.Load())
	}
}

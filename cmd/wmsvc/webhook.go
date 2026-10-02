package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type WebhookPayload struct {
	BHPNUID    int    `json:"bhpnu_id"`
	Status     string `json:"status"`
	AktaPath   string `json:"akta_path,omitempty"`
	Message    string `json:"message,omitempty"`
	Out        string `json:"out,omitempty"`
	Error      string `json:"error,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Stage      string `json:"stage,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// computeHMAC256 menghitung signature sha256=<hex>
func computeHMAC256(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// SendWebhook mengirim notifikasi webhook dengan timeout dan retry exponential backoff.
func SendWebhook(ctx context.Context, cfg *WebhookConfig, payload WebhookPayload) error {
	if cfg.URL == "" {
		return fmt.Errorf("webhook url kosong")
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("gagal marshal webhook payload: %w", err)
	}

	var sigHeader string
	if cfg.Secret != "" {
		sigHeader = computeHMAC256(cfg.Secret, bodyBytes)
	}

	client := &http.Client{
		Timeout: cfg.Timeout,
	}

	maxAttempts := cfg.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 5
	}
	backoff := cfg.BackoffBase
	if backoff <= 0 {
		backoff = 1 * time.Second
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("gagal membuat request webhook: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if sigHeader != "" {
			req.Header.Set("X-Signature", sigHeader)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d network error: %w", attempt, err)
		} else {
			// Status 2xx -> Sukses
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				_ = resp.Body.Close()
				return nil
			}
			// Status 4xx -> Permanent failure, jangan di-retry (§5)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				return fmt.Errorf("webhook returned client error %d: %s (non-retryable)", resp.StatusCode, string(body))
			}
			// Status 5xx -> Server error, retry
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
		}

		if attempt < maxAttempts {
			waitTime := backoff * time.Duration(1<<(attempt-1))
			log.Printf("[webhook] Percobaan %d gagal: %v. Mencoba lagi dalam %v...", attempt, lastErr, waitTime)
			select {
			case <-ctx.Done():
				return fmt.Errorf("webhook context dibatalkan: %w (last error: %v)", ctx.Err(), lastErr)
			case <-time.After(waitTime):
			}
		}
	}

	return fmt.Errorf("webhook gagal setelah %d percobaan: %w", maxAttempts, lastErr)
}

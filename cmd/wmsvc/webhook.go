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
	"strings"
	"sync"
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

// SendWebhook mengirim notifikasi webhook ke seluruh URL yang terdaftar dengan timeout dan retry exponential backoff.
func SendWebhook(ctx context.Context, cfg *WebhookConfig, payload WebhookPayload) error {
	urls := cfg.URL
	if len(urls) == 0 {
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

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []string
	)

	for _, targetURL := range urls {
		targetURL := strings.TrimSpace(targetURL)
		if targetURL == "" {
			continue
		}
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			if err := sendToSingleURL(ctx, client, url, cfg.Secret, sigHeader, bodyBytes, maxAttempts, backoff); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("[%s]: %v", url, err))
				mu.Unlock()
			}
		}(targetURL)
	}

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("pengiriman webhook gagal untuk %d url: %s", len(errs), strings.Join(errs, "; "))
	}
	return nil
}

func sendToSingleURL(ctx context.Context, client *http.Client, targetURL, secret, sigHeader string, bodyBytes []byte, maxAttempts int, backoff time.Duration) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("gagal membuat request webhook: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if secret != "" {
			if strings.HasPrefix(secret, "Bearer ") {
				req.Header.Set("Authorization", secret)
			} else {
				req.Header.Set("Authorization", "Bearer "+secret)
			}
		}
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
			log.Printf("[webhook] URL %s: Percobaan %d gagal: %v. Mencoba lagi dalam %v...", targetURL, attempt, lastErr, waitTime)
			select {
			case <-ctx.Done():
				return fmt.Errorf("webhook context dibatalkan: %w (last error: %v)", ctx.Err(), lastErr)
			case <-time.After(waitTime):
			}
		}
	}

	return fmt.Errorf("webhook gagal setelah %d percobaan: %w", maxAttempts, lastErr)
}

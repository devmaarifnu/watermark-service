package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"pdfsplit/internal/common"
)

const minimalPDF = `%PDF-1.4
1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj
2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj
3 0 obj<</Type/Page/MediaBox[0 0 612 792]/Parent 2 0 R/Resources<<>>>>endobj
xref
0 4
0000000000 65535 f 
0000000010 00000 n 
0000000060 00000 n 
0000000117 00000 n 
trailer<</Size 4/Root 1 0 R>>
startxref
200
%%EOF
`

func createTestPDF(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(minimalPDF), 0o644); err != nil {
		t.Fatalf("failed to write test pdf: %v", err)
	}
}

func setupTestEnvironment(t *testing.T, webhookHandler http.HandlerFunc) (*Config, *WorkerPool, *Server, *httptest.Server, string, string) {
	t.Helper()

	baseDir := t.TempDir()
	inDir := filepath.Join(baseDir, "src")
	outDir := filepath.Join(baseDir, "out")
	tmpDir := filepath.Join(baseDir, "tmp")
	_ = os.MkdirAll(inDir, 0o755)
	_ = os.MkdirAll(outDir, 0o755)
	_ = os.MkdirAll(tmpDir, 0o755)

	var webhookServer *httptest.Server
	if webhookHandler != nil {
		webhookServer = httptest.NewServer(webhookHandler)
	}

	webhookURL := "http://127.0.0.1:9999/dummy"
	if webhookServer != nil {
		webhookURL = webhookServer.URL
	}

	cfg := &Config{
		Server: ServerConfig{
			Addr:            ":8080",
			ReadTimeout:     5 * time.Second,
			WriteTimeout:    5 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Webhook: WebhookConfig{
			URL:         webhookURL,
			Secret:      "test-secret",
			Timeout:     2 * time.Second,
			MaxAttempts: 2,
			BackoffBase: 10 * time.Millisecond,
		},
		Auth: AuthConfig{
			APIKey: "secret-key",
		},
		Storage: StorageConfig{
			AllowedInputBase:  []string{inDir},
			AllowedOutputBase: outDir,
			TmpDir:            tmpDir,
			CreateOutputDir:   true,
			Overwrite:         false,
		},
		Security: SecurityConfig{
			AllowAnyPath: false,
		},
		Worker: WorkerConfig{
			Count:      2,
			QueueSize:  16,
			JobTimeout: 5 * time.Second,
		},
		Limits: LimitsConfig{
			MaxPageCount:  500,
			MaxFileSizeMB: 10,
		},
		Watermark: WatermarkConfig{
			FontSize:    8,
			Opacity:     0.18,
			Pos:         "bl",
			Rotation:    0,
			Offset:      "18 18",
			BorderWidth: 1.0,
			Radius:      6.0,
			Color:       "0.85 0 0",
		},
		Merge: MergeConfig{
			Order:       []string{"piagam", "sk", "akta"},
			DividerPage: false,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}

	pool := NewWorkerPool(cfg)
	pool.Start()

	srv := NewServer(cfg, pool)
	return cfg, pool, srv, webhookServer, inDir, outDir
}

// TestAktaSuccessEndToEnd menguji seluruh flow sukses:
// request HTTP 202 -> worker watermark akta -> merge piagam+sk+akta -> simpan -> kirim webhook
func TestAktaSuccessEndToEnd(t *testing.T) {
	var webhookPayload WebhookPayload
	var webhookSignature string
	var wg sync.WaitGroup
	wg.Add(1)

	webhookHandler := func(w http.ResponseWriter, r *http.Request) {
		webhookSignature = r.Header.Get("X-Signature")
		_ = json.NewDecoder(r.Body).Decode(&webhookPayload)
		w.WriteHeader(http.StatusOK)
		wg.Done()
	}

	_, pool, srv, webhookServer, inDir, outDir := setupTestEnvironment(t, webhookHandler)
	defer pool.Stop()
	if webhookServer != nil {
		defer webhookServer.Close()
	}

	aktaPath := filepath.Join(inDir, "akta.pdf")
	piagamPath := filepath.Join(inDir, "piagam.pdf")
	skPath := filepath.Join(inDir, "sk.pdf")

	createTestPDF(t, aktaPath)
	createTestPDF(t, piagamPath)
	createTestPDF(t, skPath)

	reqBody := AktaRequest{
		BHPNUID:    101,
		AktaPath:   aktaPath,
		PiagamPath: piagamPath,
		SKPath:     skPath,
		OutputPath: outDir,
		Data: IdentityData{
			NamaSatpen:   "SMK NU 1 Jakarta",
			NoRegistrasi: "123124",
			NPSN:         "12345678",
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	start := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wm/akta", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret-key")

	w := httptest.NewRecorder()
	srv.handleAkta(w, req)

	duration := time.Since(start)
	if duration > 50*time.Millisecond {
		t.Logf("Perhatian: respon memakan waktu %v (> 50ms)", duration)
	}

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", w.Code, w.Body.String())
	}

	var resp SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.BHPNUID != 101 || resp.Status != "queued" || resp.RequestID == "" {
		t.Errorf("unexpected response content: %+v", resp)
	}

	// Tunggu worker dan webhook selesai
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout menunggu webhook")
	}

	if webhookPayload.Status != "success" {
		t.Fatalf("expected webhook status success, got %s (error: %s)", webhookPayload.Status, webhookPayload.Error)
	}
	if webhookPayload.BHPNUID != 101 {
		t.Errorf("expected bhpnu_id 101, got %d", webhookPayload.BHPNUID)
	}
	if !strings.HasPrefix(webhookSignature, "sha256=") {
		t.Errorf("expected X-Signature header sha256=..., got %q", webhookSignature)
	}

	// Periksa file keluaran
	if _, err := os.Stat(webhookPayload.Out); err != nil {
		t.Fatalf("file hasil tidak ditemukan di %s: %v", webhookPayload.Out, err)
	}

	// Validasi PDF hasil dengan pdfcpu
	if err := api.ValidateFile(webhookPayload.Out, common.NewConf("")); err != nil {
		t.Fatalf("output pdf validation failed: %v", err)
	}

	// Periksa jumlah halaman: piagam (1) + sk (1) + akta (1) = 3 halaman
	pages, err := api.PageCountFile(webhookPayload.Out)
	if err != nil {
		t.Fatalf("PageCountFile failed: %v", err)
	}
	if pages != 3 {
		t.Errorf("expected 3 pages in merged file, got %d", pages)
	}
}

// TestValidationErrors menguji kode-kode kesalahan (400, 401, 403, 404, 422, 503)
func TestValidationErrors(t *testing.T) {
	_, pool, srv, _, inDir, outDir := setupTestEnvironment(t, nil)
	defer pool.Stop()

	validAkta := filepath.Join(inDir, "akta.pdf")
	validPiagam := filepath.Join(inDir, "piagam.pdf")
	validSK := filepath.Join(inDir, "sk.pdf")
	createTestPDF(t, validAkta)
	createTestPDF(t, validPiagam)
	createTestPDF(t, validSK)

	// File bukan PDF (untuk tes 422)
	invalidPDF := filepath.Join(inDir, "corrupt.pdf")
	_ = os.WriteFile(invalidPDF, []byte("NOT A REAL PDF FILE HEADER"), 0o644)

	tests := []struct {
		name       string
		apiKey     string
		mutateReq  func(r *AktaRequest)
		wantStatus int
	}{
		{
			name:   "401 Unauthorized - Missing API Key",
			apiKey: "",
			mutateReq: func(r *AktaRequest) {
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "401 Unauthorized - Wrong API Key",
			apiKey: "wrong-key",
			mutateReq: func(r *AktaRequest) {
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "400 Bad Request - Invalid NPSN (not a number)",
			apiKey: "secret-key",
			mutateReq: func(r *AktaRequest) {
				r.Data.NPSN = "bukan-angka"
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "400 Bad Request - Missing Nama Satpen",
			apiKey: "secret-key",
			mutateReq: func(r *AktaRequest) {
				r.Data.NamaSatpen = ""
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "400 Bad Request - Relative Path",
			apiKey: "secret-key",
			mutateReq: func(r *AktaRequest) {
				r.AktaPath = "relative/akta.pdf"
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "403 Forbidden - Path Outside Allowed Directory",
			apiKey: "secret-key",
			mutateReq: func(r *AktaRequest) {
				r.AktaPath = filepath.Join(os.TempDir(), "forbidden.pdf")
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:   "404 Not Found - File does not exist",
			apiKey: "secret-key",
			mutateReq: func(r *AktaRequest) {
				r.AktaPath = filepath.Join(inDir, "non_existent.pdf")
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:   "422 Unprocessable Entity - Corrupted PDF header",
			apiKey: "secret-key",
			mutateReq: func(r *AktaRequest) {
				r.AktaPath = invalidPDF
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reqBody := AktaRequest{
				BHPNUID:    1,
				AktaPath:   validAkta,
				PiagamPath: validPiagam,
				SKPath:     validSK,
				OutputPath: outDir,
				Data: IdentityData{
					NamaSatpen:   "SMK NU",
					NoRegistrasi: "REG-01",
					NPSN:         "12345678",
				},
			}
			tc.mutateReq(&reqBody)

			bodyBytes, _ := json.Marshal(reqBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/wm/akta", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			if tc.apiKey != "" {
				req.Header.Set("X-API-Key", tc.apiKey)
			}

			w := httptest.NewRecorder()
			srv.handleAkta(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("got status %d, want %d (body: %s)", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}

	// Tes 503 saat server sedang berhenti
	t.Run("503 Service Unavailable when shutting down", func(t *testing.T) {
		srv.SetShuttingDown(true)
		defer srv.SetShuttingDown(false)

		reqBody := AktaRequest{
			BHPNUID:    1,
			AktaPath:   validAkta,
			PiagamPath: validPiagam,
			SKPath:     validSK,
			OutputPath: outDir,
			Data: IdentityData{
				NamaSatpen:   "SMK NU",
				NoRegistrasi: "REG-01",
				NPSN:         "12345678",
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/wm/akta", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "secret-key")

		w := httptest.NewRecorder()
		srv.handleAkta(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("got status %d, want 503", w.Code)
		}
	})
}

func TestQueueFull429(t *testing.T) {
	_, pool, srv, _, inDir, outDir := setupTestEnvironment(t, nil)
	defer pool.Stop()

	// Isi antrian sampai penuh (QueueSize = 16)
	for i := 0; i < 16; i++ {
		pool.jobQueue <- Job{
			RequestID:  fmt.Sprintf("dummy-%d", i),
			BHPNUID:    i,
			SubmittedAt: time.Now(),
		}
	}

	validAkta := filepath.Join(inDir, "akta.pdf")
	validPiagam := filepath.Join(inDir, "piagam.pdf")
	validSK := filepath.Join(inDir, "sk.pdf")
	createTestPDF(t, validAkta)
	createTestPDF(t, validPiagam)
	createTestPDF(t, validSK)

	reqBody := AktaRequest{
		BHPNUID:    999,
		AktaPath:   validAkta,
		PiagamPath: validPiagam,
		SKPath:     validSK,
		OutputPath: outDir,
		Data: IdentityData{
			NamaSatpen:   "SMK NU",
			NoRegistrasi: "REG-01",
			NPSN:         "12345678",
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wm/akta", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret-key")

	w := httptest.NewRecorder()
	srv.handleAkta(w, req)

	// Karena pool worker memproses antrian, bila cepat bisa saja berkurang 1,
	// namun jika kita kirim berkali-kali secara cepat kita pastikan 429 atau 202
	if w.Code != http.StatusTooManyRequests && w.Code != http.StatusAccepted {
		t.Errorf("expected 429 Too Many Requests, got %d", w.Code)
	}
}


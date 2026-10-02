package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type AktaRequest struct {
	BHPNUID    int          `json:"bhpnu_id"`
	AktaPath   string       `json:"akta_path"`
	PiagamPath string       `json:"piagam_path"`
	SKPath     string       `json:"sk_path"`
	OutputPath string       `json:"output_path"`
	Data       IdentityData `json:"data"`
}

type SuccessResponse struct {
	BHPNUID   int    `json:"bhpnu_id"`
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}

type ErrorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id"`
}

type Server struct {
	cfg          *Config
	pool         *WorkerPool
	isShuttingDown atomic.Bool
}

func NewServer(cfg *Config, pool *WorkerPool) *Server {
	return &Server{
		cfg:  cfg,
		pool: pool,
	}
}

func (s *Server) SetShuttingDown(val bool) {
	s.isShuttingDown.Store(val)
}

func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) handleAkta(w http.ResponseWriter, r *http.Request) {
	reqID := generateRequestID()

	if s.isShuttingDown.Load() {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{
			Error:     "Service sedang berhenti",
			RequestID: reqID,
		})
		return
	}

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{
			Error:     "Method not allowed",
			RequestID: reqID,
		})
		return
	}

	// 1. Auth check (§4 & §9)
	if s.cfg.Auth.APIKey != "" {
		clientKey := r.Header.Get("X-API-Key")
		if clientKey == "" || clientKey != s.cfg.Auth.APIKey {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{
				Error:     "API key salah atau tidak ada",
				RequestID: reqID,
			})
			return
		}
	}

	// 2. Decode JSON (§4 - 400)
	var req AktaRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:     fmt.Sprintf("JSON tidak valid: %v", err),
			RequestID: reqID,
		})
		return
	}

	// 3. Validasi Field Wajib Kosong & NPSN (§4 - 400)
	if req.BHPNUID <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:     "bhpnu_id wajib diisi",
			RequestID: reqID,
		})
		return
	}
	if req.AktaPath == "" || req.PiagamPath == "" || req.SKPath == "" || req.OutputPath == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:     "akta_path, piagam_path, sk_path, dan output_path wajib diisi",
			RequestID: reqID,
		})
		return
	}
	if req.Data.NamaSatpen == "" || req.Data.NoRegistrasi == "" || req.Data.NPSN == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:     "data.nama_satpen, data.no_registrasi, dan data.npsn wajib diisi",
			RequestID: reqID,
		})
		return
	}

	// NPSN harus berupa angka
	npsnClean := strings.TrimSpace(req.Data.NPSN)
	if _, err := strconv.Atoi(npsnClean); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:     fmt.Sprintf("NPSN %q bukan angka valid", req.Data.NPSN),
			RequestID: reqID,
		})
		return
	}

	// 4. Validasi Path Absolute (§9)
	if !filepath.IsAbs(req.AktaPath) || !filepath.IsAbs(req.PiagamPath) || !filepath.IsAbs(req.SKPath) || !filepath.IsAbs(req.OutputPath) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:     "seluruh path harus berupa path absolut",
			RequestID: reqID,
		})
		return
	}

	// 5. Batasi Ekstensi .pdf (§9)
	if strings.ToLower(filepath.Ext(req.AktaPath)) != ".pdf" ||
		strings.ToLower(filepath.Ext(req.PiagamPath)) != ".pdf" ||
		strings.ToLower(filepath.Ext(req.SKPath)) != ".pdf" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:     "akta_path, piagam_path, dan sk_path harus berekstensi .pdf",
			RequestID: reqID,
		})
		return
	}
	if ext := filepath.Ext(req.OutputPath); ext != "" && strings.ToLower(ext) != ".pdf" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:     "output_path bila menyertakan ekstensi berkas harus berekstensi .pdf",
			RequestID: reqID,
		})
		return
	}

	// 6. Validasi Path Traversal & Base Directory (§9 - 403)
	if !s.cfg.Security.AllowAnyPath {
		if !s.isInputAllowed(req.AktaPath) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{
				Error:     fmt.Sprintf("akta_path %q di luar direktori yang diizinkan", req.AktaPath),
				RequestID: reqID,
			})
			return
		}
		if !s.isInputAllowed(req.PiagamPath) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{
				Error:     fmt.Sprintf("piagam_path %q di luar direktori yang diizinkan", req.PiagamPath),
				RequestID: reqID,
			})
			return
		}
		if !s.isInputAllowed(req.SKPath) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{
				Error:     fmt.Sprintf("sk_path %q di luar direktori yang diizinkan", req.SKPath),
				RequestID: reqID,
			})
			return
		}
		if !s.isOutputAllowed(req.OutputPath) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{
				Error:     fmt.Sprintf("output_path %q di luar direktori yang diizinkan", req.OutputPath),
				RequestID: reqID,
			})
			return
		}
	}

	// 7. Cek Keberadaan Berkas (404) dan Header PDF Valid (422) (§4)
	inputFiles := []struct {
		name string
		path string
	}{
		{"akta_path", req.AktaPath},
		{"piagam_path", req.PiagamPath},
		{"sk_path", req.SKPath},
	}

	for _, f := range inputFiles {
		code, err := s.validatePDFFile(f.path)
		if err != nil {
			writeJSON(w, code, ErrorResponse{
				Error:     fmt.Sprintf("%s: %v", f.name, err),
				RequestID: reqID,
			})
			return
		}
	}

	// 8. Masukkan ke Antrian Worker (429 jika antrian penuh) (§4)
	job := Job{
		RequestID:   reqID,
		BHPNUID:     req.BHPNUID,
		AktaPath:    filepath.Clean(req.AktaPath),
		PiagamPath:  filepath.Clean(req.PiagamPath),
		SKPath:      filepath.Clean(req.SKPath),
		OutputPath:  filepath.Clean(req.OutputPath),
		Data:        req.Data,
		SubmittedAt: time.Now(),
	}

	if ok := s.pool.Enqueue(job); !ok {
		writeJSON(w, http.StatusTooManyRequests, ErrorResponse{
			Error:     "Antrian penuh",
			RequestID: reqID,
		})
		return
	}

	// 9. Balas 202 Accepted (< 50 ms) (§4 & §11)
	writeJSON(w, http.StatusAccepted, SuccessResponse{
		BHPNUID:   req.BHPNUID,
		RequestID: reqID,
		Status:    "queued",
	})
}

func (s *Server) isInputAllowed(targetPath string) bool {
	for _, base := range s.cfg.Storage.AllowedInputBase {
		if isPathInsideBase(base, targetPath) {
			return true
		}
	}
	return false
}

func (s *Server) isOutputAllowed(targetPath string) bool {
	for _, base := range s.cfg.Storage.AllowedOutputBase {
		if isPathInsideBase(base, targetPath) {
			return true
		}
	}
	return false
}

func isPathInsideBase(baseDir, targetPath string) bool {
	cleanBase := filepath.Clean(baseDir)
	cleanTarget := filepath.Clean(targetPath)

	if realBase, err := filepath.EvalSymlinks(cleanBase); err == nil {
		cleanBase = realBase
	}

	curr := cleanTarget
	var unexistingParts []string
	for {
		if realCurr, err := filepath.EvalSymlinks(curr); err == nil {
			for i := len(unexistingParts) - 1; i >= 0; i-- {
				realCurr = filepath.Join(realCurr, unexistingParts[i])
			}
			cleanTarget = realCurr
			break
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		unexistingParts = append(unexistingParts, filepath.Base(curr))
		curr = parent
	}

	rel, err := filepath.Rel(cleanBase, cleanTarget)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}

	return true
}

func (s *Server) validatePDFFile(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return http.StatusNotFound, fmt.Errorf("berkas tidak ditemukan")
		}
		return http.StatusInternalServerError, fmt.Errorf("gagal membaca info berkas: %w", err)
	}

	if info.IsDir() {
		return http.StatusBadRequest, fmt.Errorf("path adalah direktori, bukan berkas")
	}

	// Batas ukuran berkas
	if s.cfg.Limits.MaxFileSizeMB > 0 {
		maxBytes := s.cfg.Limits.MaxFileSizeMB * 1024 * 1024
		if info.Size() > maxBytes {
			return http.StatusBadRequest, fmt.Errorf("ukuran berkas melebihi batas %d MB", s.cfg.Limits.MaxFileSizeMB)
		}
	}

	// Validasi header PDF (422 jika bukan PDF valid)
	f, err := os.Open(path)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("gagal membuka berkas: %w", err)
	}
	defer f.Close()

	header := make([]byte, 1024)
	n, err := f.Read(header)
	if err != nil && err != io.EOF {
		return http.StatusInternalServerError, fmt.Errorf("gagal membaca header berkas: %w", err)
	}

	// PDF dimulai dengan "%PDF-" di baris-baris awal
	if !strings.Contains(string(header[:n]), "%PDF-") {
		return http.StatusUnprocessableEntity, fmt.Errorf("berkas ada tapi bukan PDF valid")
	}

	return http.StatusOK, nil
}

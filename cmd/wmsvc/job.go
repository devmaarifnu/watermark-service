package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"pdfsplit/internal/merge"
	"pdfsplit/internal/wm"
)

type IdentityData struct {
	NamaSatpen   string `json:"nama_satpen"`
	NoRegistrasi string `json:"no_registrasi"`
	NPSN         string `json:"npsn"`
}

type Job struct {
	RequestID   string
	BHPNUID     int
	AktaPath    string
	PiagamPath  string
	SKPath      string
	OutputPath  string
	Data        IdentityData
	SubmittedAt time.Time
}

type WorkerPool struct {
	cfg      *Config
	jobQueue chan Job
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewWorkerPool(cfg *Config) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		cfg:      cfg,
		jobQueue: make(chan Job, cfg.Worker.QueueSize),
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (wp *WorkerPool) Start() {
	for i := 0; i < wp.cfg.Worker.Count; i++ {
		wp.wg.Add(1)
		go wp.worker(i + 1)
	}
}

// Enqueue mencoba memasukkan job ke antrian. Mengembalikan false bila antrian penuh.
func (wp *WorkerPool) Enqueue(job Job) bool {
	select {
	case wp.jobQueue <- job:
		return true
	default:
		return false
	}
}

// Stop menghentikan pool secara teratur (menunggu job dalam antrian selesai).
func (wp *WorkerPool) Stop() {
	close(wp.jobQueue)
	wp.wg.Wait()
	wp.cancel()
}

func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()
	log.Printf("[worker-%d] Siap melayani job", id)

	for job := range wp.jobQueue {
		wp.processJob(id, job)
	}

	log.Printf("[worker-%d] Berhenti", id)
}

func (wp *WorkerPool) processJob(workerID int, job Job) {
	start := time.Now()
	log.Printf("[worker-%d] Memproses job bhpnu_id=%d (req=%s)", workerID, job.BHPNUID, job.RequestID)

	jobTimeout := wp.cfg.Worker.JobTimeout
	if jobTimeout <= 0 {
		jobTimeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(wp.ctx, jobTimeout)
	defer cancel()

	// Direktori sementara unik per job
	tmpDir := wp.cfg.Storage.TmpDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	jobTmpDir, err := os.MkdirTemp(tmpDir, fmt.Sprintf("job_%d_%s_*", job.BHPNUID, job.RequestID))
	if err != nil {
		wp.notifyFailure(ctx, job, "store", fmt.Sprintf("gagal membuat direktori sementara: %v", err), start)
		return
	}
	defer func() {
		_ = os.RemoveAll(jobTmpDir)
	}()

	wmAktaPath := filepath.Join(jobTmpDir, "wm_akta.pdf")
	mergedTempPath := filepath.Join(jobTmpDir, "merged.pdf")

	// Tahap 1: Watermark Akta
	wmOpts := wm.Options{
		FontSize:    wp.cfg.Watermark.FontSize,
		Opacity:     wp.cfg.Watermark.Opacity,
		Pos:         wp.cfg.Watermark.Pos,
		Rotation:    wp.cfg.Watermark.Rotation,
		Offset:      wp.cfg.Watermark.Offset,
		BorderWidth: wp.cfg.Watermark.BorderWidth,
		Radius:      wp.cfg.Watermark.Radius,
		Color:       wp.cfg.Watermark.Color,
	}
	identity := wm.Identity{
		NamaSatpen:   job.Data.NamaSatpen,
		NoRegistrasi: job.Data.NoRegistrasi,
		NPSN:         job.Data.NPSN,
	}

	if err := wm.Apply(job.AktaPath, wmAktaPath, identity, wmOpts); err != nil {
		wp.notifyFailure(ctx, job, "watermark", fmt.Sprintf("gagal memberi watermark akta: %v", err), start)
		return
	}

	// Tahap 2: Merge Piagam + SK + Akta (ber-watermark)
	var orderedPaths []string
	for _, doc := range wp.cfg.Merge.Order {
		switch doc {
		case "piagam":
			orderedPaths = append(orderedPaths, job.PiagamPath)
		case "sk":
			orderedPaths = append(orderedPaths, job.SKPath)
		case "akta":
			orderedPaths = append(orderedPaths, wmAktaPath)
		}
	}

	mergeOpts := merge.Options{
		DividerPage: wp.cfg.Merge.DividerPage,
	}
	if err := merge.Merge(orderedPaths, mergedTempPath, mergeOpts); err != nil {
		wp.notifyFailure(ctx, job, "merge", fmt.Sprintf("gagal menggabungkan dokumen: %v", err), start)
		return
	}

	// Tahap 3: Validasi Keluaran
	if err := merge.Validate(mergedTempPath); err != nil {
		wp.notifyFailure(ctx, job, "validate", fmt.Sprintf("validasi PDF hasil merge gagal: %v", err), start)
		return
	}

	// Tahap 4: Simpan ke output_path
	if err := os.MkdirAll(job.OutputPath, 0o755); err != nil {
		wp.notifyFailure(ctx, job, "store", fmt.Sprintf("gagal membuat output_path: %v", err), start)
		return
	}

	timestamp := time.Now().UTC().Format("20060102T150405Z")
	filename := fmt.Sprintf("%d_%s_akta.pdf", job.BHPNUID, timestamp)
	finalPath := filepath.Join(job.OutputPath, filename)

	if err := moveOrCopy(mergedTempPath, finalPath); err != nil {
		wp.notifyFailure(ctx, job, "store", fmt.Sprintf("gagal menyimpan file ke %s: %v", finalPath, err), start)
		return
	}

	durationMS := time.Since(start).Milliseconds()
	log.Printf("[worker-%d] Sukses menyelesaikan job bhpnu_id=%d dalam %d ms -> %s", workerID, job.BHPNUID, durationMS, finalPath)

	// Tahap 5: Kirim Webhook Sukses
	payload := WebhookPayload{
		BHPNUID:    job.BHPNUID,
		RequestID:  job.RequestID,
		Status:     "success",
		AktaPath:   filepath.ToSlash(finalPath),
		Out:        filepath.ToSlash(finalPath),
		Message:    "Watermark akta berhasil",
		DurationMS: durationMS,
		FinishedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := SendWebhook(ctx, &wp.cfg.Webhook, payload); err != nil {
		log.Printf("[ERROR] Webhook sukses gagal dikirim untuk job %d: %v", job.BHPNUID, err)
	}
}

func (wp *WorkerPool) notifyFailure(ctx context.Context, job Job, stage, errMsg string, start time.Time) {
	durationMS := time.Since(start).Milliseconds()
	log.Printf("[ERROR] Job bhpnu_id=%d gagal di tahap %s: %s", job.BHPNUID, stage, errMsg)

	payload := WebhookPayload{
		BHPNUID:    job.BHPNUID,
		RequestID:  job.RequestID,
		Status:     "failed",
		Message:    "Watermark akta gagal: " + errMsg,
		Error:      errMsg,
		Stage:      stage,
		DurationMS: durationMS,
		FinishedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := SendWebhook(ctx, &wp.cfg.Webhook, payload); err != nil {
		log.Printf("[ERROR] Webhook kegagalan gagal dikirim untuk job %d: %v", job.BHPNUID, err)
	}
}

func moveOrCopy(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	_ = in.Close()
	_ = out.Close()
	return os.Remove(src)
}

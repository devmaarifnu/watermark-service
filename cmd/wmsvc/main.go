package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	configPathFlag := flag.String("config", "", "path ke berkas config.yaml")
	flag.Parse()

	cfg, err := LoadConfig(*configPathFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: gagal memuat konfigurasi: %v\n", err)
		os.Exit(1)
	}

	if cfg.Auth.APIKey == "" {
		log.Println("[PERINGATAN] auth.api_key kosong: service berjalan tanpa autentikasi!")
	}

	pool := NewWorkerPool(cfg)
	pool.Start()

	srv := NewServer(cfg, pool)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/wm/akta", srv.handleAkta)
	mux.HandleFunc("/api/v1/webhook-test", func(w http.ResponseWriter, r *http.Request) {
		log.Println("[WEBHOOK-TEST] Berhasil menerima panggilan balik webhook!")
		w.WriteHeader(http.StatusOK)
	})

	httpServer := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Watermark Service (wmsvc) berjalan di %s ...", cfg.Server.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("Menerima sinyal berhenti, memulai proses graceful shutdown...")
	srv.SetShuttingDown(true)

	shutdownTimeout := cfg.Server.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = 30
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}

	log.Println("Menunggu seluruh worker menyelesaikan antrian job aktif...")
	pool.Stop()

	log.Println("Service wmsvc berhasil berhenti dengan rapi.")
}

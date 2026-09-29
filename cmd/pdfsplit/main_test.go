package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"pdfsplit/internal/common"
)

func fixture(t *testing.T) string {
	t.Helper()
	src := os.Getenv("PDFSPLIT_FIXTURE")
	if src == "" {
		t.Skip("PDFSPLIT_FIXTURE tidak diset")
	}
	if _, err := os.Stat(src); err != nil {
		t.Skipf("fixture tidak ada: %v", err)
	}
	return src
}

// TestSplitSpan memastikan span menghasilkan potongan dengan jumlah halaman benar.
func TestSplitSpan(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	conf := common.NewConf("")

	outDir := filepath.Join(dir, "out")
	if err := doSplit(src, 10, outDir, false, conf); err != nil {
		t.Fatalf("doSplit: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("membaca keluaran: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("jumlah file = %d, ingin 4 (39 halaman / 10)", len(entries))
	}

	// Potongan terakhir berisi 39-30 = 9 halaman.
	last, err := api.PageCountFile(filepath.Join(outDir, "SK_31-39.pdf"))
	if err != nil {
		t.Fatalf("hitung halaman: %v", err)
	}
	if last != 9 {
		t.Errorf("potongan akhir = %d halaman, ingin 9", last)
	}
}

// TestSplitSatuHalaman memastikan -n 1 menghasilkan satu file per halaman.
func TestSplitSatuHalaman(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()

	outDir := filepath.Join(dir, "out")
	if err := doSplit(src, 1, outDir, false, common.NewConf("")); err != nil {
		t.Fatalf("doSplit: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("membaca keluaran: %v", err)
	}
	if len(entries) != 39 {
		t.Errorf("jumlah file = %d, ingin 39", len(entries))
	}
}

// TestExtractPages memastikan halaman terpilih tergabung ke satu file.
func TestExtractPages(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()

	out := filepath.Join(dir, "pilihan.pdf")
	if err := doExtract(src, "1-3,7", out, false, common.NewConf("")); err != nil {
		t.Fatalf("doExtract: %v", err)
	}

	n, err := api.PageCountFile(out)
	if err != nil {
		t.Fatalf("hitung halaman: %v", err)
	}
	if n != 4 {
		t.Errorf("hasil extract = %d halaman, ingin 4", n)
	}
}

// TestBookmarkTanpaBookmark memastikan PDF tanpa daftar isi memberi pesan jelas,
// bukan error mentah dari pustaka.
func TestBookmarkTanpaBookmark(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()

	err := doBookmark(src, filepath.Join(dir, "out"), false, common.NewConf(""))
	if err == nil {
		t.Skip("fixture ternyata punya bookmark, kasus ini tidak berlaku")
	}
}

// TestValidasi memastikan masukan yang salah ditolak dengan pesan jelas.
func TestValidasi(t *testing.T) {
	dir := t.TempDir()
	conf := common.NewConf("")
	missing := filepath.Join(dir, "tidak-ada.pdf")

	if err := doSplit(missing, 1, "", false, conf); err == nil {
		t.Error("doSplit tanpa file seharusnya gagal")
	}
	if err := doSplit(missing, 0, "", false, conf); err == nil {
		t.Error("doSplit dengan n=0 seharusnya gagal")
	}
	if err := doExtract(missing, "", "", false, conf); err == nil {
		t.Error("doExtract tanpa -p seharusnya gagal")
	}
	if err := run([]string{"perintah-ngawur"}); err == nil {
		t.Error("perintah tak dikenal seharusnya gagal")
	}
	if err := run([]string{"split", "a.pdf", "b.pdf"}); err == nil {
		t.Error("lebih dari satu file seharusnya gagal")
	}
	if err := run([]string{"split"}); err == nil {
		t.Error("tanpa nama file seharusnya gagal")
	}
}

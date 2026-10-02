package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCSV membaca daftar satpen, termasuk deteksi header dan urutan kolom.
func TestCSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "satpen.csv")
	content := "sekolah,npsn,registrasi\n" +
		"SMA Negeri 1 Kediri,20512345,BHPNU/2026/001234\n" +
		"MTs NU Al-Hidayah,20567890,BHPNU/2026/005678\n" +
		"\n" +
		"SMP NU 2 Malang,20511111,BHPNU/2026/007777\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readSatpenCSV(p)
	if err != nil {
		t.Fatalf("readSatpenCSV: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("baris terbaca = %d, ingin 3 (header dan baris kosong dilewati)", len(got))
	}
	if got[0].NPSN != "20512345" {
		t.Errorf("NPSN baris 1 = %q, ingin 20512345", got[0].NPSN)
	}
	if got[0].Registrasi != "BHPNU/2026/001234" {
		t.Errorf("Registrasi baris 1 = %q", got[0].Registrasi)
	}
	if got[2].Sekolah != "SMP NU 2 Malang" {
		t.Errorf("Sekolah baris 3 = %q", got[2].Sekolah)
	}
}

// TestCSVTanpaHeader memastikan CSV tanpa header memakai urutan yang
// didokumentasikan: sekolah, registrasi, npsn.
func TestCSVTanpaHeader(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "polos.csv")
	content := "SMA Negeri 1 Kediri,BHPNU/2026/001234,20512345\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readSatpenCSV(p)
	if err != nil {
		t.Fatalf("readSatpenCSV: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("baris = %d, ingin 1", len(got))
	}
	if got[0].Sekolah != "SMA Negeri 1 Kediri" {
		t.Errorf("Sekolah = %q", got[0].Sekolah)
	}
	if got[0].Registrasi != "BHPNU/2026/001234" {
		t.Errorf("Registrasi = %q", got[0].Registrasi)
	}
	if got[0].NPSN != "20512345" {
		t.Errorf("NPSN = %q", got[0].NPSN)
	}
}

// TestValidasi memastikan data satpen yang salah ditolak lebih awal.
func TestValidasi(t *testing.T) {
	cases := []struct {
		nama string
		s    satpen
	}{
		{"semua kosong", satpen{}},
		{"sekolah kosong", satpen{NPSN: "20512345"}},
		{"npsn bukan angka", satpen{Sekolah: "A", NPSN: "abcdefgh"}},
		{"npsn kurang digit", satpen{Sekolah: "A", NPSN: "205123"}},
		{"npsn lebih digit", satpen{Sekolah: "A", NPSN: "205123456"}},
	}
	for _, c := range cases {
		if err := validateSatpen(c.s); err == nil {
			t.Errorf("%s: seharusnya ditolak", c.nama)
		}
	}

	valid := satpen{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "X/1", NPSN: "20512345"}
	if err := validateSatpen(valid); err != nil {
		t.Errorf("data valid ditolak: %v", err)
	}
}

// TestNamaFileAman memastikan nama sekolah dipakai di nama file tanpa karakter
// yang bermasalah di Windows.
func TestNamaFileAman(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SMA Negeri 1 Kediri / Filial: A*B?C", "SMA_Negeri_1_Kediri_Filial_A_B_C"},
		{"MTs  NU   Al-Hidayah", "MTs_NU_Al-Hidayah"},
		{"///", ""},
	}
	for _, c := range cases {
		if got := safeName(c.in); got != c.want {
			t.Errorf("safeName(%q) = %q, ingin %q", c.in, got, c.want)
		}
	}
}

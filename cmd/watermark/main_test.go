package main

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"pdfsplit/internal/common"
)

// posisiWatermark mencocokkan matriks penempatan form watermark pada halaman:
//
//	/Artifact <<...>>BDC q a b c d e f cm /GS0 gs /Fm0 Do Q EMC
var posisiWatermark = regexp.MustCompile(
	`/Subtype /Watermark[^>]*>>BDC q (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) cm`)

// fontUkuran mencocokkan ukuran font di dalam form watermark: "/F1 8 Tf".
var fontUkuran = regexp.MustCompile(`/F\d+ ([\d.]+) Tf`)

// fixture mengembalikan path PDF uji dari env, atau skip bila tidak diset.
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

// dumpPages membuang isi content stream semua halaman ke disk dan
// mengembalikan map nomor halaman -> isi stream.
//
// Catatan: watermark pdfcpu disimpan sebagai Form XObject yang dirujuk halaman
// lewat operator "Do", bukan sebagai teks inline. Karena itu penanda yang
// dicari di sini adalah rujukan "/Fm0 Do" dan blok "/Subtype /Watermark".
func dumpPages(t *testing.T, pdfPath string) map[int]string {
	t.Helper()

	dir := t.TempDir()
	if err := api.ExtractContentFile(pdfPath, dir, nil, common.NewConf("")); err != nil {
		t.Fatalf("extract content %s: %v", pdfPath, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("membaca hasil extract: %v", err)
	}

	out := map[int]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("membaca %s: %v", e.Name(), err)
		}
		n := pageFromDumpName(e.Name())
		if n > 0 {
			out[n] = string(b)
		}
	}
	return out
}

// pageFromDumpName mengambil nomor halaman dari nama file keluaran
// "xxx_Content_page_12.txt".
func pageFromDumpName(name string) int {
	const marker = "_page_"
	i := strings.LastIndex(name, marker)
	if i < 0 {
		return 0
	}
	s := strings.TrimSuffix(name[i+len(marker):], ".txt")
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// watermarkAda melaporkan apakah sebuah content stream memuat watermark.
func watermarkAda(content string) bool {
	return strings.Contains(content, "/Fm0 Do") ||
		strings.Contains(content, "/Subtype /Watermark")
}

// testOpts mengembalikan pilihan tampilan yang dipakai test, dengan batas
// lebar teks dihitung dari halaman terkecil dokumen uji.
func testOpts(src string) wmOpts {
	return wmOpts{
		Opacity:     0.18,
		FontSize:    8,
		Color:       "0.85 0 0",
		Pos:         "bl",
		Rotation:    0,
		Offset:      "18 18",
		BorderWidth: 1.0,
		Radius:      6.0,
		MaxWidth:    maxTextWidth(src),
	}
}

// TestWatermarkDiSetiapHalaman adalah inti keamanan fitur ini. Identitas satpen
// harus ada di SEMUA halaman, bukan hanya cover: kalau hanya cover, orang bisa
// split lalu ganti lembar depan dan identitasnya hilang.
//
// Dokumen uji adalah PDF hasil scan (satu gambar penuh per halaman), dan itu
// justru kasus terberat: watermark yang digambar di belakang gambar tidak akan
// terlihat sama sekali. Test ini memastikan watermark benar-benar tertanam di
// setiap halaman tanpa kecuali.
func TestWatermarkDiSetiapHalaman(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()

	out := filepath.Join(dir, "berwm.pdf")
	s := satpen{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := tempelWatermark(src, out, s, nil, testOpts(src)); err != nil {
		t.Fatalf("tempelWatermark: %v", err)
	}

	total, err := api.PageCountFile(out)
	if err != nil {
		t.Fatalf("jumlah halaman: %v", err)
	}
	if total != 39 {
		t.Fatalf("halaman = %d, ingin 39", total)
	}

	pages := dumpPages(t, out)
	if len(pages) != total {
		t.Fatalf("halaman terbaca dari dump = %d, ingin %d", len(pages), total)
	}

	// Setiap halaman harus memuat watermark. Tidak boleh ada satu pun yang lolos,
	// sebab satu halaman bersih sudah cukup untuk menyusun ulang dokumen tanpa identitas.
	var bersih []int
	for p := 1; p <= total; p++ {
		if !watermarkAda(pages[p]) {
			bersih = append(bersih, p)
		}
	}
	if len(bersih) > 0 {
		t.Errorf("watermark TIDAK ada di halaman: %v (dari %d halaman)", bersih, total)
	}
}

// TestWatermarkDiatasGambar memastikan watermark digambar setelah gambar scan.
// Kalau urutannya terbalik, watermark tertutup gambar penuh dan tidak terlihat,
// sehingga proteksinya hilang tanpa disadari.
func TestWatermarkDiatasGambar(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "urutan.pdf")

	s := satpen{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := tempelWatermark(src, out, s, nil, testOpts(src)); err != nil {
		t.Fatalf("tempelWatermark: %v", err)
	}

	pages := dumpPages(t, out)

	// Cari halaman yang memuat gambar penuh (operator "Do" pada XObject gambar)
	// dan pastikan rujukan watermark muncul SETELAH gambar itu.
	diperiksa := 0
	for p := 1; p <= len(pages); p++ {
		c := pages[p]
		wmIdx := strings.Index(c, "/Fm0 Do")
		if wmIdx < 0 {
			continue
		}
		imgIdx := strings.LastIndex(c[:wmIdx], " Do")
		if imgIdx < 0 {
			continue // halaman tanpa gambar, tidak perlu diuji urutannya
		}
		diperiksa++
		if wmIdx < imgIdx {
			t.Errorf("halaman %d: watermark digambar sebelum gambar, akan tertutup", p)
		}
	}
	if diperiksa == 0 {
		t.Error("tidak ada halaman bergambar yang bisa diperiksa urutannya")
	}
}

// TestCoverDilewati memastikan -cover benar-benar melewati halaman awal,
// dan halaman sesudahnya tetap ditempeli.
func TestCoverDilewati(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "cover.pdf")

	s := satpen{Sekolah: "MTs NU Al-Hidayah", Registrasi: "BHPNU/2026/009999", NPSN: "20567890"}
	if err := tempelWatermark(src, out, s, []string{"2-39"}, testOpts(src)); err != nil {
		t.Fatalf("tempelWatermark: %v", err)
	}

	pages := dumpPages(t, out)

	if watermarkAda(pages[1]) {
		t.Error("halaman 1 seharusnya dilewati tetapi ditempeli watermark")
	}
	if !watermarkAda(pages[2]) {
		t.Error("halaman 2 seharusnya ditempeli watermark tetapi tidak")
	}
	if !watermarkAda(pages[39]) {
		t.Error("halaman 39 seharusnya ditempeli watermark tetapi tidak")
	}
}

// TestTeksHorizontalDiKiriBawah memastikan watermark berbentuk teks horizontal
// kecil di kiri bawah halaman, bukan diagonal menimpa isi.
//
// Dua sifat yang dijaga:
//  1. Tidak ada rotasi — default pdfcpu adalah diagonal, jadi rot:0 harus
//     benar-benar diteruskan. Matriks form yang memuat komponen sinus tak nol
//     berarti teksnya miring atau terbalik.
//  2. Berada di kuadran kiri bawah — titik acuan form harus dekat sudut kiri
//     bawah halaman, bukan di tengah.
func TestTeksHorizontalDiKiriBawah(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "kiri-bawah.pdf")

	s := satpen{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := tempelWatermark(src, out, s, nil, testOpts(src)); err != nil {
		t.Fatalf("tempelWatermark: %v", err)
	}

	// Halaman 2 dan 39: keduanya tanpa /Rotate, jadi acuan posisinya lugas.
	pages := dumpPages(t, out)
	for _, p := range []int{2, 39} {
		c := pages[p]
		if !watermarkAda(c) {
			t.Fatalf("halaman %d tidak memuat watermark", p)
		}

		m := posisiWatermark.FindStringSubmatch(c)
		if m == nil {
			t.Fatalf("halaman %d: matriks penempatan watermark tidak ditemukan di: %s", p, c)
		}
		a, _ := strconv.ParseFloat(m[1], 64) // komponen x skala
		b, _ := strconv.ParseFloat(m[2], 64) // komponen y skala
		e, _ := strconv.ParseFloat(m[5], 64) // translasi x
		f, _ := strconv.ParseFloat(m[6], 64) // translasi y

		// Teks horizontal: tidak ada rotasi. Toleransi kecil untuk pembulatan.
		if math.Abs(b) > 0.01 || math.Abs(a-1) > 0.01 {
			t.Errorf("halaman %d: teks tidak horizontal (matriks %v %v)", p, a, b)
		}

		// Posisi: harus di paruh kiri dan paruh bawah halaman (A4 ~595x842 pt).
		if e > 595.0/2 || f > 842.0/2 {
			t.Errorf("halaman %d: watermark tidak di kiri bawah (x=%.2f y=%.2f)", p, e, f)
		}
		if e < 0 || f < 0 {
			t.Errorf("halaman %d: watermark keluar dari tepi halaman (x=%.2f y=%.2f)", p, e, f)
		}
	}
}

// TestTeksKecil memastikan ukuran teks benar-benar kecil. Ukuran absolut dipakai
// supaya tidak ikut membesar pada halaman berukuran besar.
func TestTeksKecil(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "kecil.pdf")

	s := satpen{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := tempelWatermark(src, out, s, nil, testOpts(src)); err != nil {
		t.Fatalf("tempelWatermark: %v", err)
	}

	form := watermarkFormContent(t, out)
	m := fontUkuran.FindStringSubmatch(form)
	if m == nil {
		t.Fatalf("ukuran font tidak ditemukan di form watermark")
	}
	fs, _ := strconv.ParseFloat(m[1], 64)

	// Ukuran dasar 8 pt; batas longgar sampai 12 untuk toleransi penskalaan.
	if fs <= 0 || fs > 12 {
		t.Errorf("ukuran font = %.2f pt, ingin kecil (<= 12)", fs)
	}
}

// TestIdentitasTerbaca memastikan teks identitas satpen benar-benar tersimpan
// di dalam berkas dan terbaca, bukan sekadar rujukan form kosong.
func TestIdentitasTerbaca(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "teks.pdf")

	s := satpen{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := tempelWatermark(src, out, s, nil, testOpts(src)); err != nil {
		t.Fatalf("tempelWatermark: %v", err)
	}

	form := watermarkFormContent(t, out)
	for _, want := range []string{"SMA Negeri 1 Kediri", "No. Registrasi: BHPNU/2026/001234", "NPSN: 20512345"} {
		// Teks PDF dibungkus tanda kurung oleh operator Tj.
		if !strings.Contains(form, "("+want+")") {
			t.Errorf("teks %q tidak ditemukan di form watermark", want)
		}
	}
}

// watermarkFormContent mengembalikan isi form XObject watermark (objek kecil
// yang memuat operator teks). Diperlukan karena teks watermark tidak berada di
// content stream halaman — halaman hanya merujuknya lewat operator "Do".
func watermarkFormContent(t *testing.T, pdfPath string) string {
	t.Helper()

	f, err := os.Open(pdfPath)
	if err != nil {
		t.Fatalf("membuka %s: %v", pdfPath, err)
	}
	defer f.Close()

	ctx, err := api.ReadContext(f, common.NewConf(""))
	if err != nil {
		t.Fatalf("membaca %s: %v", pdfPath, err)
	}

	var sb strings.Builder
	for _, e := range ctx.Table {
		sd, ok := e.Object.(types.StreamDict)
		if !ok {
			continue
		}
		if err := sd.Decode(); err != nil {
			continue
		}
		ct := string(sd.Content)
		// Form watermark berukuran kecil dan memuat operator teks; objek besar
		// adalah gambar hasil scan yang tidak relevan di sini.
		if strings.Contains(ct, "Tf") && len(ct) < 4000 {
			sb.WriteString(ct)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// TestKotakSudutTumpul memastikan teks dibungkus kotak yang sudutnya tidak lancip.
//
// Dua hal diperiksa:
//  1. Tidak ada lagi operator "re" (persegi siku) di form watermark — kotaknya
//     harus digambar sebagai path dengan kurva Bezier ("c").
//  2. Path itu memuat operator kurva, yang berarti sudutnya dibulatkan.
//
// Ini penting karena pdfcpu hanya menyediakan line join "round" yang radiusnya
// terbatas pada setengah lebar garis, sehingga pada border tipis hasilnya
// praktis masih siku.
func TestKotakSudutTumpul(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "tumpul.pdf")

	s := satpen{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := tempelWatermark(src, out, s, nil, testOpts(src)); err != nil {
		t.Fatalf("tempelWatermark: %v", err)
	}

	form := watermarkFormContent(t, out)

	if rectDiForm.MatchString(form) {
		t.Error("form watermark masih memakai persegi siku (operator 're')")
	}
	// Path bersudut bulat memakai operator kurva Bezier "c".
	if !strings.Contains(form, " c ") {
		t.Error("form watermark tidak memuat kurva, sudut tidak dibulatkan")
	}
}

// TestKotakTanpaRadius memastikan -radius 0 mempertahankan kotak siku, supaya
// pilihan itu tetap tersedia bila diinginkan.
func TestKotakTanpaRadius(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "siku.pdf")

	o := testOpts(src)
	o.Radius = 0

	s := satpen{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := tempelWatermark(src, out, s, nil, o); err != nil {
		t.Fatalf("tempelWatermark: %v", err)
	}

	form := watermarkFormContent(t, out)
	if !rectDiForm.MatchString(form) {
		t.Error("dengan radius 0, kotak seharusnya tetap siku (operator 're')")
	}
}

// TestKotakMembungkusTeks memastikan kotak benar-benar membungkus teks: lebar
// kotak harus melebihi lebar baris teks terpanjang, dan teks dimulai setelah
// tepi kiri kotak.
func TestKotakMembungkusTeks(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "bungkus.pdf")

	o := testOpts(src)
	s := satpen{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := tempelWatermark(src, out, s, nil, o); err != nil {
		t.Fatalf("tempelWatermark: %v", err)
	}

	form := watermarkFormContent(t, out)

	// Lebar kotak diambil dari bbox form.
	lebarKotak := bboxWidth(t, out)

	// Baris terpanjang: nomor registrasi.
	teks := "No. Registrasi: BHPNU/2026/001234"
	lebarTeks, err := font.TextWidthFloat(teks, "Helvetica-Bold", o.FontSize)
	if err != nil {
		t.Fatalf("mengukur lebar teks: %v", err)
	}

	if lebarKotak <= lebarTeks {
		t.Errorf("lebar kotak %.1f tidak melebihi lebar teks %.1f — teks tidak terbungkus",
			lebarKotak, lebarTeks)
	}
	// Margin kiri-kanan harus wajar, tidak berlebihan.
	if lebarKotak > lebarTeks+4*boxPadX {
		t.Errorf("lebar kotak %.1f terlalu longgar untuk teks %.1f", lebarKotak, lebarTeks)
	}

	// Teks harus digambar setelah tepi kiri kotak, bukan menempel di 0.
	m := teksPosisi.FindStringSubmatch(form)
	if m == nil {
		t.Fatalf("posisi teks tidak ditemukan di form")
	}
	x, _ := strconv.ParseFloat(m[1], 64)
	if x < boxPadX*0.5 {
		t.Errorf("teks dimulai di x=%.1f, terlalu mepet tepi kotak", x)
	}
}

// bboxWidth mengembalikan lebar bbox form watermark.
func bboxWidth(t *testing.T, pdfPath string) float64 {
	t.Helper()

	f, err := os.Open(pdfPath)
	if err != nil {
		t.Fatalf("membuka %s: %v", pdfPath, err)
	}
	defer f.Close()

	ctx, err := api.ReadContext(f, common.NewConf(""))
	if err != nil {
		t.Fatalf("membaca %s: %v", pdfPath, err)
	}

	for _, e := range ctx.Table {
		sd, ok := e.Object.(types.StreamDict)
		if !ok {
			continue
		}
		if err := sd.Decode(); err != nil {
			continue
		}
		if !strings.Contains(string(sd.Content), "Tf") || len(sd.Content) > batasUkuranForm {
			continue
		}
		bb, ok := sd.Dict["BBox"]
		if !ok {
			continue
		}
		arr, ok := bb.(types.Array)
		if !ok || len(arr) != 4 {
			continue
		}
		ll, _ := arr[0].(types.Float)
		ur, _ := arr[2].(types.Float)
		return float64(ur) - float64(ll)
	}
	t.Fatalf("bbox form watermark tidak ditemukan di %s", pdfPath)
	return 0
}

// teksPosisi mencocokkan posisi teks di dalam form: "8.00 31.04 Td".
var teksPosisi = regexp.MustCompile(`([\d.]+) [\d.]+ Td`)

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
	// Urutan kolom di CSV berbeda dari default, jadi harus terbaca via header.
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

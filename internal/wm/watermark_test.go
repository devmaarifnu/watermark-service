package wm

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

var posisiWatermark = regexp.MustCompile(
	`/Subtype /Watermark[^>]*>>BDC q (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) cm`)

var fontUkuran = regexp.MustCompile(`/F\d+ ([\d.]+) Tf`)
var teksPosisi = regexp.MustCompile(`([\d.]+) [\d.]+ Td`)

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

func TestApplyBasic(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pdf")
	out := filepath.Join(dir, "out.pdf")
	if err := os.WriteFile(in, []byte(minimalPDF), 0o644); err != nil {
		t.Fatal(err)
	}
	id := Identity{NamaSatpen: "SMK NU 1 Jakarta", NoRegistrasi: "123124", NPSN: "32423"}
	opts := Options{FontSize: 8, Opacity: 0.18, Pos: "bl", Offset: "18 18", BorderWidth: 1, Radius: 6}
	if err := Apply(in, out, id, opts); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	count, err := api.PageCountFile(out)
	if err != nil {
		t.Fatalf("PageCountFile failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 page, got %d", count)
	}
	form := watermarkFormContent(t, out)
	if !strings.Contains(form, "SMK NU 1 Jakarta") {
		t.Errorf("watermark form does not contain school name")
	}
}

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

func watermarkAda(content string) bool {
	return strings.Contains(content, "/Fm0 Do") ||
		strings.Contains(content, "/Subtype /Watermark")
}

func testOpts(src string) Options {
	return Options{
		Opacity:     0.18,
		FontSize:    8,
		Color:       "0.85 0 0",
		Pos:         "bl",
		Rotation:    0,
		Offset:      "18 18",
		BorderWidth: 1.0,
		Radius:      6.0,
	}
}

func TestWatermarkDiSetiapHalaman(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()

	out := filepath.Join(dir, "berwm.pdf")
	id := Identity{NamaSatpen: "SMA Negeri 1 Kediri", NoRegistrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := Apply(src, out, id, testOpts(src)); err != nil {
		t.Fatalf("Apply: %v", err)
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

func TestWatermarkDiatasGambar(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "urutan.pdf")

	id := Identity{NamaSatpen: "SMA Negeri 1 Kediri", NoRegistrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := Apply(src, out, id, testOpts(src)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pages := dumpPages(t, out)

	diperiksa := 0
	for p := 1; p <= len(pages); p++ {
		c := pages[p]
		wmIdx := strings.Index(c, "/Fm0 Do")
		if wmIdx < 0 {
			continue
		}
		imgIdx := strings.LastIndex(c[:wmIdx], " Do")
		if imgIdx < 0 {
			continue
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

func TestCoverDilewati(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "cover.pdf")

	id := Identity{NamaSatpen: "MTs NU Al-Hidayah", NoRegistrasi: "BHPNU/2026/009999", NPSN: "20567890"}
	if err := ApplyPages(src, out, id, testOpts(src), []string{"2-39"}); err != nil {
		t.Fatalf("ApplyPages: %v", err)
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

func TestTeksHorizontalDiKiriBawah(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "kiri-bawah.pdf")

	id := Identity{NamaSatpen: "SMA Negeri 1 Kediri", NoRegistrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := Apply(src, out, id, testOpts(src)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

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
		a, _ := strconv.ParseFloat(m[1], 64)
		b, _ := strconv.ParseFloat(m[2], 64)
		e, _ := strconv.ParseFloat(m[5], 64)
		f, _ := strconv.ParseFloat(m[6], 64)

		if math.Abs(b) > 0.01 || math.Abs(a-1) > 0.01 {
			t.Errorf("halaman %d: teks tidak horizontal (matriks %v %v)", p, a, b)
		}

		if e > 595.0/2 || f > 842.0/2 {
			t.Errorf("halaman %d: watermark tidak di kiri bawah (x=%.2f y=%.2f)", p, e, f)
		}
		if e < 0 || f < 0 {
			t.Errorf("halaman %d: watermark keluar dari tepi halaman (x=%.2f y=%.2f)", p, e, f)
		}
	}
}

func TestTeksKecil(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "kecil.pdf")

	id := Identity{NamaSatpen: "SMA Negeri 1 Kediri", NoRegistrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := Apply(src, out, id, testOpts(src)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	form := watermarkFormContent(t, out)
	m := fontUkuran.FindStringSubmatch(form)
	if m == nil {
		t.Fatalf("ukuran font tidak ditemukan di form watermark")
	}
	fs, _ := strconv.ParseFloat(m[1], 64)

	if fs <= 0 || fs > 12 {
		t.Errorf("ukuran font = %.2f pt, ingin kecil (<= 12)", fs)
	}
}

func TestIdentitasTerbaca(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "teks.pdf")

	id := Identity{NamaSatpen: "SMA Negeri 1 Kediri", NoRegistrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := Apply(src, out, id, testOpts(src)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	form := watermarkFormContent(t, out)
	for _, want := range []string{"SMA Negeri 1 Kediri", "No. Registrasi: BHPNU/2026/001234", "NPSN: 20512345"} {
		if !strings.Contains(form, "("+want+")") {
			t.Errorf("teks %q tidak ditemukan di form watermark", want)
		}
	}
}

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
		if strings.Contains(ct, "Tf") && len(ct) < 4000 {
			sb.WriteString(ct)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func TestKotakSudutTumpul(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "tumpul.pdf")

	id := Identity{NamaSatpen: "SMA Negeri 1 Kediri", NoRegistrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := Apply(src, out, id, testOpts(src)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	form := watermarkFormContent(t, out)

	if rectDiForm.MatchString(form) {
		t.Error("form watermark masih memakai persegi siku (operator 're')")
	}
	if !strings.Contains(form, " c ") {
		t.Error("form watermark tidak memuat kurva, sudut tidak dibulatkan")
	}
}

func TestKotakTanpaRadius(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "siku.pdf")

	o := testOpts(src)
	o.Radius = 0

	id := Identity{NamaSatpen: "SMA Negeri 1 Kediri", NoRegistrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := Apply(src, out, id, o); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	form := watermarkFormContent(t, out)
	if !rectDiForm.MatchString(form) {
		t.Error("dengan radius 0, kotak seharusnya tetap siku (operator 're')")
	}
}

func TestKotakMembungkusTeks(t *testing.T) {
	src := fixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "bungkus.pdf")

	o := testOpts(src)
	id := Identity{NamaSatpen: "SMA Negeri 1 Kediri", NoRegistrasi: "BHPNU/2026/001234", NPSN: "20512345"}
	if err := Apply(src, out, id, o); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	form := watermarkFormContent(t, out)
	lebarKotak := bboxWidth(t, out)

	teks := "No. Registrasi: BHPNU/2026/001234"
	lebarTeks, err := font.TextWidthFloat(teks, "Helvetica-Bold", o.FontSize)
	if err != nil {
		t.Fatalf("mengukur lebar teks: %v", err)
	}

	if lebarKotak <= lebarTeks {
		t.Errorf("lebar kotak %.1f tidak melebihi lebar teks %.1f — teks tidak terbungkus",
			lebarKotak, lebarTeks)
	}
	if lebarKotak > lebarTeks+4*boxPadX {
		t.Errorf("lebar kotak %.1f terlalu longgar untuk teks %.1f", lebarKotak, lebarTeks)
	}

	m := teksPosisi.FindStringSubmatch(form)
	if m == nil {
		t.Fatalf("posisi teks tidak ditemukan di form")
	}
	x, _ := strconv.ParseFloat(m[1], 64)
	if x < boxPadX*0.5 {
		t.Errorf("teks dimulai di x=%.1f, terlalu mepet tepi kotak", x)
	}
}

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

// Command watermark menempelkan identitas satuan pendidikan di setiap halaman PDF.
//
// Latar belakang: dokumen SK bersifat general. Bila satu satpen mendapatkannya
// lalu membagikannya ke satpen lain, dokumen itu tetap harus bisa dibedakan
// milik siapa. Karena itu identitas ditempel di semua halaman, bukan hanya
// halaman cover — supaya trik "split lalu ganti lembar depan" tidak berguna:
// halaman hasil split tetap membawa identitas satpen asalnya.
//
// Pemakaian:
//
//	watermark -in SK.pdf -out hasil.pdf -sekolah "SMA Negeri 1 Kediri" \
//	          -reg "BHPNU/2026/001234" -npsn 20512345
//
//	watermark -in SK.pdf -csv satpen.csv -outdir hasil
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"pdfsplit/internal/common"
)

const wmUsage = `watermark - tempel identitas satuan pendidikan di setiap halaman PDF

Penggunaan:
  watermark -in <SK.pdf> -sekolah <nama> -reg <nomor> -npsn <npsn> [-out <hasil.pdf>]
  watermark -in <SK.pdf> -csv <satpen.csv> -outdir <folder>

Mode satu satpen (opsi -sekolah/-reg/-npsn):
  -in string        PDF sumber yang akan ditempeli watermark.
  -sekolah string   Nama satuan pendidikan.
  -reg string       Nomor registrasi satpen.
  -npsn string      NPSN satpen.
  -out string       Nama file keluaran. Default: <nama-sumber>_<npsn>.pdf

Mode batch (opsi -csv):
  -csv string       CSV berisi kolom: sekolah,registrasi,npsn
                    (baris pertama dideteksi otomatis sebagai header atau bukan).
  -outdir string    Folder keluaran untuk mode batch. Default: <nama-sumber>_watermark

Tampilan:
  -fontsize float    Ukuran teks dalam point (default 8). Ukuran ini absolut,
                     tidak berubah mengikuti ukuran halaman. Nama sekolah yang
                     terlalu panjang dikecilkan otomatis agar muat.
  -pos string        Posisi: tl tc tr l c r bl bc br (default bl = kiri bawah).
  -rot float         Rotasi teks dalam derajat (default 0 = horizontal).
  -offset string     Jarak dari tepi halaman dalam point, "x y" (default "18 18").
  -opacity float     Transparansi, 0.0-1.0 (default 0.18).
  -color string      Warna teks dan garis kotak: "#RRGGBB" atau "r g b"
                     0.0-1.0 (default "0.85 0 0").
  -border float      Tebal garis kotak pembungkus dalam point (default 1).
  -radius float      Radius sudut kotak dalam point (default 6). Isi 0 untuk
                     kotak bersiku. Radius otomatis dibatasi bila kotak kecil.
  -cover int         Jumlah halaman awal yang TIDAK ditempeli (default 0 = semua).
                     Pakai 1 bila halaman pertama sampul. Ingat: halaman yang
                     dilewati menjadi celah untuk di-split tanpa identitas.
  -dryrun            Hanya tampilkan rencana, tanpa menulis file.
  -v                 Tampilkan log pdfcpu.

Tanpa -sekolah/-csv, tool memakai 2 satpen contoh supaya alur kerja bisa dilihat.

Contoh:
  watermark -in SK.pdf -sekolah "SMA Negeri 1 Kediri" -reg "BHPNU/2026/001234" -npsn 20512345
  watermark -in SK.pdf -csv satpen.csv -outdir hasil -fontsize 7 -pos br
`

func main() {
	if err := runWM(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "watermark: %v\n", err)
		os.Exit(1)
	}
}

// satpen adalah identitas satu satuan pendidikan.
type satpen struct {
	Sekolah    string
	Registrasi string
	NPSN       string
}

// Jarak tepi kotak terhadap teks, dalam point.
const (
	boxPadX = 7.0
	boxPadY = 9.0
)

// wmOpts menampung pilihan tampilan watermark.
type wmOpts struct {
	Opacity     float64
	FontSize    float64
	Color       string
	Pos         string
	Rotation    float64
	Offset      string
	BorderWidth float64
	Radius      float64 // radius sudut kotak; 0 = tanpa pembulatan
	MaxWidth    float64 // lebar maksimum teks dalam point; 0 = tanpa batas
}

func runWM(args []string) error {
	if len(args) == 0 {
		fmt.Print(wmUsage)
		return nil
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(wmUsage)
		return nil
	}

	fs := flag.NewFlagSet("watermark", flag.ContinueOnError)
	var (
		inFile   = fs.String("in", "", "PDF sumber")
		outFile  = fs.String("out", "", "PDF keluaran (mode satu satpen)")
		outDir   = fs.String("outdir", "", "folder keluaran (mode batch)")
		sekolah  = fs.String("sekolah", "", "nama satuan pendidikan")
		reg      = fs.String("reg", "", "nomor registrasi")
		npsn     = fs.String("npsn", "", "NPSN")
		csvPath  = fs.String("csv", "", "CSV berisi daftar satpen")
		fontSize = fs.Float64("fontsize", 8, "ukuran teks (point)")
		pos      = fs.String("pos", "bl", "posisi teks pada halaman")
		rot      = fs.Float64("rot", 0, "rotasi teks (derajat)")
		offset   = fs.String("offset", "18 18", "jarak dari tepi halaman (point)")
		opacity  = fs.Float64("opacity", 0.48, "transparansi watermark")
		colStr   = fs.String("color", "0.85 0 0", "warna teks")
		border   = fs.Float64("border", 1.0, "tebal garis kotak (point)")
		radius   = fs.Float64("radius", 6.0, "radius sudut kotak (point); 0 = siku")
		cover    = fs.Int("cover", 0, "jumlah halaman awal tanpa watermark")
		dryRun   = fs.Bool("dryrun", false, "hanya tampilkan rencana")
		verbose  = fs.Bool("v", false, "log pdfcpu")
	)
	fs.Usage = func() { fmt.Print(wmUsage) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("argumen tak terduga %q, gunakan opsi (lihat: watermark -h)", fs.Arg(0))
	}

	if *inFile == "" {
		return fmt.Errorf("opsi -in wajib diisi (lihat: watermark -h)")
	}
	if err := common.CheckInput(*inFile); err != nil {
		return err
	}
	if *opacity < 0 || *opacity > 1 {
		return fmt.Errorf("nilai -opacity harus 0.0-1.0, diberikan %v", *opacity)
	}
	if *fontSize <= 0 {
		return fmt.Errorf("nilai -fontsize harus > 0, diberikan %v", *fontSize)
	}
	if *rot < -180 || *rot > 180 {
		return fmt.Errorf("nilai -rot harus -180..180, diberikan %v", *rot)
	}
	if *cover < 0 {
		return fmt.Errorf("nilai -cover tidak boleh negatif, diberikan %d", *cover)
	}

	common.EnableLog(*verbose)

	// Tentukan daftar satpen: satu dari opsi, atau banyak dari CSV.
	var batch []satpen
	switch {
	case *csvPath != "":
		if *sekolah != "" || *npsn != "" || *reg != "" {
			return fmt.Errorf("mode -csv tidak bisa digabung dengan -sekolah/-reg/-npsn")
		}
		var err error
		batch, err = readSatpenCSV(*csvPath)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return fmt.Errorf("CSV %s tidak berisi data satpen", *csvPath)
		}
	case *sekolah != "" || *npsn != "" || *reg != "":
		batch = []satpen{{Sekolah: *sekolah, Registrasi: *reg, NPSN: *npsn}}
	default:
		// Tanpa data satpen: pakai contoh supaya alur kerja bisa langsung dilihat.
		batch = contohSatpen()
		fmt.Fprintln(os.Stderr, "catatan: -sekolah/-reg/-npsn tidak diisi, memakai 2 satpen contoh.")
		fmt.Fprintln(os.Stderr)
	}

	// Validasi seluruh data sebelum menyentuh file apa pun.
	for i, s := range batch {
		if err := validateSatpen(s); err != nil {
			return fmt.Errorf("satpen ke-%d: %w", i+1, err)
		}
	}

	total, err := api.PageCountFile(*inFile)
	if err != nil {
		return fmt.Errorf("membaca jumlah halaman %s: %w", *inFile, err)
	}
	if *cover >= total {
		return fmt.Errorf("-cover %d menghabiskan seluruh %d halaman, tidak ada yang ditempeli", *cover, total)
	}

	o := wmOpts{
		Opacity:     *opacity,
		FontSize:    *fontSize,
		Color:       *colStr,
		Pos:         *pos,
		Rotation:    *rot,
		Offset:      *offset,
		BorderWidth: *border,
		Radius:      *radius,
		MaxWidth:    maxTextWidth(*inFile),
	}

	// Halaman yang ditempeli: semua kecuali -cover halaman awal.
	var pages []string
	if *cover > 0 {
		pages = []string{fmt.Sprintf("%d-%d", *cover+1, total)}
	}

	base := strings.TrimSuffix(filepath.Base(*inFile), filepath.Ext(*inFile))

	// Mode batch tanpa -outdir: buat folder di samping file sumber.
	targetDir := *outDir
	if targetDir == "" && *csvPath != "" {
		targetDir = filepath.Join(filepath.Dir(*inFile), base+"_watermark")
	}

	if *dryRun {
		fmt.Printf("PDF sumber : %s (%d halaman)\n", *inFile, total)
		if *cover > 0 {
			fmt.Printf("Dilewati   : %d halaman awal (perhatian: ini celah untuk di-split)\n", *cover)
		}
		fmt.Printf("Watermark  : %v pt, posisi %q, rotasi %v, opasitas %v, warna %q\n",
			o.FontSize, o.Pos, o.Rotation, o.Opacity, o.Color)
		fmt.Println()
		for _, s := range batch {
			fmt.Printf("  %-40s -> %s\n", s.Sekolah, tujuanKeluaran(targetDir, *outFile, base, s))
		}
		return nil
	}

	if targetDir != "" {
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return fmt.Errorf("membuat folder %s: %w", targetDir, err)
		}
	}

	// Satu satpen: tulis ke -out atau nama turunan di samping file sumber.
	if len(batch) == 1 && targetDir == "" {
		out := *outFile
		if out == "" {
			out = filepath.Join(filepath.Dir(*inFile), fmt.Sprintf("%s_%s.pdf", base, safeName(batch[0].NPSN+"_"+batch[0].Sekolah)))
		}
		if err := tempelWatermark(*inFile, out, batch[0], pages, o); err != nil {
			return err
		}
		fmt.Printf("Selesai: %s\n", out)
		return nil
	}

	// Mode batch.
	for i, s := range batch {
		out := tujuanKeluaran(targetDir, "", base, s)
		if err := tempelWatermark(*inFile, out, s, pages, o); err != nil {
			return fmt.Errorf("satpen ke-%d (%s): %w", i+1, s.Sekolah, err)
		}
		fmt.Printf("[%d/%d] %s\n", i+1, len(batch), filepath.Base(out))
	}
	fmt.Printf("Selesai: %d file di %s\n", len(batch), targetDir)
	return nil
}

// maxTextWidth menghitung lebar maksimum teks agar tidak melewati tepi halaman.
// Dipakai lebar halaman terkecil di dokumen, karena satu PDF bisa memuat
// halaman dengan ukuran berbeda.
func maxTextWidth(inFile string) float64 {
	dims, err := api.PageDimsFile(inFile)
	if err != nil || len(dims) == 0 {
		return 0 // tanpa data, biarkan tanpa batas
	}
	minW := dims[0].Width
	for _, d := range dims {
		if d.Width > 0 && d.Width < minW {
			minW = d.Width
		}
	}
	return 0.90 * minW
}

// tempelWatermark menulis satu PDF keluaran dengan identitas satpen di setiap halaman.
//
// Alur ini memakai context pdfcpu secara langsung, bukan api.AddWatermarksFile,
// karena kotak pembungkus perlu ditambal menjadi bersudut bulat setelah pdfcpu
// membuat form watermark. Menulis ulang lewat context menjaga seluruh objek
// asli dokumen tetap utuh.
func tempelWatermark(inFile, outFile string, s satpen, pages []string, o wmOpts) error {
	// Teks multi-baris: nama sekolah, nomor registrasi, lalu NPSN.
	// "\n" diubah pdfcpu menjadi baris baru.
	var lines []string
	if s.Sekolah != "" {
		lines = append(lines, s.Sekolah)
	}
	if s.Registrasi != "" {
		lines = append(lines, "No. Registrasi: "+s.Registrasi)
	}
	if s.NPSN != "" {
		lines = append(lines, "NPSN: "+s.NPSN)
	}
	text := strings.Join(lines, "\\n")

	const fontName = "Helvetica-Bold"

	// Ukuran teks dipakai apa adanya sebagai point (skala absolut), bukan
	// skala relatif terhadap halaman — supaya teks tetap kecil dan seragam
	// di halaman berukuran apa pun. Baris terpanjang dikecilkan hanya bila
	// memang melebihi lebar yang tersedia.
	fs := int(math.Round(o.FontSize))
	if fs < 1 {
		fs = 1
	}
	if o.MaxWidth > 0 {
		longest := ""
		for _, l := range lines {
			if len(l) > len(longest) {
				longest = l
			}
		}
		if w, err := font.TextWidthFloat(longest, fontName, float64(fs)); err == nil && w > o.MaxWidth {
			fs = int(math.Round(float64(fs) * o.MaxWidth / w))
			if fs < 4 {
				fs = 4
			}
		}
	}

	// rot:0 memastikan teks horizontal. Ini wajib ditulis eksplisit: default
	// pdfcpu adalah diagonal, sehingga tanpa ini teks akan miring.
	//
	// bgcolor + border mengaktifkan penggambaran kotak lewat TextDescriptor,
	// yang menyediakan kotak sesuai ukuran teks secara otomatis. "round" di
	// situ hanya membulatkan sebesar setengah lebar garis, jadi sudut yang
	// benar-benar tumpul dipasang belakangan lewat patchSudutBulat.
	desc := fmt.Sprintf(
		"pos:%s, rot:%.1f, op:%.3f, points:%d, scalefactor:1 abs, offset:%s, "+
			"margins:%.1f %.1f, border:%.2f round %s, bgcolor:1 1 1, fontname:%s, color:%s",
		o.Pos, o.Rotation, o.Opacity, fs, o.Offset,
		boxPadY, boxPadX, o.BorderWidth, o.Color, fontName, o.Color)

	// onTop=true (stamp) WAJIB untuk dokumen hasil scan. SK seperti ini
	// umumnya berupa gambar penuh satu halaman; kalau watermark digambar di
	// belakang gambar tersebut, ia tidak akan terlihat sama sekali dan
	// proteksinya hilang tanpa disadari.
	wm, err := pdfcpu.ParseTextWatermarkDetails(text, desc, true, types.POINTS)
	if err != nil {
		return fmt.Errorf("menyusun watermark: %w", err)
	}

	f, err := os.Open(inFile)
	if err != nil {
		return fmt.Errorf("membuka %s: %w", inFile, err)
	}
	defer f.Close()

	conf := common.NewConf("")
	conf.Cmd = model.ADDWATERMARKS

	ctx, err := api.ReadValidateAndOptimize(f, conf)
	if err != nil {
		return fmt.Errorf("membaca %s: %w", inFile, err)
	}

	if err := pdfcpu.AddWatermarks(ctx, halamanTerpilih(ctx.PageCount, pages), wm); err != nil {
		return fmt.Errorf("menempelkan watermark: %w", err)
	}

	if o.Radius > 0 {
		if err := patchSudutBulat(ctx, o.Radius); err != nil {
			return fmt.Errorf("membulatkan sudut kotak: %w", err)
		}
	}

	out, err := os.Create(outFile)
	if err != nil {
		return fmt.Errorf("membuat %s: %w", outFile, err)
	}
	defer out.Close()
	if err := api.WriteContext(ctx, out); err != nil {
		return fmt.Errorf("menulis %s: %w", outFile, err)
	}
	return nil
}

// halamanTerpilih mengubah pilihan halaman berbentuk string menjadi IntSet.
// Daftar kosong berarti semua halaman.
func halamanTerpilih(pageCount int, pages []string) types.IntSet {
	if len(pages) == 0 {
		return nil
	}
	// Format yang dipakai pemanggil: satu rentang "awal-akhir".
	set := types.IntSet{}
	for _, p := range pages {
		awal, akhir, ok := strings.Cut(p, "-")
		if !ok {
			continue
		}
		a, err1 := strconv.Atoi(strings.TrimSpace(awal))
		b, err2 := strconv.Atoi(strings.TrimSpace(akhir))
		if err1 != nil || err2 != nil {
			continue
		}
		for i := a; i <= b && i <= pageCount; i++ {
			set[i] = true
		}
	}
	return set
}

// Persegi siku pada form watermark: "... 0.50 0.50 155.05 43.56 re B ..."
var rectDiForm = regexp.MustCompile(`(-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) re B`)

// batasUkuranForm memisahkan form watermark dari gambar hasil scan. Form
// watermark hanya beberapa ratus byte, sedangkan gambar scan berukuran
// ratusan kilobyte.
const batasUkuranForm = 4000

// patchSudutBulat mengganti persegi siku pada kotak watermark dengan path
// bersudut bulat memakai kurva Bezier.
//
// Ini diperlukan karena pdfcpu hanya menyediakan line join "round", yang
// radius pembulatannya terbatas pada setengah lebar garis — pada border 1 pt
// hasilnya praktis masih siku.
func patchSudutBulat(ctx *model.Context, radius float64) error {
	for _, ent := range ctx.Table {
		sd, ok := ent.Object.(types.StreamDict)
		if !ok {
			continue
		}
		if err := sd.Decode(); err != nil {
			continue
		}
		ct := string(sd.Content)
		// Form watermark memuat operator teks dan berukuran kecil.
		if !strings.Contains(ct, "Tf") || len(ct) > batasUkuranForm {
			continue
		}
		loc := rectDiForm.FindStringSubmatchIndex(ct)
		if loc == nil {
			continue // bukan kotak watermark
		}

		m := rectDiForm.FindStringSubmatch(ct)
		x, _ := strconv.ParseFloat(m[1], 64)
		y, _ := strconv.ParseFloat(m[2], 64)
		w, _ := strconv.ParseFloat(m[3], 64)
		h, _ := strconv.ParseFloat(m[4], 64)

		// Radius tidak boleh melebihi setengah sisi terpendek.
		r := radius
		if r > w/2 {
			r = w / 2
		}
		if r > h/2 {
			r = h / 2
		}

		baru := []byte(ct[:loc[0]] + pathSudutBulat(x, y, w, h, r) + ct[loc[1]:])

		// Raw dan StreamLength harus konsisten: penulis PDF memakai sd.Raw
		// dan memverifikasi panjangnya terhadap StreamLength. Isi yang sudah
		// didekode ditulis apa adanya, jadi filter dilepas.
		sd.Content = baru
		sd.Raw = baru
		n := int64(len(baru))
		sd.StreamLength = &n
		sd.Dict.Update("Length", types.Integer(n))
		sd.FilterPipeline = nil
		delete(sd.Dict, "Filter")

		// StreamDict disimpan by value, jadi hasil modifikasi harus ditulis
		// kembali ke entri tabel agar ikut tersimpan.
		ent.Object = sd
		return nil
	}
	return fmt.Errorf("kotak watermark tidak ditemukan")
}

// pathSudutBulat menyusun path persegi bersudut bulat ber-radius r.
func pathSudutBulat(x, y, w, h, r float64) string {
	// Konstanta Bezier untuk aproksimasi seperempat lingkaran.
	const k = 0.5522847498307933
	c := k * r

	var b strings.Builder
	fmt.Fprintf(&b, "%.2f %.2f m ", x+r, y)       // mulai sisi bawah
	fmt.Fprintf(&b, "%.2f %.2f l ", x+w-r, y)     // sisi bawah
	fmt.Fprintf(&b, "%.2f %.2f %.2f %.2f %.2f %.2f c ", x+w-r+c, y, x+w, y+r-c, x+w, y+r)
	fmt.Fprintf(&b, "%.2f %.2f l ", x+w, y+h-r)   // sisi kanan
	fmt.Fprintf(&b, "%.2f %.2f %.2f %.2f %.2f %.2f c ", x+w, y+h-r+c, x+w-r+c, y+h, x+w-r, y+h)
	fmt.Fprintf(&b, "%.2f %.2f l ", x+r, y+h)     // sisi atas
	fmt.Fprintf(&b, "%.2f %.2f %.2f %.2f %.2f %.2f c ", x+r-c, y+h, x, y+h-r+c, x, y+h-r)
	fmt.Fprintf(&b, "%.2f %.2f l ", x, y+r)       // sisi kiri
	fmt.Fprintf(&b, "%.2f %.2f %.2f %.2f %.2f %.2f c ", x, y+r-c, x+r-c, y, x+r, y)
	b.WriteString("B ")
	return b.String()
}

func tujuanKeluaran(outDir, explicit, base string, s satpen) string {
	if explicit != "" && outDir == "" {
		return explicit
	}
	label := s.NPSN
	if label == "" {
		label = safeName(s.Sekolah)
	}
	if label == "" {
		label = "satpen"
	}
	return filepath.Join(outDir, fmt.Sprintf("%s_%s.pdf", base, label))
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_') // spasi dan tanda baca menjadi pemisah
		}
	}
	// Rapikan pemisah beruntun: "a / b" -> "a_b", bukan "a___b".
	return strings.Join(strings.FieldsFunc(b.String(), func(r rune) bool { return r == '_' }), "_")
}

func validateSatpen(s satpen) error {
	if s.Sekolah == "" && s.NPSN == "" && s.Registrasi == "" {
		return fmt.Errorf("nama sekolah, nomor registrasi, dan NPSN kosong semua")
	}
	if s.Sekolah == "" {
		return fmt.Errorf("nama sekolah kosong")
	}
	if s.NPSN != "" {
		if _, err := strconv.Atoi(strings.TrimSpace(s.NPSN)); err != nil {
			return fmt.Errorf("NPSN %q bukan angka (seharusnya 8 digit angka)", s.NPSN)
		}
		if len(strings.TrimSpace(s.NPSN)) != 8 {
			return fmt.Errorf("NPSN %q panjangnya %d digit, seharusnya 8", s.NPSN, len(strings.TrimSpace(s.NPSN)))
		}
	}
	return nil
}

// readSatpenCSV membaca CSV dengan kolom sekolah, registrasi, npsn.
// Urutan dan penamaan kolom dideteksi dari baris header bila ada.
func readSatpenCSV(path string) ([]satpen, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("membuka CSV: %w", err)
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("membaca CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("CSV kosong")
	}

	// Tanpa header, urutan kolom mengikuti format yang didokumentasikan:
	// sekolah, registrasi, npsn.
	idxSekolah, idxReg, idxNPSN := 0, 1, 2
	start := 0

	head := strings.ToLower(strings.Join(rows[0], " "))
	if strings.Contains(head, "sekolah") || strings.Contains(head, "satpen") ||
		strings.Contains(head, "npsn") || strings.Contains(head, "registrasi") {
		start = 1
		for i, c := range rows[0] {
			c = strings.ToLower(strings.TrimSpace(c))
			switch {
			case strings.Contains(c, "npsn"):
				idxNPSN = i
			case strings.Contains(c, "registrasi"), strings.Contains(c, "reg"):
				idxReg = i
			case strings.Contains(c, "sekolah"), strings.Contains(c, "satpen"), strings.Contains(c, "nama"):
				idxSekolah = i
			}
		}
	}

	var out []satpen
	for n := start; n < len(rows); n++ {
		row := rows[n]
		if len(row) == 0 {
			continue
		}
		get := func(i int) string {
			if i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		s := satpen{Sekolah: get(idxSekolah), Registrasi: get(idxReg), NPSN: get(idxNPSN)}
		if s.Sekolah == "" && s.NPSN == "" && s.Registrasi == "" {
			continue // lewati baris kosong
		}
		out = append(out, s)
	}
	return out, nil
}

// contohSatpen dipakai bila tidak ada data satpen yang diberikan, supaya alur
// kerja bisa langsung dilihat tanpa harus mengisi opsi.
func contohSatpen() []satpen {
	return []satpen{
		{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "BHPNU/2026/001234", NPSN: "20512345"},
		{Sekolah: "MTs NU Al-Hidayah Surabaya", Registrasi: "BHPNU/2026/005678", NPSN: "20567890"},
	}
}

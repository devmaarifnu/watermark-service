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
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"pdfsplit/internal/common"
	"pdfsplit/internal/wm"
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
		batch = contohSatpen()
		fmt.Fprintln(os.Stderr, "catatan: -sekolah/-reg/-npsn tidak diisi, memakai 2 satpen contoh.")
		fmt.Fprintln(os.Stderr)
	}

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

	o := wm.Options{
		Opacity:     *opacity,
		FontSize:    *fontSize,
		Color:       *colStr,
		Pos:         *pos,
		Rotation:    *rot,
		Offset:      *offset,
		BorderWidth: *border,
		Radius:      *radius,
	}

	var pages []string
	if *cover > 0 {
		pages = []string{fmt.Sprintf("%d-%d", *cover+1, total)}
	}

	base := strings.TrimSuffix(filepath.Base(*inFile), filepath.Ext(*inFile))

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

	if len(batch) == 1 && targetDir == "" {
		out := *outFile
		if out == "" {
			out = filepath.Join(filepath.Dir(*inFile), fmt.Sprintf("%s_%s.pdf", base, safeName(batch[0].NPSN+"_"+batch[0].Sekolah)))
		}
		id := wm.Identity{NamaSatpen: batch[0].Sekolah, NoRegistrasi: batch[0].Registrasi, NPSN: batch[0].NPSN}
		if err := wm.ApplyPages(*inFile, out, id, o, pages); err != nil {
			return err
		}
		fmt.Printf("Selesai: %s\n", out)
		return nil
	}

	for i, s := range batch {
		out := tujuanKeluaran(targetDir, "", base, s)
		id := wm.Identity{NamaSatpen: s.Sekolah, NoRegistrasi: s.Registrasi, NPSN: s.NPSN}
		if err := wm.ApplyPages(*inFile, out, id, o, pages); err != nil {
			return fmt.Errorf("satpen ke-%d (%s): %w", i+1, s.Sekolah, err)
		}
		fmt.Printf("[%d/%d] %s\n", i+1, len(batch), filepath.Base(out))
	}
	fmt.Printf("Selesai: %d file di %s\n", len(batch), targetDir)
	return nil
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
			b.WriteRune('_')
		}
	}
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
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func contohSatpen() []satpen {
	return []satpen{
		{Sekolah: "SMA Negeri 1 Kediri", Registrasi: "BHPNU/2026/001234", NPSN: "20512345"},
		{Sekolah: "MTs NU Al-Hidayah Surabaya", Registrasi: "BHPNU/2026/005678", NPSN: "20567890"},
	}
}

// Command pdfsplit memecah satu file PDF menjadi beberapa file PDF.
//
// Mode yang tersedia:
//
//	pdfsplit split <file.pdf>               satu file per halaman
//	pdfsplit split -n 5 <file.pdf>          satu file tiap 5 halaman
//	pdfsplit bookmark <file.pdf>            pecah mengikuti daftar isi (bookmark)
//	pdfsplit extract -p 1-3,7 <file.pdf>    ambil hanya halaman tertentu
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"pdfsplit/internal/common"
)

const usage = `pdfsplit - pecah file PDF menjadi beberapa bagian

Penggunaan:
  pdfsplit split    [-n N] [opsi] <file.pdf>
  pdfsplit bookmark [-o DIR] [opsi] <file.pdf>
  pdfsplit extract  -p HALAMAN [-o FILE] [opsi] <file.pdf>

Perintah:
  split     Pecah menjadi potongan berisi N halaman. Default N=1 (satu file per halaman).
            Contoh: pdfsplit split -n 5 dokumen.pdf -> potongan 5 halaman
  bookmark  Pecah mengikuti bookmark/daftar isi level 1 di dalam PDF.
  extract   Gabungkan halaman terpilih menjadi SATU file baru.

Opsi:
  -n int      Jumlah halaman per file untuk perintah split (default 1).
  -p string   Pilihan halaman untuk perintah extract, contoh: "1-3,7,10" atau "2-".
  -o string   Tujuan. Untuk split/bookmark: direktori keluaran.
              Untuk extract: nama file keluaran.
  -pw string  Password PDF bila file terkunci.
  -v          Tampilkan log proses dari pdfcpu.
  -h          Tampilkan bantuan ini.

Contoh:
  pdfsplit split laporan.pdf
  pdfsplit split -n 10 -o hasil laporan.pdf
  pdfsplit bookmark -o per_bab laporan.pdf
  pdfsplit extract -p 1-5 -o ringkasan.pdf laporan.pdf
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "pdfsplit: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	cmd := args[0]
	switch cmd {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	}

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	var (
		span    = fs.Int("n", 1, "jumlah halaman per file (split)")
		pages   = fs.String("p", "", "pilihan halaman (extract), contoh 1-3,7,10")
		out     = fs.String("o", "", "direktori keluaran (split/bookmark) atau nama file (extract)")
		pw      = fs.String("pw", "", "password PDF bila terkunci")
		verbose = fs.Bool("v", false, "tampilkan log pdfcpu")
	)
	fs.Usage = func() { fmt.Print(usage) }

	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("nama file PDF belum diberikan (lihat: pdfsplit -h)")
	}
	if len(rest) > 1 {
		return fmt.Errorf("hanya menerima satu file PDF, tetapi diberi %d", len(rest))
	}
	inFile := rest[0]

	conf := common.NewConf(*pw)

	switch cmd {
	case "split":
		return doSplit(inFile, *span, *out, *verbose, conf)
	case "bookmark":
		return doBookmark(inFile, *out, *verbose, conf)
	case "extract":
		return doExtract(inFile, *pages, *out, *verbose, conf)
	default:
		return fmt.Errorf("perintah %q tidak dikenal (lihat: pdfsplit -h)", cmd)
	}
}

// doSplit memecah inFile menjadi potongan berisi span halaman tiap file.
func doSplit(inFile string, span int, outDir string, verbose bool, conf *model.Configuration) error {
	if span < 1 {
		return fmt.Errorf("nilai -n harus minimal 1, diberikan %d", span)
	}
	if err := common.CheckInput(inFile); err != nil {
		return err
	}

	total, err := api.PageCountFile(inFile)
	if err != nil {
		return fmt.Errorf("membaca jumlah halaman: %w", err)
	}

	outDir, err = prepareOutDir(inFile, outDir)
	if err != nil {
		return err
	}
	common.EnableLog(verbose)

	// span=1 berarti satu file per halaman, pdfcpu menangani keduanya.
	if err := api.SplitFile(inFile, outDir, span, conf); err != nil {
		return err
	}

	n := (total + span - 1) / span
	fmt.Printf("Selesai: %s (%d halaman) -> %d file di %s\n", filepath.Base(inFile), total, n, outDir)
	return nil
}

// doBookmark memecah inFile mengikuti bookmark level 1.
func doBookmark(inFile string, outDir string, verbose bool, conf *model.Configuration) error {
	if err := common.CheckInput(inFile); err != nil {
		return err
	}

	outDir, err := prepareOutDir(inFile, outDir)
	if err != nil {
		return err
	}
	common.EnableLog(verbose)

	// span=0 memerintahkan pdfcpu memecah mengikuti bookmark.
	if err := api.SplitFile(inFile, outDir, 0, conf); err != nil {
		return fmt.Errorf("%w (pastikan PDF memiliki bookmark/daftar isi)", err)
	}

	fmt.Printf("Selesai: %s dipecah mengikuti bookmark ke %s\n", filepath.Base(inFile), outDir)
	return nil
}

// doExtract menggabungkan halaman terpilih menjadi satu file keluaran.
func doExtract(inFile, pageSel, outFile string, verbose bool, conf *model.Configuration) error {
	if strings.TrimSpace(pageSel) == "" {
		return fmt.Errorf("pilihan halaman belum diberikan, contoh: -p 1-3,7,10")
	}
	if err := common.CheckInput(inFile); err != nil {
		return err
	}

	sel := strings.Split(pageSel, ",")
	for i := range sel {
		sel[i] = strings.TrimSpace(sel[i])
	}

	if outFile == "" {
		base := strings.TrimSuffix(filepath.Base(inFile), filepath.Ext(inFile))
		outFile = filepath.Join(filepath.Dir(inFile), fmt.Sprintf("%s_hal%s.pdf", base, pageSel))
	}

	common.EnableLog(verbose)

	if err := api.TrimFile(inFile, outFile, sel, conf); err != nil {
		return err
	}

	fmt.Printf("Selesai: halaman %s dari %s -> %s\n", pageSel, filepath.Base(inFile), outFile)
	return nil
}

// prepareOutDir menentukan direktori keluaran dan membuatnya bila belum ada.
// Bila outDir kosong, dipakai "<nama-file>_pecahan" di samping file asal.
func prepareOutDir(inFile, outDir string) (string, error) {
	if outDir == "" {
		base := strings.TrimSuffix(filepath.Base(inFile), filepath.Ext(inFile))
		outDir = filepath.Join(filepath.Dir(inFile), base+"_pecahan")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("membuat direktori %s: %w", outDir, err)
	}
	return outDir, nil
}

package wm

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"pdfsplit/internal/common"
)

// Options menampung konfigurasi visual watermark.
type Options struct {
	FontSize    float64
	Opacity     float64
	Pos         string
	Rotation    float64
	Offset      string
	BorderWidth float64
	Radius      float64
	Color       string
}

// Identity menampung data satuan pendidikan yang ditempelkan ke Akta.
type Identity struct {
	NamaSatpen   string
	NoRegistrasi string
	NPSN         string
}

// Jarak tepi kotak terhadap teks, dalam point.
const (
	boxPadX = 7.0
	boxPadY = 9.0
)

// Apply menempelkan watermark identitas ke seluruh halaman dokumen PDF.
func Apply(inFile, outFile string, id Identity, o Options) error {
	return ApplyPages(inFile, outFile, id, o, nil)
}

// ApplyPages menempelkan watermark identitas ke halaman tertentu (atau semua jika pages kosong).
func ApplyPages(inFile, outFile string, id Identity, o Options, pages []string) error {
	if o.FontSize <= 0 {
		o.FontSize = 8
	}
	if o.Opacity <= 0 {
		o.Opacity = 0.18
	}
	if o.Pos == "" {
		o.Pos = "bl"
	}
	if o.Offset == "" {
		o.Offset = "18 18"
	}
	if o.Color == "" {
		o.Color = "0.85 0 0"
	}
	if o.BorderWidth <= 0 {
		o.BorderWidth = 1.0
	}

	maxWidth := maxTextWidth(inFile)

	var lines []string
	if id.NamaSatpen != "" {
		lines = append(lines, id.NamaSatpen)
	}
	if id.NoRegistrasi != "" {
		lines = append(lines, "No. Registrasi: "+id.NoRegistrasi)
	}
	if id.NPSN != "" {
		lines = append(lines, "NPSN: "+id.NPSN)
	}
	text := strings.Join(lines, "\\n")

	const fontName = "Helvetica-Bold"

	fs := int(math.Round(o.FontSize))
	if fs < 1 {
		fs = 1
	}
	if maxWidth > 0 {
		longest := ""
		for _, l := range lines {
			if len(l) > len(longest) {
				longest = l
			}
		}
		if w, err := font.TextWidthFloat(longest, fontName, float64(fs)); err == nil && w > maxWidth {
			fs = int(math.Round(float64(fs) * maxWidth / w))
			if fs < 4 {
				fs = 4
			}
		}
	}

	desc := fmt.Sprintf(
		"pos:%s, rot:%.1f, op:%.3f, points:%d, scalefactor:1 abs, offset:%s, "+
			"margins:%.1f %.1f, border:%.2f round %s, bgcolor:1 1 1, fontname:%s, color:%s",
		o.Pos, o.Rotation, o.Opacity, fs, o.Offset,
		boxPadY, boxPadX, o.BorderWidth, o.Color, fontName, o.Color)

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

// maxTextWidth menghitung lebar maksimum teks agar tidak melewati tepi halaman.
func maxTextWidth(inFile string) float64 {
	dims, err := api.PageDimsFile(inFile)
	if err != nil || len(dims) == 0 {
		return 0
	}
	minW := dims[0].Width
	for _, d := range dims {
		if d.Width > 0 && d.Width < minW {
			minW = d.Width
		}
	}
	return 0.90 * minW
}

// halamanTerpilih mengubah pilihan halaman berbentuk string menjadi IntSet.
func halamanTerpilih(pageCount int, pages []string) types.IntSet {
	if len(pages) == 0 {
		return nil
	}
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

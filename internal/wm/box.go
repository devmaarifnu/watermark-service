package wm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

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

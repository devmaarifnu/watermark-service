package merge

import (
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"pdfsplit/internal/common"
)

// Options menampung opsi penggabungan file PDF.
type Options struct {
	DividerPage bool
}

// Merge menggabungkan file-file PDF pada inFiles berurutan ke outFile.
func Merge(inFiles []string, outFile string, opts Options) error {
	if len(inFiles) == 0 {
		return fmt.Errorf("tidak ada berkas untuk digabungkan")
	}
	conf := common.NewConf("")
	if err := api.MergeCreateFile(inFiles, outFile, opts.DividerPage, conf); err != nil {
		return fmt.Errorf("gagal menggabungkan berkas PDF: %w", err)
	}
	return nil
}

// Validate memeriksa keabsahan berkas PDF keluaran.
func Validate(pdfPath string) error {
	conf := common.NewConf("")
	if err := api.ValidateFile(pdfPath, conf); err != nil {
		return fmt.Errorf("validasi PDF gagal untuk %s: %w", pdfPath, err)
	}
	return nil
}

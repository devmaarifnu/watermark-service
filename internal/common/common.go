// Package common berisi helper yang dipakai bersama oleh tool pdfsplit dan watermark.
package common

import (
	"fmt"
	"os"

	plog "github.com/pdfcpu/pdfcpu/pkg/log"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// CheckInput memastikan path yang diberikan benar-benar ada dan bukan direktori.
func CheckInput(inFile string) error {
	info, err := os.Stat(inFile)
	if err != nil {
		return fmt.Errorf("membuka %s: %w", inFile, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s adalah direktori, bukan file PDF", inFile)
	}
	return nil
}

// NewConf menyiapkan konfigurasi pdfcpu: mode validasi longgar (banyak PDF
// Indonesia tidak sepenuhnya patuh ISO-32000) dan password opsional.
func NewConf(password string) *model.Configuration {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	conf.UserPW = password
	conf.OwnerPW = password
	return conf
}

// EnableLog menyalakan log pdfcpu. Secara default log pdfcpu sudah mati,
// jadi cukup dinyalakan saat opsi -v dipakai.
func EnableLog(verbose bool) {
	if verbose {
		plog.SetDefaultLoggers()
	}
}

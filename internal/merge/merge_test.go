package merge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

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

func createDummyPDF(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(minimalPDF), 0o644); err != nil {
		t.Fatalf("failed to write dummy pdf: %v", err)
	}
}

func TestMergeAndValidate(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "doc1.pdf")
	p2 := filepath.Join(dir, "doc2.pdf")
	out := filepath.Join(dir, "merged.pdf")

	createDummyPDF(t, p1)
	createDummyPDF(t, p2)

	if err := Merge([]string{p1, p2}, out, Options{DividerPage: false}); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	if err := Validate(out); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	count, err := api.PageCountFile(out)
	if err != nil {
		t.Fatalf("PageCountFile failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 pages, got %d", count)
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigSuccess(t *testing.T) {
	dir := t.TempDir()
	inDir := filepath.Join(dir, "src")
	outDir := filepath.Join(dir, "out")
	tmpDir := filepath.Join(dir, "tmp")
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
server:
  addr: ":8081"
webhook:
  url: "https://example.com/webhook"
  secret: "secret123"
auth:
  api_key: "key123"
storage:
  allowed_input_base:
    - "` + filepath.ToSlash(inDir) + `"
  allowed_output_base: "` + filepath.ToSlash(outDir) + `"
  tmp_dir: "` + filepath.ToSlash(tmpDir) + `"
worker:
  count: 3
  queue_size: 20
watermark:
  opacity: 0.25
log:
  level: "debug"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Server.Addr != ":8081" {
		t.Errorf("Server.Addr = %s, want :8081", cfg.Server.Addr)
	}
	if cfg.Worker.Count != 3 {
		t.Errorf("Worker.Count = %d, want 3", cfg.Worker.Count)
	}
	if cfg.Watermark.Opacity != 0.25 {
		t.Errorf("Watermark.Opacity = %f, want 0.25", cfg.Watermark.Opacity)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %s, want debug", cfg.Log.Level)
	}
}

func TestConfigValidationFailures(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")

	tests := []struct {
		name    string
		content string
	}{
		{
			name: "missing webhook url",
			content: `
storage:
  allowed_output_base: "` + filepath.ToSlash(outDir) + `"
security:
  allow_any_path: true
`,
		},
		{
			name: "missing allowed_output_base",
			content: `
webhook:
  url: "https://example.com"
security:
  allow_any_path: true
`,
		},
		{
			name: "invalid opacity",
			content: `
webhook:
  url: "https://example.com"
storage:
  allowed_output_base: "` + filepath.ToSlash(outDir) + `"
security:
  allow_any_path: true
watermark:
  opacity: 1.5
`,
		},
		{
			name: "invalid merge order",
			content: `
webhook:
  url: "https://example.com"
storage:
  allowed_output_base: "` + filepath.ToSlash(outDir) + `"
security:
  allow_any_path: true
merge:
  order: ["piagam", "unknown_doc"]
`,
		},
		{
			name: "invalid log level",
			content: `
webhook:
  url: "https://example.com"
storage:
  allowed_output_base: "` + filepath.ToSlash(outDir) + `"
security:
  allow_any_path: true
log:
  level: "verbose"
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name+".yaml")
			if err := os.WriteFile(p, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(p)
			if err == nil {
				t.Errorf("expected error for case %q, got nil", tc.name)
			}
		})
	}
}

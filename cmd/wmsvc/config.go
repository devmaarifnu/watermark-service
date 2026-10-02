package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type StringOrSlice []string

func (s *StringOrSlice) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		var str string
		if err := value.Decode(&str); err != nil {
			return err
		}
		str = strings.TrimSpace(str)
		if str != "" {
			*s = []string{str}
		} else {
			*s = []string{}
		}
		return nil
	}
	if value.Kind == yaml.SequenceNode {
		var slice []string
		if err := value.Decode(&slice); err != nil {
			return err
		}
		var cleaned []string
		for _, item := range slice {
			item = strings.TrimSpace(item)
			if item != "" {
				cleaned = append(cleaned, item)
			}
		}
		*s = cleaned
		return nil
	}
	return fmt.Errorf("harus berupa string atau array string")
}

type ServerConfig struct {
	Addr            string        `yaml:"addr"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type WebhookConfig struct {
	URL         StringOrSlice `yaml:"url"`
	URLs        StringOrSlice `yaml:"urls,omitempty"`
	Secret      string        `yaml:"secret"`
	Timeout     time.Duration `yaml:"timeout"`
	MaxAttempts int           `yaml:"max_attempts"`
	BackoffBase time.Duration `yaml:"backoff_base"`
}

type AuthConfig struct {
	APIKey string `yaml:"api_key"`
}

type StorageConfig struct {
	AllowedInputBase   StringOrSlice `yaml:"allowed_input_base"`
	AllowedOutputBase  StringOrSlice `yaml:"allowed_output_base"`
	AllowedOutputBases StringOrSlice `yaml:"allowed_output_bases,omitempty"`
	TmpDir             string        `yaml:"tmp_dir"`
	CreateOutputDir    bool          `yaml:"create_output_dir"`
	Overwrite          bool          `yaml:"overwrite"`
}

type SecurityConfig struct {
	AllowAnyPath bool `yaml:"allow_any_path"`
}

type WorkerConfig struct {
	Count      int           `yaml:"count"`
	QueueSize  int           `yaml:"queue_size"`
	JobTimeout time.Duration `yaml:"job_timeout"`
}

type LimitsConfig struct {
	MaxPageCount  int   `yaml:"max_page_count"`
	MaxFileSizeMB int64 `yaml:"max_file_size_mb"`
}

type WatermarkConfig struct {
	FontSize    float64 `yaml:"font_size"`
	Opacity     float64 `yaml:"opacity"`
	Pos         string  `yaml:"pos"`
	Rotation    float64 `yaml:"rotation"`
	Offset      string  `yaml:"offset"`
	BorderWidth float64 `yaml:"border_width"`
	Radius      float64 `yaml:"radius"`
	Color       string  `yaml:"color"`
}

type MergeConfig struct {
	Order       []string `yaml:"order"`
	DividerPage bool     `yaml:"divider_page"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Webhook   WebhookConfig   `yaml:"webhook"`
	Auth      AuthConfig      `yaml:"auth"`
	Storage   StorageConfig   `yaml:"storage"`
	Security  SecurityConfig  `yaml:"security"`
	Worker    WorkerConfig    `yaml:"worker"`
	Limits    LimitsConfig    `yaml:"limits"`
	Watermark WatermarkConfig `yaml:"watermark"`
	Merge     MergeConfig     `yaml:"merge"`
	Log       LogConfig       `yaml:"log"`
}

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Addr:            ":8080",
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 30 * time.Second,
		},
		Webhook: WebhookConfig{
			Timeout:     10 * time.Second,
			MaxAttempts: 5,
			BackoffBase: 1 * time.Second,
		},
		Storage: StorageConfig{
			TmpDir:          filepath.Join(os.TempDir(), "wmsvc"),
			CreateOutputDir: true,
			Overwrite:       false,
		},
		Security: SecurityConfig{
			AllowAnyPath: false,
		},
		Worker: WorkerConfig{
			Count:      2,
			QueueSize:  16,
			JobTimeout: 120 * time.Second,
		},
		Limits: LimitsConfig{
			MaxPageCount:  500,
			MaxFileSizeMB: 100,
		},
		Watermark: WatermarkConfig{
			FontSize:    8,
			Opacity:     0.18,
			Pos:         "bl",
			Rotation:    0,
			Offset:      "18 18",
			BorderWidth: 1.0,
			Radius:      6.0,
			Color:       "0.85 0 0",
		},
		Merge: MergeConfig{
			Order:       []string{"piagam", "sk", "akta"},
			DividerPage: false,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

// LoadConfig memuat konfigurasi dari file dan env, lalu memvalidasinya.
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = os.Getenv("CONFIG_PATH")
		if configPath == "" {
			configPath = "./config.yaml"
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("config.yaml tidak ditemukan (%s): %w", configPath, err)
	}

	cfg := defaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("format YAML config tidak valid: %w", err)
	}

	// Environment variable overrides
	if envURL := os.Getenv("WEBHOOK_URL"); envURL != "" {
		parts := strings.FieldsFunc(envURL, func(r rune) bool {
			return r == ',' || r == ';'
		})
		var urls []string
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				urls = append(urls, s)
			}
		}
		if len(urls) > 0 {
			cfg.Webhook.URL = urls
		}
	}
	if envURLs := os.Getenv("WEBHOOK_URLS"); envURLs != "" {
		parts := strings.FieldsFunc(envURLs, func(r rune) bool {
			return r == ',' || r == ';'
		})
		var urls []string
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				urls = append(urls, s)
			}
		}
		if len(urls) > 0 {
			cfg.Webhook.URL = urls
		}
	}
	if envSecret := os.Getenv("WEBHOOK_SECRET"); envSecret != "" {
		cfg.Webhook.Secret = envSecret
	}
	if envAPIKey := os.Getenv("API_KEY"); envAPIKey != "" {
		cfg.Auth.APIKey = envAPIKey
	}
	if envLogLevel := os.Getenv("LOG_LEVEL"); envLogLevel != "" {
		cfg.Log.Level = envLogLevel
	}
	if envOut := os.Getenv("ALLOWED_OUTPUT_BASE"); envOut != "" {
		parts := strings.FieldsFunc(envOut, func(r rune) bool {
			return r == ',' || r == ';'
		})
		var bases []string
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				bases = append(bases, s)
			}
		}
		if len(bases) > 0 {
			cfg.Storage.AllowedOutputBase = bases
		}
	}
	if envOutBases := os.Getenv("ALLOWED_OUTPUT_BASES"); envOutBases != "" {
		parts := strings.FieldsFunc(envOutBases, func(r rune) bool {
			return r == ',' || r == ';'
		})
		var bases []string
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				bases = append(bases, s)
			}
		}
		if len(bases) > 0 {
			cfg.Storage.AllowedOutputBase = bases
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validasi konfigurasi gagal: %w", err)
	}

	return &cfg, nil
}

func (c *Config) normalize() {
	if len(c.Webhook.URLs) > 0 {
		c.Webhook.URL = append(c.Webhook.URL, c.Webhook.URLs...)
		c.Webhook.URLs = nil
	}
	if len(c.Storage.AllowedOutputBases) > 0 {
		c.Storage.AllowedOutputBase = append(c.Storage.AllowedOutputBase, c.Storage.AllowedOutputBases...)
		c.Storage.AllowedOutputBases = nil
	}
}

// Validate memeriksa kelayakan konfigurasi saat service dinyalakan (§8.4).
func (c *Config) Validate() error {
	c.normalize()

	if len(c.Webhook.URL) == 0 {
		return fmt.Errorf("webhook.url kosong")
	}
	for _, u := range c.Webhook.URL {
		if strings.TrimSpace(u) == "" {
			return fmt.Errorf("webhook.url memuat URL kosong")
		}
	}

	if len(c.Storage.AllowedOutputBase) == 0 {
		return fmt.Errorf("storage.allowed_output_base kosong")
	}

	// Cek / buat direktori allowed_output_base
	for _, outBase := range c.Storage.AllowedOutputBase {
		cleanOut := filepath.Clean(outBase)
		if _, err := os.Stat(cleanOut); err != nil {
			if os.IsNotExist(err) {
				if err := os.MkdirAll(cleanOut, 0o755); err != nil {
					if !c.Security.AllowAnyPath {
						return fmt.Errorf("storage.allowed_output_base (%s) tidak ada dan gagal dibuat: %w", outBase, err)
					}
				}
			} else if !c.Security.AllowAnyPath {
				return fmt.Errorf("storage.allowed_output_base (%s) bermasalah: %w", outBase, err)
			}
		}
	}

	// Cek / buat direktori allowed_input_base bila allow_any_path false
	if !c.Security.AllowAnyPath {
		if len(c.Storage.AllowedInputBase) == 0 {
			return fmt.Errorf("storage.allowed_input_base kosong (kecuali security.allow_any_path: true)")
		}
		for _, inBase := range c.Storage.AllowedInputBase {
			cleanIn := filepath.Clean(inBase)
			if _, err := os.Stat(cleanIn); err != nil {
				if os.IsNotExist(err) {
					if err := os.MkdirAll(cleanIn, 0o755); err != nil {
						return fmt.Errorf("direktori input %s tidak ada dan gagal dibuat: %w", inBase, err)
					}
				} else {
					return fmt.Errorf("direktori input %s bermasalah: %w", inBase, err)
				}
			}
		}
	}

	// Tmp dir
	if c.Storage.TmpDir != "" {
		if err := os.MkdirAll(filepath.Clean(c.Storage.TmpDir), 0o755); err != nil {
			return fmt.Errorf("gagal membuat storage.tmp_dir (%s): %w", c.Storage.TmpDir, err)
		}
	}

	if c.Worker.Count < 1 {
		return fmt.Errorf("worker.count < 1 (minimal 1)")
	}
	if c.Worker.QueueSize < 1 {
		return fmt.Errorf("worker.queue_size < 1 (minimal 1)")
	}

	if c.Watermark.Opacity < 0.0 || c.Watermark.Opacity > 1.0 {
		return fmt.Errorf("watermark.opacity di luar rentang 0.0-1.0: %v", c.Watermark.Opacity)
	}

	if len(c.Merge.Order) == 0 {
		c.Merge.Order = []string{"piagam", "sk", "akta"}
	}
	for _, o := range c.Merge.Order {
		switch o {
		case "piagam", "sk", "akta":
		default:
			return fmt.Errorf("merge.order memuat nama selain piagam, sk, akta: %q", o)
		}
	}

	level := strings.ToLower(strings.TrimSpace(c.Log.Level))
	switch level {
	case "debug", "info", "warn", "error":
		c.Log.Level = level
	default:
		return fmt.Errorf("log.level bukan salah satu dari debug, info, warn, error: %q", c.Log.Level)
	}

	return nil
}

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFromPath_BlockList(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultFileName)
	content := `bootstrap_servers: kafka-a:9092,kafka-b:9092
auth_mechanism: SCRAM-SHA-512
exclude_topics:
  - "__*"
  - "*.dlq"
exclude_internal: true
format: json
timeout: 30s
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}

	if cfg.BootstrapServers != "kafka-a:9092,kafka-b:9092" {
		t.Fatalf("bootstrap_servers = %q", cfg.BootstrapServers)
	}
	if cfg.AuthMechanism != "SCRAM-SHA-512" {
		t.Fatalf("auth_mechanism = %q", cfg.AuthMechanism)
	}
	if len(cfg.ExcludeTopics) != 2 || cfg.ExcludeTopics[0] != "__*" || cfg.ExcludeTopics[1] != "*.dlq" {
		t.Fatalf("exclude_topics = %#v", cfg.ExcludeTopics)
	}
	if cfg.ExcludeInternal == nil || !*cfg.ExcludeInternal {
		t.Fatalf("exclude_internal = %v", cfg.ExcludeInternal)
	}
	if cfg.Format != "json" {
		t.Fatalf("format = %q", cfg.Format)
	}
	if !cfg.HasTimeout || cfg.Timeout != 30*time.Second {
		t.Fatalf("timeout = %v (has=%t)", cfg.Timeout, cfg.HasTimeout)
	}
}

func TestLoadFromPath_InlineList(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultFileName)
	content := `exclude_topics: ["__*", "*.retry", "orders,archive"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}

	if len(cfg.ExcludeTopics) != 3 {
		t.Fatalf("exclude_topics length = %d, want 3", len(cfg.ExcludeTopics))
	}
	if cfg.ExcludeTopics[2] != "orders,archive" {
		t.Fatalf("exclude_topics[2] = %q, want %q", cfg.ExcludeTopics[2], "orders,archive")
	}
}

func TestLoad_AutoDiscovery(t *testing.T) {
	cwdDir := filepath.Join(t.TempDir(), "cwd")
	if err := os.MkdirAll(cwdDir, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	homeDir := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	cwdConfig := filepath.Join(cwdDir, DefaultFileName)
	homeConfig := filepath.Join(homeDir, DefaultFileName)

	if err := os.WriteFile(cwdConfig, []byte("bootstrap_servers: cwd:9092\n"), 0o644); err != nil {
		t.Fatalf("write cwd config: %v", err)
	}
	if err := os.WriteFile(homeConfig, []byte("bootstrap_servers: home:9092\n"), 0o644); err != nil {
		t.Fatalf("write home config: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore wd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(cwdDir); err != nil {
		t.Fatalf("Chdir(cwd): %v", err)
	}
	t.Setenv("HOME", homeDir)

	cfg, path, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg == nil {
		t.Fatalf("Load() cfg is nil")
	}
	if !samePath(path, cwdConfig) {
		t.Fatalf("loaded path = %q, want %q", path, cwdConfig)
	}
	if cfg.BootstrapServers != "cwd:9092" {
		t.Fatalf("bootstrap_servers = %q, want %q", cfg.BootstrapServers, "cwd:9092")
	}
}

func TestLoad_AutoDiscoveryHomeFallback(t *testing.T) {
	cwdDir := t.TempDir()
	homeDir := t.TempDir()
	homeConfig := filepath.Join(homeDir, DefaultFileName)
	if err := os.WriteFile(homeConfig, []byte("bootstrap_servers: home:9092\n"), 0o644); err != nil {
		t.Fatalf("write home config: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore wd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(cwdDir); err != nil {
		t.Fatalf("Chdir(cwd): %v", err)
	}
	t.Setenv("HOME", homeDir)

	cfg, path, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg == nil {
		t.Fatalf("Load() cfg is nil")
	}
	if !samePath(path, homeConfig) {
		t.Fatalf("loaded path = %q, want %q", path, homeConfig)
	}
}

func TestLoad_NoFile(t *testing.T) {
	cwdDir := t.TempDir()
	homeDir := t.TempDir()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore wd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(cwdDir); err != nil {
		t.Fatalf("Chdir(cwd): %v", err)
	}
	t.Setenv("HOME", homeDir)

	cfg, path, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg != nil || path != "" {
		t.Fatalf("Load() = (%v, %q), want (nil, \"\")", cfg, path)
	}
}

func TestLoadFromPath_Errors(t *testing.T) {
	tempDir := t.TempDir()

	unknownKey := filepath.Join(tempDir, "unknown.yaml")
	if err := os.WriteFile(unknownKey, []byte("unknown: value\n"), 0o644); err != nil {
		t.Fatalf("write unknown config: %v", err)
	}
	if _, err := LoadFromPath(unknownKey); err == nil {
		t.Fatalf("expected error for unknown key")
	}

	badTimeout := filepath.Join(tempDir, "bad-timeout.yaml")
	if err := os.WriteFile(badTimeout, []byte("timeout: soon\n"), 0o644); err != nil {
		t.Fatalf("write timeout config: %v", err)
	}
	if _, err := LoadFromPath(badTimeout); err == nil {
		t.Fatalf("expected error for invalid timeout")
	}
}

func samePath(left, right string) bool {
	leftResolved, leftErr := filepath.EvalSymlinks(left)
	rightResolved, rightErr := filepath.EvalSymlinks(right)
	if leftErr == nil && rightErr == nil {
		return leftResolved == rightResolved
	}

	return filepath.Clean(left) == filepath.Clean(right)
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadShorthandGroups(t *testing.T) {
	cfg := loadTestConfig(t, `
id: sample
groups:
  core: '^BenchmarkCore'
  storage:
    title: Local storage
    match:
      - '^BenchmarkGlobal'
exclude: '^BenchmarkCoreSlow$'
`)
	if cfg.SitePath != "go-benchmarks/sample" || cfg.Title != "sample benchmarks" {
		t.Fatalf("defaults = %+v", cfg)
	}
	if !cfg.IncludeBenchmark("BenchmarkCoreFast", "BenchmarkCoreFast") {
		t.Fatal("expected core benchmark to be included")
	}
	if cfg.IncludeBenchmark("BenchmarkCoreSlow", "BenchmarkCoreSlow") {
		t.Fatal("expected excluded benchmark")
	}
	if group, err := cfg.GroupFor("BenchmarkGlobalRead", "BenchmarkGlobalRead"); err != nil || group != "storage" {
		t.Fatalf("GroupFor = %q, %v", group, err)
	}
	if group, err := cfg.GroupFor("BenchmarkFuture", "BenchmarkFuture"); err != nil || group != "other" {
		t.Fatalf("future GroupFor = %q, %v", group, err)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"id", "id: ../bad", "id"},
		{"version", "version: 2\nid: sample", "unsupported version"},
		{"path", "id: sample\nsite-path: ../bad", "site-path"},
		{"regex", "id: sample\ninclude: '['", "include pattern"},
		{"empty group", "id: sample\ngroups:\n  core:\n    title: Core", "no match pattern"},
		{"max", "id: sample\nmax-benchmarks: 5001", "max-benchmarks"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestGroupForRejectsOverlap(t *testing.T) {
	cfg := loadTestConfig(t, `
id: sample
groups:
  one: '^BenchmarkCore'
  two: 'CoreFast$'
`)
	_, err := cfg.GroupFor("BenchmarkCoreFast", "BenchmarkCoreFast")
	if err == nil || !strings.Contains(err.Error(), "multiple groups") {
		t.Fatalf("GroupFor error = %v", err)
	}
}

func loadTestConfig(t *testing.T, body string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpunion/setup-benchmark-go-action/internal/config"
	"github.com/cpunion/setup-benchmark-go-action/internal/model"
)

func TestWriteAndLoadPlatforms(t *testing.T) {
	cfg := loadTestConfig(t, "id: sample\ngroups:\n  core: '^BenchmarkCore'\n")
	root := t.TempDir()
	for _, platform := range []string{"linux-amd64", "darwin-arm64"} {
		if err := Write(filepath.Join(root, platform), cfg, testResult(platform)); err != nil {
			t.Fatal(err)
		}
	}
	loaded, results, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != "sample" || len(results) != 2 {
		t.Fatalf("Load = config %q, %d results", loaded.ID, len(results))
	}
	if results[0].Platform.ID != "darwin-arm64" || results[1].Platform.ID != "linux-amd64" {
		t.Fatalf("results are not sorted: %+v", results)
	}
}

func TestLoadRejectsDifferentConfigurations(t *testing.T) {
	root := t.TempDir()
	first := loadTestConfig(t, "id: sample\ngroups:\n  core: '^BenchmarkCore'\n")
	second := loadTestConfig(t, "id: sample\ngroups:\n  runtime: '^BenchmarkCore'\n")
	if err := Write(filepath.Join(root, "one"), first, testResult("linux-amd64")); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(root, "two"), second, func() model.Result {
		result := testResult("darwin-arm64")
		result.Benchmarks[0].Group = "runtime"
		return result
	}()); err != nil {
		t.Fatal(err)
	}
	_, _, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "configurations do not match") {
		t.Fatalf("Load error = %v", err)
	}
}

func testResult(platform string) model.Result {
	return model.Result{
		SchemaVersion: model.SchemaVersion,
		SuiteID:       "sample",
		Source: model.Source{
			Repository: "owner/project",
			SHA:        strings.Repeat("a", 40),
			URL:        "https://github.com/owner/project/commit/" + strings.Repeat("a", 40),
			Timestamp:  time.Unix(100, 0).UTC(),
		},
		Platform: model.Platform{ID: platform, Label: platform},
		Benchmarks: []model.Benchmark{{
			Name:  "BenchmarkCore",
			Group: "core",
			Samples: []model.Sample{{
				Iterations:   10,
				Measurements: map[string]float64{"ns/op": 1},
			}},
			Measurements: map[string]float64{"ns/op": 1},
		}},
	}
}

func loadTestConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

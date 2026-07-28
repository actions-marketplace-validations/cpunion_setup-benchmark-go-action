package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpunion/setup-benchmark-go-action/internal/config"
	"github.com/cpunion/setup-benchmark-go-action/internal/model"
)

func TestWriteFirstSeriesAndDelta(t *testing.T) {
	cfg := reportConfig(t)
	current := reportEntry(12)
	path := filepath.Join(t.TempDir(), "comment.md")
	if err := Write(path, "https://example.test/?series=pull%2F1", cfg, current, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"<!-- go-benchmark:sample -->", "| 12 | new |", "No main baseline exists"} {
		if !strings.Contains(text, want) {
			t.Errorf("report does not contain %q:\n%s", want, text)
		}
	}

	baseline := reportEntry(10)
	if err := Write(path, "", cfg, current, &baseline); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "| 12 | +20.0% |") {
		t.Fatalf("delta report:\n%s", body)
	}
}

func TestDeltaFromZero(t *testing.T) {
	zero := 0.0
	if got := delta(0, &zero, "lower"); got != "0.0%" {
		t.Fatalf("delta(0, 0) = %q", got)
	}
	if got := delta(1, &zero, "lower"); got != "from 0" {
		t.Fatalf("delta(1, 0) = %q", got)
	}
	ten := 10.0
	if got := delta(12, &ten, "lower"); got != "+20.0% (worse)" {
		t.Fatalf("lower delta = %q", got)
	}
}

func reportConfig(t *testing.T) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("id: sample\ntitle: Sample benchmarks\ngroups:\n  core: '^BenchmarkCore'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func reportEntry(value float64) model.Entry {
	sha := strings.Repeat("a", 40)
	result := model.Result{
		Source: model.Source{
			Repository: "owner/project",
			SHA:        sha,
			URL:        "https://github.com/owner/project/commit/" + sha,
			Timestamp:  time.Unix(100, 0).UTC(),
		},
		Platform: model.Platform{ID: "linux-amd64", Label: "Linux"},
		Benchmarks: []model.Benchmark{{
			Name:  "BenchmarkCore",
			Group: "core",
			Samples: []model.Sample{{
				Iterations:   10,
				Measurements: map[string]float64{"ns/op": value},
			}},
			Measurements: map[string]float64{"ns/op": value},
		}},
	}
	return model.Entry{Source: result.Source, Platforms: map[string]model.Result{"linux-amd64": result}}
}

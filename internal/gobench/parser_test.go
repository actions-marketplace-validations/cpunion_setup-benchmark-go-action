package gobench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpunion/setup-benchmark-go-action/internal/config"
)

func TestParse(t *testing.T) {
	cfg := testConfig(t, `
id: sample
groups:
  core: '^Benchmark(Core|Alloc)'
exclude: '^BenchmarkCoreSkip$'
`)
	input := `
goos: linux
goarch: amd64
pkg: example.com/project/core
cpu: Test CPU
Unit ns/op better=lower
Unit ns/op assume=exact
BenchmarkCoreFast-8 100 12.5 ns/op 8 B/op 1 allocs/op
BenchmarkCoreFast-8 80 11.5 ns/op 6 B/op 1 allocs/op
BenchmarkCoreSkip-8 100 99 ns/op
BenchmarkAlloc-8 50 4.25 ns/op 0 B/op 0 allocs/op
PASS
`
	parsed, err := Parse(strings.NewReader(input), cfg)
	if err != nil {
		t.Fatal(err)
	}
	values := parsed.Benchmarks
	if len(values) != 2 {
		t.Fatalf("got %d benchmarks: %+v", len(values), values)
	}
	if got := values[0]; got.Name != "BenchmarkAlloc" || got.Package != "example.com/project/core" ||
		got.Group != "core" || got.Measurements["ns/op"] != 4.25 {
		t.Fatalf("first benchmark = %+v", got)
	}
	if got := values[1]; len(got.Samples) != 2 || got.Measurements["ns/op"] != 12 ||
		got.Measurements["B/op"] != 7 {
		t.Fatalf("repeated benchmark summary = %+v", got)
	}
	if got := parsed.Units["ns/op"]; got.Better != "lower" || got.Assume != "exact" {
		t.Fatalf("ns/op metadata = %+v", got)
	}
	if got := parsed.Units["B/op"].Better; got != "lower" {
		t.Fatalf("B/op better = %q", got)
	}
}

func TestParseRejectsInvalidOutput(t *testing.T) {
	cfg := testConfig(t, "id: sample\n")
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "PASS\n", "no included"},
		{"iteration", "BenchmarkOne-8 bad 1 ns/op\n", "iteration"},
		{"value", "BenchmarkOne-8 1 bad ns/op\n", "parsing measurement"},
		{"malformed", "BenchmarkOne-8 1 1\n", "missing units"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input), cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse error = %v, want %q", err, tt.want)
			}
		})
	}
}

func testConfig(t *testing.T, body string) *config.Config {
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

package gobench

import (
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/cpunion/setup-benchmark-go-action/internal/config"
	"github.com/cpunion/setup-benchmark-go-action/internal/model"
	"golang.org/x/perf/benchfmt"
)

var cpuSuffix = regexp.MustCompile(`-\d+$`)

type Parsed struct {
	Benchmarks []model.Benchmark
	Units      map[string]model.UnitMetadata
}

func Parse(r io.Reader, cfg *config.Config) (*Parsed, error) {
	reader := benchfmt.NewReader(r, "benchmark output")
	benchmarks := make(map[string]*model.Benchmark)
	normalizedUnits := make(map[string]string)
	for reader.Scan() {
		switch record := reader.Result().(type) {
		case *benchfmt.SyntaxError:
			return nil, record
		case *benchfmt.UnitMetadata:
			continue
		case *benchfmt.Result:
			if err := addResult(benchmarks, normalizedUnits, record, cfg); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported Go benchmark record %T", record)
		}
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	if len(benchmarks) == 0 {
		return nil, fmt.Errorf("no included Go benchmarks found")
	}
	if len(benchmarks) > cfg.MaxBenchmarks {
		return nil, fmt.Errorf("benchmark count exceeds max-benchmarks %d", cfg.MaxBenchmarks)
	}
	values := make([]model.Benchmark, 0, len(benchmarks))
	for _, benchmark := range benchmarks {
		benchmark.Measurements = summarize(benchmark.Samples)
		values = append(values, *benchmark)
	}
	slices.SortFunc(values, func(a, b model.Benchmark) int {
		return strings.Compare(a.Key(), b.Key())
	})
	return &Parsed{
		Benchmarks: values,
		Units:      collectUnitMetadata(reader.Units(), normalizedUnits),
	}, nil
}

func addResult(
	benchmarks map[string]*model.Benchmark,
	normalizedUnits map[string]string,
	result *benchfmt.Result,
	cfg *config.Config,
) error {
	name := "Benchmark" + cpuSuffix.ReplaceAllString(string(result.Name.Full()), "")
	candidate := model.Benchmark{Name: name, Package: result.GetConfig("pkg")}
	key := candidate.Key()
	if !cfg.IncludeBenchmark(name, key) {
		return nil
	}
	if result.Iters <= 0 {
		return fmt.Errorf("benchmark %q has invalid iteration count %d", key, result.Iters)
	}
	measurements := make(map[string]float64, len(result.Values))
	for _, measurement := range result.Values {
		value, unit := measurement.Value, measurement.Unit
		if measurement.OrigUnit != "" {
			value, unit = measurement.OrigValue, measurement.OrigUnit
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return fmt.Errorf("benchmark %q unit %q has invalid value %v", key, unit, value)
		}
		if _, exists := measurements[unit]; exists {
			return fmt.Errorf("benchmark %q repeats unit %q", key, unit)
		}
		measurements[unit] = value
		normalizedUnits[unit] = measurement.Unit
	}
	if len(measurements) == 0 {
		return fmt.Errorf("benchmark %q has no measurements", key)
	}
	benchmark := benchmarks[key]
	if benchmark == nil {
		group, err := cfg.GroupFor(name, key)
		if err != nil {
			return err
		}
		candidate.Group = group
		benchmark = &candidate
		benchmarks[key] = benchmark
	}
	benchmark.Samples = append(benchmark.Samples, model.Sample{
		Iterations:   uint64(result.Iters),
		Measurements: measurements,
	})
	return nil
}

func summarize(samples []model.Sample) map[string]float64 {
	values := make(map[string][]float64)
	for _, sample := range samples {
		for unit, value := range sample.Measurements {
			values[unit] = append(values[unit], value)
		}
	}
	summary := make(map[string]float64, len(values))
	for unit, samples := range values {
		slices.Sort(samples)
		middle := len(samples) / 2
		if len(samples)%2 == 0 {
			summary[unit] = (samples[middle-1] + samples[middle]) / 2
		} else {
			summary[unit] = samples[middle]
		}
	}
	return summary
}

func collectUnitMetadata(units benchfmt.UnitMetadataMap, normalized map[string]string) map[string]model.UnitMetadata {
	result := make(map[string]model.UnitMetadata)
	for display, canonical := range normalized {
		metadata := model.UnitMetadata{}
		switch units.GetBetter(canonical) {
		case -1:
			metadata.Better = "lower"
		case 1:
			metadata.Better = "higher"
		default:
			metadata.Better = defaultBetter(display)
		}
		if value := units.Get(canonical, "assume"); value != nil {
			metadata.Assume = value.Value
		}
		if metadata.Better != "" || metadata.Assume != "" {
			result[display] = metadata
		}
	}
	return result
}

func defaultBetter(unit string) string {
	switch unit {
	case "ns/op", "B/op", "allocs/op":
		return "lower"
	case "MB/s":
		return "higher"
	default:
		return ""
	}
}

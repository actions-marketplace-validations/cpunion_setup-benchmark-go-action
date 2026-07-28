package store

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cpunion/setup-benchmark-go-action/internal/config"
	"github.com/cpunion/setup-benchmark-go-action/internal/model"
)

var (
	safePart       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	safeSHA        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	safeRepository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	safePackage    = regexp.MustCompile(`^[A-Za-z0-9._~+/-]*$`)
)

//go:embed web/*
var webFiles embed.FS

type SeriesSpec struct {
	Kind  string
	ID    string
	Label string
}

type UpdateResult struct {
	Entry       model.Entry
	Main        *model.Entry
	HistoryPath string
	SitePath    string
}

func LoadResults(root string, cfg *config.Config) ([]model.Result, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("artifact contains symlink %s", path)
		}
		if !entry.IsDir() && entry.Name() == "result.json" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no result.json artifacts found under %s", root)
	}
	results := make([]model.Result, 0, len(paths))
	platforms := make(map[string]bool)
	var source *model.Source
	for _, path := range paths {
		var result model.Result
		if err := readJSON(path, &result); err != nil {
			return nil, err
		}
		if err := ValidateResult(&result, cfg); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if platforms[result.Platform.ID] {
			return nil, fmt.Errorf("duplicate platform %q", result.Platform.ID)
		}
		platforms[result.Platform.ID] = true
		if source == nil {
			copy := result.Source
			source = &copy
		} else if source.Repository != result.Source.Repository || source.SHA != result.Source.SHA {
			return nil, fmt.Errorf("platform %q source %s@%s does not match %s@%s",
				result.Platform.ID, result.Source.Repository, result.Source.SHA,
				source.Repository, source.SHA)
		}
		results = append(results, result)
	}
	slices.SortFunc(results, func(a, b model.Result) int {
		return strings.Compare(a.Platform.ID, b.Platform.ID)
	})
	return results, nil
}

func ValidateResult(result *model.Result, cfg *config.Config) error {
	if result.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("unsupported result schema %d", result.SchemaVersion)
	}
	if result.SuiteID != cfg.ID {
		return fmt.Errorf("suite id %q does not match config %q", result.SuiteID, cfg.ID)
	}
	if !safeRepository.MatchString(result.Source.Repository) {
		return fmt.Errorf("invalid source repository %q", result.Source.Repository)
	}
	if !safeSHA.MatchString(result.Source.SHA) {
		return fmt.Errorf("invalid source SHA %q", result.Source.SHA)
	}
	if err := validateURL("source URL", result.Source.URL); err != nil {
		return err
	}
	if result.Source.RunURL != "" {
		if err := validateURL("run URL", result.Source.RunURL); err != nil {
			return err
		}
	}
	if result.Source.Timestamp.IsZero() {
		return errors.New("source timestamp is empty")
	}
	if !safePart.MatchString(result.Platform.ID) {
		return fmt.Errorf("invalid platform id %q", result.Platform.ID)
	}
	if err := validateLabel("platform label", result.Platform.Label); err != nil {
		return err
	}
	if len(result.Benchmarks) == 0 || len(result.Benchmarks) > cfg.MaxBenchmarks {
		return fmt.Errorf("benchmark count %d is outside 1..%d", len(result.Benchmarks), cfg.MaxBenchmarks)
	}
	if len(result.Units) > 64 {
		return fmt.Errorf("unit metadata count %d exceeds 64", len(result.Units))
	}
	for unit, metadata := range result.Units {
		if !validUnit(unit) {
			return fmt.Errorf("invalid metadata unit %q", unit)
		}
		if metadata.Better != "" && metadata.Better != "higher" && metadata.Better != "lower" {
			return fmt.Errorf("unit %q has invalid better value %q", unit, metadata.Better)
		}
		if metadata.Assume != "" && metadata.Assume != "exact" && metadata.Assume != "nothing" {
			return fmt.Errorf("unit %q has invalid assume value %q", unit, metadata.Assume)
		}
	}
	seen := make(map[string]bool, len(result.Benchmarks))
	for i := range result.Benchmarks {
		benchmark := &result.Benchmarks[i]
		if !validBenchmark(benchmark.Name) {
			return fmt.Errorf("invalid benchmark name %q", benchmark.Name)
		}
		if !safePackage.MatchString(benchmark.Package) {
			return fmt.Errorf("benchmark %q has invalid package %q", benchmark.Name, benchmark.Package)
		}
		key := benchmark.Key()
		if seen[key] {
			return fmt.Errorf("duplicate benchmark %q", key)
		}
		seen[key] = true
		if !cfg.IncludeBenchmark(benchmark.Name, key) {
			return fmt.Errorf("benchmark %q is excluded by trusted config", key)
		}
		group, err := cfg.GroupFor(benchmark.Name, key)
		if err != nil {
			return err
		}
		if benchmark.Group != group {
			return fmt.Errorf("benchmark %q group %q does not match trusted group %q", key, benchmark.Group, group)
		}
		if len(benchmark.Samples) == 0 || len(benchmark.Samples) > 100 {
			return fmt.Errorf("benchmark %q sample count %d is outside 1..100", key, len(benchmark.Samples))
		}
		if len(benchmark.Measurements) == 0 || len(benchmark.Measurements) > 32 {
			return fmt.Errorf("benchmark %q has no measurements", key)
		}
		samples := make(map[string][]float64)
		for _, sample := range benchmark.Samples {
			if sample.Iterations == 0 {
				return fmt.Errorf("benchmark %q has a sample with zero iterations", key)
			}
			if len(sample.Measurements) == 0 || len(sample.Measurements) > 32 {
				return fmt.Errorf("benchmark %q has an invalid sample measurement count", key)
			}
			for unit, value := range sample.Measurements {
				if err := validateMeasurement(key, unit, value); err != nil {
					return err
				}
				samples[unit] = append(samples[unit], value)
			}
		}
		for unit, value := range benchmark.Measurements {
			if err := validateMeasurement(key, unit, value); err != nil {
				return err
			}
			values := samples[unit]
			if len(values) == 0 || value != median(values) {
				return fmt.Errorf("benchmark %q unit %q summary is not the sample median", key, unit)
			}
		}
		if len(samples) != len(benchmark.Measurements) {
			return fmt.Errorf("benchmark %q sample and summary units do not match", key)
		}
	}
	return nil
}

func validateMeasurement(benchmark, unit string, value float64) error {
	if !validUnit(unit) {
		return fmt.Errorf("benchmark %q has invalid unit %q", benchmark, unit)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fmt.Errorf("benchmark %q unit %q has invalid value %v", benchmark, unit, value)
	}
	return nil
}

func validUnit(unit string) bool {
	if unit == "" || len(unit) > 64 || !utf8.ValidString(unit) {
		return false
	}
	for _, r := range unit {
		if unicode.IsControl(r) || unicode.IsSpace(r) || strings.ContainsRune("`|<>", r) {
			return false
		}
	}
	return true
}

func validBenchmark(name string) bool {
	if !strings.HasPrefix(name, "Benchmark") || len(name) == len("Benchmark") ||
		len(name) > 300 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) || strings.ContainsRune("`|<>", r) {
			return false
		}
	}
	return true
}

func median(values []float64) float64 {
	values = slices.Clone(values)
	slices.Sort(values)
	middle := len(values) / 2
	if len(values)%2 == 0 {
		return (values[middle-1] + values[middle]) / 2
	}
	return values[middle]
}

func validateURL(field, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		strings.ContainsAny(value, "<>\r\n") {
		return fmt.Errorf("invalid %s %q", field, value)
	}
	return nil
}

func validateLabel(field, value string) error {
	if value == "" || len(value) > 160 || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be a trimmed string of 1..160 bytes", field)
	}
	for _, r := range value {
		if r < ' ' || r == '\u007f' {
			return fmt.Errorf("%s contains a control character", field)
		}
	}
	return nil
}

func Update(dataRoot string, cfg *config.Config, series SeriesSpec, results []model.Result) (*UpdateResult, error) {
	if series.Kind != "main" && series.Kind != "pull" && series.Kind != "branch" {
		return nil, fmt.Errorf("unsupported series kind %q", series.Kind)
	}
	if !safePart.MatchString(series.ID) {
		return nil, fmt.Errorf("invalid series id %q", series.ID)
	}
	if err := validateLabel("series label", series.Label); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, errors.New("cannot update history without results")
	}
	source := results[0].Source
	platforms := make(map[string]model.Result, len(results))
	for _, result := range results {
		if err := ValidateResult(&result, cfg); err != nil {
			return nil, err
		}
		if result.Source.Repository != source.Repository || result.Source.SHA != source.SHA {
			return nil, fmt.Errorf("platform %q does not match source %s@%s",
				result.Platform.ID, source.Repository, source.SHA)
		}
		if _, exists := platforms[result.Platform.ID]; exists {
			return nil, fmt.Errorf("duplicate platform %q", result.Platform.ID)
		}
		platforms[result.Platform.ID] = result
	}
	entry := model.Entry{Source: source, Platforms: platforms}
	relative := filepath.Join("series", series.Kind, series.ID, "history.json")
	configRelative := filepath.Join("series", series.Kind, series.ID, "config.json")
	siteRoot := filepath.Join(dataRoot, filepath.FromSlash(cfg.SitePath))
	historyPath := filepath.Join(siteRoot, relative)
	history := model.History{
		SchemaVersion: model.SchemaVersion,
		SuiteID:       cfg.ID,
		Kind:          series.Kind,
		ID:            series.ID,
		Label:         series.Label,
	}
	if err := readJSONIfExists(historyPath, &history); err != nil {
		return nil, err
	}
	if err := validateHistory(&history, cfg, series); err != nil {
		return nil, err
	}
	replaced := false
	for i := range history.Entries {
		if history.Entries[i].Source.SHA == source.SHA {
			history.Entries[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		history.Entries = append(history.Entries, entry)
	}
	slices.SortFunc(history.Entries, func(a, b model.Entry) int {
		if order := a.Source.Timestamp.Compare(b.Source.Timestamp); order != 0 {
			return order
		}
		return strings.Compare(a.Source.SHA, b.Source.SHA)
	})
	if len(history.Entries) > 500 {
		history.Entries = history.Entries[len(history.Entries)-500:]
	}
	if err := writeJSON(historyPath, history); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(siteRoot, configRelative), cfg); err != nil {
		return nil, err
	}

	indexPath := filepath.Join(siteRoot, "series.json")
	index := model.SeriesIndex{SchemaVersion: model.SchemaVersion}
	if err := readJSONIfExists(indexPath, &index); err != nil {
		return nil, err
	}
	if index.SchemaVersion != model.SchemaVersion {
		return nil, fmt.Errorf("unsupported series index schema %d", index.SchemaVersion)
	}
	item := model.Series{
		Kind:       series.Kind,
		ID:         series.ID,
		Label:      series.Label,
		Path:       filepath.ToSlash(relative),
		ConfigPath: filepath.ToSlash(configRelative),
		SHA:        source.SHA,
		SourceURL:  source.URL,
		UpdatedAt:  time.Now().UTC(),
	}
	found := false
	for i := range index.Series {
		if index.Series[i].Kind == item.Kind && index.Series[i].ID == item.ID {
			index.Series[i] = item
			found = true
			break
		}
	}
	if !found {
		index.Series = append(index.Series, item)
	}
	slices.SortFunc(index.Series, compareSeries)
	if err := writeJSON(indexPath, index); err != nil {
		return nil, err
	}
	if err := writeWeb(dataRoot, siteRoot); err != nil {
		return nil, err
	}

	var main *model.Entry
	mainPath := filepath.Join(siteRoot, "series", "main", "main", "history.json")
	var mainHistory model.History
	if err := readJSONIfExists(mainPath, &mainHistory); err != nil {
		return nil, err
	}
	main = latestMatchingPlatforms(&mainHistory, platforms)
	return &UpdateResult{
		Entry:       entry,
		Main:        main,
		HistoryPath: historyPath,
		SitePath:    siteRoot,
	}, nil
}

func validateHistory(history *model.History, cfg *config.Config, series SeriesSpec) error {
	if history.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("unsupported history schema %d", history.SchemaVersion)
	}
	if history.SuiteID != cfg.ID || history.Kind != series.Kind || history.ID != series.ID {
		return fmt.Errorf("history identity does not match %s/%s/%s", cfg.ID, series.Kind, series.ID)
	}
	history.Label = series.Label
	return nil
}

func latestMatchingPlatforms(history *model.History, current map[string]model.Result) *model.Entry {
	if len(history.Entries) == 0 {
		return nil
	}
	platforms := make(map[string]model.Result, len(current))
	var source model.Source
	for i := len(history.Entries) - 1; i >= 0 && len(platforms) < len(current); i-- {
		entry := history.Entries[i]
		for id := range current {
			if _, exists := platforms[id]; exists {
				continue
			}
			if result, ok := entry.Platforms[id]; ok {
				platforms[id] = result
				if source.Timestamp.IsZero() {
					source = entry.Source
				}
			}
		}
	}
	if len(platforms) == 0 {
		return nil
	}
	return &model.Entry{Source: source, Platforms: platforms}
}

func compareSeries(a, b model.Series) int {
	rank := func(kind string) int {
		switch kind {
		case "main":
			return 0
		case "branch":
			return 1
		default:
			return 2
		}
	}
	if left, right := rank(a.Kind), rank(b.Kind); left != right {
		return left - right
	}
	return strings.Compare(a.Label, b.Label)
}

func writeWeb(dataRoot, siteRoot string) error {
	for _, name := range []string{"index.html", "app.js", "styles.css"} {
		data, err := webFiles.ReadFile("web/" + name)
		if err != nil {
			return err
		}
		path := filepath.Join(siteRoot, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dataRoot, ".nojekyll"), nil, 0o644)
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func readJSONIfExists(path string, value any) error {
	err := readJSON(path, value)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

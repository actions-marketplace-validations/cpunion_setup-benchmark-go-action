package report

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/cpunion/setup-benchmark-go-action/internal/config"
	"github.com/cpunion/setup-benchmark-go-action/internal/model"
)

func Write(path, siteURL string, cfg *config.Config, current model.Entry, baseline *model.Entry) error {
	if len(current.Source.SHA) < 12 {
		return fmt.Errorf("source SHA %q is too short", current.Source.SHA)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "<!-- go-benchmark:%s -->\n", cfg.ID)
	fmt.Fprintf(&body, "## %s\n\n", markdown(cfg.Title))
	fmt.Fprintf(&body, "[`%s`](<%s>)", current.Source.SHA[:12], current.Source.URL)
	if current.Source.RunURL != "" {
		fmt.Fprintf(&body, " | [workflow run](<%s>)", current.Source.RunURL)
	}
	if siteURL != "" {
		fmt.Fprintf(&body, " | [long-term charts](<%s>)", siteURL)
	}
	body.WriteString("\n\n")

	platformIDs := make([]string, 0, len(current.Platforms))
	for id := range current.Platforms {
		platformIDs = append(platformIDs, id)
	}
	slices.Sort(platformIDs)
	for _, platformID := range platformIDs {
		result := current.Platforms[platformID]
		fmt.Fprintf(&body, "### %s\n\n", markdown(result.Platform.Label))
		body.WriteString("| Group | Benchmark | Metric | Current | vs main |\n")
		body.WriteString("|---|---|---|---:|---:|\n")
		for _, benchmark := range result.Benchmarks {
			units := make([]string, 0, len(benchmark.Measurements))
			for unit := range benchmark.Measurements {
				units = append(units, unit)
			}
			slices.Sort(units)
			for _, unit := range units {
				value := benchmark.Measurements[unit]
				group := benchmark.Group
				if group == "other" {
					group = "Other"
				} else if spec, ok := cfg.Groups[group]; ok {
					group = spec.Title
				}
				fmt.Fprintf(&body, "| %s | `%s` | `%s` | %s | %s |\n",
					tableCell(group), benchmarkLabel(benchmark), unit, format(value),
					delta(value, baselineValue(baseline, platformID, benchmark.Key(), unit),
						result.Units[unit].Better))
			}
		}
		body.WriteString("\n")
	}
	if baseline == nil {
		body.WriteString("_No main baseline exists yet; this first series is published with all metrics marked `new`._\n")
	} else {
		body.WriteString("_Compared only with the latest matching platform in the main series._\n")
	}
	return os.WriteFile(path, []byte(body.String()), 0o644)
}

func markdown(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`*`, `\*`,
		`_`, `\_`,
		`[`, `\[`,
		`]`, `\]`,
		`#`, `\#`,
		"`", "\\`",
	)
	return replacer.Replace(value)
}

func tableCell(value string) string {
	return strings.ReplaceAll(markdown(value), "|", `\|`)
}

func benchmarkLabel(benchmark model.Benchmark) string {
	if benchmark.Package == "" {
		return benchmark.Name
	}
	return benchmark.Package + "::" + benchmark.Name
}

func baselineValue(entry *model.Entry, platformID, benchmarkKey, unit string) *float64 {
	if entry == nil {
		return nil
	}
	result, ok := entry.Platforms[platformID]
	if !ok {
		return nil
	}
	for _, benchmark := range result.Benchmarks {
		if benchmark.Key() == benchmarkKey {
			if value, ok := benchmark.Measurements[unit]; ok {
				copy := value
				return &copy
			}
		}
	}
	return nil
}

func delta(current float64, baseline *float64, better string) string {
	if baseline == nil {
		return "new"
	}
	if *baseline == 0 {
		if current == 0 {
			return "0.0%"
		}
		return "from 0"
	}
	change := (current/(*baseline) - 1) * 100
	value := fmt.Sprintf("%+.1f%%", change)
	if change == 0 {
		return value
	}
	switch better {
	case "lower":
		if change < 0 {
			return value + " (better)"
		}
		return value + " (worse)"
	case "higher":
		if change > 0 {
			return value + " (better)"
		}
		return value + " (worse)"
	default:
		return value
	}
}

func format(value float64) string {
	if math.Trunc(value) == value && value < 1e12 {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', 3, 64)
}

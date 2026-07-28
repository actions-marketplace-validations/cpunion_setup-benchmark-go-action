package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cpunion/setup-benchmark-go-action/internal/artifact"
	"github.com/cpunion/setup-benchmark-go-action/internal/config"
	"github.com/cpunion/setup-benchmark-go-action/internal/gobench"
	"github.com/cpunion/setup-benchmark-go-action/internal/model"
	"github.com/cpunion/setup-benchmark-go-action/internal/report"
	"github.com/cpunion/setup-benchmark-go-action/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "setup-benchmark-go:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected record or render command")
	}
	switch args[0] {
	case "record":
		return runRecord(args[1:])
	case "render":
		return runRender(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runRecord(args []string) error {
	flags := flag.NewFlagSet("record", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var (
		configPath   = flags.String("config", "", "benchmark configuration")
		inputPath    = flags.String("input", "", "Go benchmark output")
		outputDir    = flags.String("output-dir", "", "artifact output directory")
		platformID   = flags.String("platform-id", runtime.GOOS+"-"+runtime.GOARCH, "platform identifier")
		platformName = flags.String("platform-label", "", "platform display label")
		goVersion    = flags.String("go-version", strings.TrimPrefix(runtime.Version(), "go"), "Go version")
		targetOS     = flags.String("os", runtime.GOOS, "platform operating system")
		targetArch   = flags.String("arch", runtime.GOARCH, "platform architecture")
		runner       = flags.String("runner", os.Getenv("RUNNER_NAME"), "runner label")
		repository   = flags.String("repository", os.Getenv("GITHUB_REPOSITORY"), "source repository")
		sha          = flags.String("sha", os.Getenv("GITHUB_SHA"), "source commit")
		ref          = flags.String("ref", sourceRef(), "source ref")
		sourceURL    = flags.String("source-url", "", "source commit URL")
		runURL       = flags.String("run-url", defaultRunURL(), "workflow run URL")
		timestamp    = flags.String("timestamp", "", "RFC3339 source timestamp")
		githubOutput = flags.String("github-output", os.Getenv("GITHUB_OUTPUT"), "GitHub output file")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || *inputPath == "" {
		return errors.New("record requires --config and --input")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	input, err := os.Open(*inputPath)
	if err != nil {
		return err
	}
	parsed, parseErr := gobench.Parse(input, cfg)
	closeErr := input.Close()
	if parseErr != nil {
		return parseErr
	}
	if closeErr != nil {
		return closeErr
	}
	when := time.Now().UTC()
	if *timestamp != "" {
		when, err = time.Parse(time.RFC3339Nano, *timestamp)
		if err != nil {
			return fmt.Errorf("invalid timestamp: %w", err)
		}
	}
	if *sourceURL == "" {
		*sourceURL = defaultSourceURL(*repository, *sha)
	}
	if *platformName == "" {
		*platformName = fmt.Sprintf("%s / %s / Go %s", displayOS(*targetOS), *targetArch, *goVersion)
	}
	result := model.Result{
		SchemaVersion: model.SchemaVersion,
		SuiteID:       cfg.ID,
		Source: model.Source{
			Repository: *repository,
			SHA:        *sha,
			Ref:        *ref,
			URL:        *sourceURL,
			RunURL:     *runURL,
			Timestamp:  when,
		},
		Platform: model.Platform{
			ID:        *platformID,
			Label:     *platformName,
			OS:        *targetOS,
			Arch:      *targetArch,
			GoVersion: *goVersion,
			Runner:    *runner,
		},
		Units:      parsed.Units,
		Benchmarks: parsed.Benchmarks,
	}
	if *outputDir == "" {
		*outputDir, err = os.MkdirTemp(os.Getenv("RUNNER_TEMP"), "go-benchmark-"+cfg.ID+"-"+*platformID+"-")
		if err != nil {
			return err
		}
	}
	if err := artifact.Write(*outputDir, cfg, result); err != nil {
		return err
	}
	outputs := map[string]string{
		"artifact-name": "go-benchmark-" + cfg.ID + "-" + *platformID,
		"artifact-path": *outputDir,
		"platform-id":   *platformID,
		"suite-id":      cfg.ID,
	}
	if err := writeOutputs(*githubOutput, outputs); err != nil {
		return err
	}
	fmt.Printf("recorded %d benchmarks for %s in %s\n", len(parsed.Benchmarks), *platformID, *outputDir)
	return nil
}

func runRender(args []string) error {
	flags := flag.NewFlagSet("render", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var (
		artifacts    = flags.String("artifacts", "", "downloaded artifact directory")
		dataDir      = flags.String("data-dir", "", "benchmark data branch directory")
		seriesKind   = flags.String("series-kind", "", "main, branch, or pull")
		seriesID     = flags.String("series-id", "", "stable series identifier")
		seriesLabel  = flags.String("series-label", "", "series display label")
		siteBaseURL  = flags.String("site-base-url", "", "GitHub Pages root URL")
		commentPath  = flags.String("comment", "", "generated comment path")
		githubOutput = flags.String("github-output", os.Getenv("GITHUB_OUTPUT"), "GitHub output file")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *artifacts == "" || *dataDir == "" || *seriesKind == "" || *seriesID == "" || *seriesLabel == "" {
		return errors.New("render requires --artifacts, --data-dir, --series-kind, --series-id, and --series-label")
	}
	cfg, results, err := artifact.Load(*artifacts)
	if err != nil {
		return err
	}
	updated, err := store.Update(*dataDir, cfg, store.SeriesSpec{
		Kind:  *seriesKind,
		ID:    *seriesID,
		Label: *seriesLabel,
	}, results)
	if err != nil {
		return err
	}
	if *commentPath == "" {
		*commentPath = filepath.Join(*dataDir, ".go-benchmark-comment.md")
	}
	siteURL := ""
	if *siteBaseURL != "" {
		siteURL = strings.TrimRight(*siteBaseURL, "/") + "/" + strings.Trim(cfg.SitePath, "/") +
			"/?series=" + url.QueryEscape(*seriesKind+"/"+*seriesID)
	}
	if err := report.Write(*commentPath, siteURL, cfg, updated.Entry, updated.Main); err != nil {
		return err
	}
	outputs := map[string]string{
		"comment-path": *commentPath,
		"marker":       "<!-- go-benchmark:" + cfg.ID + " -->",
		"site-path":    updated.SitePath,
		"site-url":     siteURL,
		"suite-id":     cfg.ID,
	}
	if err := writeOutputs(*githubOutput, outputs); err != nil {
		return err
	}
	fmt.Printf("updated %s series %s with %d platforms\n", *seriesKind, *seriesID, len(results))
	return nil
}

func sourceRef() string {
	if value := os.Getenv("GITHUB_HEAD_REF"); value != "" {
		return value
	}
	if value := os.Getenv("GITHUB_REF_NAME"); value != "" {
		return value
	}
	return os.Getenv("GITHUB_REF")
}

func defaultSourceURL(repository, sha string) string {
	server := strings.TrimRight(os.Getenv("GITHUB_SERVER_URL"), "/")
	if server == "" {
		server = "https://github.com"
	}
	return server + "/" + repository + "/commit/" + sha
}

func defaultRunURL() string {
	repository, runID := os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID")
	if repository == "" || runID == "" {
		return ""
	}
	server := strings.TrimRight(os.Getenv("GITHUB_SERVER_URL"), "/")
	if server == "" {
		server = "https://github.com"
	}
	return server + "/" + repository + "/actions/runs/" + runID
}

func displayOS(value string) string {
	switch value {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	default:
		return value
	}
}

func writeOutputs(path string, values map[string]string) error {
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	for name, value := range values {
		if strings.ContainsAny(name+value, "\r\n") {
			return fmt.Errorf("GitHub output %q contains a newline", name)
		}
		if _, err := fmt.Fprintf(file, "%s=%s\n", name, value); err != nil {
			return err
		}
	}
	return nil
}

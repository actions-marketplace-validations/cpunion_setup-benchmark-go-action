package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordAndRender(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yml")
	benchmarkPath := filepath.Join(root, "go.txt")
	artifactPath := filepath.Join(root, "artifacts", "linux")
	outputPath := filepath.Join(root, "record-output")
	sha := strings.Repeat("a", 40)
	writeTestFile(t, configPath, "id: sample\ngroups:\n  core: '^BenchmarkCore'\n")
	writeTestFile(t, benchmarkPath, "pkg: example.com/project\nBenchmarkCore-2 10 12 ns/op 8 B/op\n")

	err := run([]string{
		"record",
		"--config", configPath,
		"--input", benchmarkPath,
		"--output-dir", artifactPath,
		"--platform-id", "linux-amd64",
		"--repository", "owner/project",
		"--sha", sha,
		"--source-url", "https://github.com/owner/project/commit/" + sha,
		"--timestamp", "2026-01-02T03:04:05Z",
		"--github-output", outputPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "artifact-name=go-benchmark-sample-linux-amd64") {
		t.Fatalf("record outputs:\n%s", output)
	}

	dataPath := filepath.Join(root, "data")
	commentPath := filepath.Join(root, "comment.md")
	if err := run([]string{
		"render",
		"--artifacts", filepath.Join(root, "artifacts"),
		"--data-dir", dataPath,
		"--series-kind", "pull",
		"--series-id", "1",
		"--series-label", "PR #1",
		"--site-base-url", "https://owner.github.io/project",
		"--comment", commentPath,
	}); err != nil {
		t.Fatal(err)
	}
	comment, err := os.ReadFile(commentPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(comment), "new") || !strings.Contains(string(comment), "pull%2F1") {
		t.Fatalf("rendered comment:\n%s", comment)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

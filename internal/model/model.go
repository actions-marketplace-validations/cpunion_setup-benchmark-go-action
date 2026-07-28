package model

import "time"

const SchemaVersion = 1

type Source struct {
	Repository string    `json:"repository"`
	SHA        string    `json:"sha"`
	Ref        string    `json:"ref,omitempty"`
	URL        string    `json:"url"`
	RunURL     string    `json:"runUrl,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

type Platform struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	OS        string `json:"os,omitempty"`
	Arch      string `json:"arch,omitempty"`
	GoVersion string `json:"goVersion,omitempty"`
	Runner    string `json:"runner,omitempty"`
}

type Sample struct {
	Iterations   uint64             `json:"iterations"`
	Measurements map[string]float64 `json:"measurements"`
}

type UnitMetadata struct {
	Better string `json:"better,omitempty"`
	Assume string `json:"assume,omitempty"`
}

type Benchmark struct {
	Name         string             `json:"name"`
	Package      string             `json:"package,omitempty"`
	Group        string             `json:"group"`
	Samples      []Sample           `json:"samples"`
	Measurements map[string]float64 `json:"measurements"`
}

func (b Benchmark) Key() string {
	if b.Package == "" {
		return b.Name
	}
	return b.Package + "::" + b.Name
}

type Result struct {
	SchemaVersion int                     `json:"schemaVersion"`
	SuiteID       string                  `json:"suiteId"`
	Source        Source                  `json:"source"`
	Platform      Platform                `json:"platform"`
	Units         map[string]UnitMetadata `json:"units,omitempty"`
	Benchmarks    []Benchmark             `json:"benchmarks"`
}

type Entry struct {
	Source    Source            `json:"source"`
	Platforms map[string]Result `json:"platforms"`
}

type History struct {
	SchemaVersion int     `json:"schemaVersion"`
	SuiteID       string  `json:"suiteId"`
	Kind          string  `json:"kind"`
	ID            string  `json:"id"`
	Label         string  `json:"label"`
	Entries       []Entry `json:"entries"`
}

type Series struct {
	Kind       string    `json:"kind"`
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	Path       string    `json:"path"`
	ConfigPath string    `json:"configPath"`
	SHA        string    `json:"sha"`
	SourceURL  string    `json:"sourceUrl"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type SeriesIndex struct {
	SchemaVersion int      `json:"schemaVersion"`
	Series        []Series `json:"series"`
}

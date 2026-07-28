package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type StringList []string

func (s *StringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" {
			*s = nil
		} else {
			*s = []string{node.Value}
		}
		return nil
	case yaml.SequenceNode:
		var values []string
		if err := node.Decode(&values); err != nil {
			return err
		}
		*s = values
		return nil
	default:
		return fmt.Errorf("expected a string or string list")
	}
}

type Group struct {
	Title string     `yaml:"title" json:"title"`
	Match StringList `yaml:"match" json:"match"`
}

func (g *Group) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		g.Match = []string{node.Value}
		return nil
	}
	type group Group
	var decoded group
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*g = Group(decoded)
	return nil
}

type Config struct {
	Version       int              `yaml:"version" json:"version"`
	ID            string           `yaml:"id" json:"id"`
	Title         string           `yaml:"title" json:"title"`
	SitePath      string           `yaml:"site-path" json:"sitePath"`
	Include       StringList       `yaml:"include" json:"include"`
	Exclude       StringList       `yaml:"exclude" json:"exclude"`
	MaxBenchmarks int              `yaml:"max-benchmarks" json:"maxBenchmarks"`
	Groups        map[string]Group `yaml:"groups" json:"groups"`

	include []*regexp.Regexp
	exclude []*regexp.Regexp
	groups  map[string][]*regexp.Regexp
}

func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}
	return prepare(filename, &cfg)
}

func LoadSnapshot(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}
	return prepare(filename, &cfg)
}

func prepare(filename string, cfg *Config) (*Config, error) {
	if err := cfg.prepare(); err != nil {
		return nil, fmt.Errorf("validate %s: %w", filename, err)
	}
	return cfg, nil
}

func (c *Config) prepare() error {
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Version != 1 {
		return fmt.Errorf("unsupported version %d", c.Version)
	}
	if !safeID.MatchString(c.ID) {
		return fmt.Errorf("id %q must match %s", c.ID, safeID)
	}
	if c.Title == "" {
		c.Title = c.ID + " benchmarks"
	}
	if err := validateLabel("title", c.Title); err != nil {
		return err
	}
	if c.SitePath == "" {
		c.SitePath = path.Join("go-benchmarks", c.ID)
	}
	c.SitePath = path.Clean(c.SitePath)
	if c.SitePath == "." || strings.HasPrefix(c.SitePath, "../") || path.IsAbs(c.SitePath) {
		return fmt.Errorf("site-path %q must stay within the data branch", c.SitePath)
	}
	if len(c.Include) == 0 {
		c.Include = []string{`^Benchmark`}
	}
	if c.MaxBenchmarks == 0 {
		c.MaxBenchmarks = 500
	}
	if c.MaxBenchmarks < 1 || c.MaxBenchmarks > 5000 {
		return errors.New("max-benchmarks must be between 1 and 5000")
	}
	var err error
	if c.include, err = compilePatterns("include", c.Include); err != nil {
		return err
	}
	if c.exclude, err = compilePatterns("exclude", c.Exclude); err != nil {
		return err
	}
	c.groups = make(map[string][]*regexp.Regexp, len(c.Groups))
	for id, group := range c.Groups {
		if !safeID.MatchString(id) {
			return fmt.Errorf("group id %q must match %s", id, safeID)
		}
		if len(group.Match) == 0 {
			return fmt.Errorf("group %q has no match pattern", id)
		}
		patterns, err := compilePatterns("group "+id, group.Match)
		if err != nil {
			return err
		}
		if group.Title == "" {
			group.Title = title(id)
			c.Groups[id] = group
		}
		if err := validateLabel("group "+id+" title", group.Title); err != nil {
			return err
		}
		c.groups[id] = patterns
	}
	return nil
}

func validateLabel(field, value string) error {
	if len(value) > 120 || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be a trimmed string of at most 120 bytes", field)
	}
	for _, r := range value {
		if r < ' ' || r == '\u007f' {
			return fmt.Errorf("%s contains a control character", field)
		}
	}
	return nil
}

func compilePatterns(field string, values []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(values))
	for _, value := range values {
		pattern, err := regexp.Compile(value)
		if err != nil {
			return nil, fmt.Errorf("%s pattern %q: %w", field, value, err)
		}
		out = append(out, pattern)
	}
	return out, nil
}

func title(id string) string {
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	for i := range words {
		if len(words[i]) != 0 {
			words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
		}
	}
	return strings.Join(words, " ")
}

func matches(patterns []*regexp.Regexp, name, key string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(name) || pattern.MatchString(key) {
			return true
		}
	}
	return false
}

func (c *Config) IncludeBenchmark(name, key string) bool {
	return matches(c.include, name, key) && !matches(c.exclude, name, key)
}

func (c *Config) GroupFor(name, key string) (string, error) {
	var matched []string
	for id, patterns := range c.groups {
		if matches(patterns, name, key) {
			matched = append(matched, id)
		}
	}
	slices.Sort(matched)
	switch len(matched) {
	case 0:
		return "other", nil
	case 1:
		return matched[0], nil
	default:
		return "", fmt.Errorf("benchmark %q matches multiple groups: %s", key, strings.Join(matched, ", "))
	}
}

func (c *Config) GroupIDs() []string {
	ids := make([]string, 0, len(c.Groups)+1)
	for id := range c.Groups {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return append(ids, "other")
}

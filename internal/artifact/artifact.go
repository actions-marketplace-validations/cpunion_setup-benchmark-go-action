package artifact

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/cpunion/setup-benchmark-go-action/internal/config"
	"github.com/cpunion/setup-benchmark-go-action/internal/model"
	"github.com/cpunion/setup-benchmark-go-action/internal/store"
)

const (
	ConfigName = "config.json"
	ResultName = "result.json"
)

func Write(dir string, cfg *config.Config, result model.Result) error {
	if err := store.ValidateResult(&result, cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, ConfigName), cfg); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, ResultName), result)
}

func Load(root string) (*config.Config, []model.Result, error) {
	cfg, err := loadConfig(root)
	if err != nil {
		return nil, nil, err
	}
	results, err := store.LoadResults(root, cfg)
	if err != nil {
		return nil, nil, err
	}
	return cfg, results, nil
}

func loadConfig(root string) (*config.Config, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("artifact contains symlink %s", path)
		}
		if !entry.IsDir() && entry.Name() == ConfigName {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no %s artifacts found under %s", ConfigName, root)
	}
	var (
		first     *config.Config
		canonical []byte
	)
	for _, path := range paths {
		cfg, err := config.LoadSnapshot(path)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		if first == nil {
			first = cfg
			canonical = encoded
			continue
		}
		if string(canonical) != string(encoded) {
			return nil, fmt.Errorf("benchmark configurations do not match: %s", path)
		}
	}
	return first, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

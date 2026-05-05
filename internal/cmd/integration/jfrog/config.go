// internal/cmd/integration/jfrog/config.go
package jfrog

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Source SourceConfig `yaml:"source"`
	JFrog  JFrogConfig  `yaml:"jfrog"`
}

type SourceConfig struct {
	PollInterval time.Duration `yaml:"poll_interval"`
	CursorFile   string        `yaml:"cursor_file"`
}

type JFrogConfig struct {
	URL         string `yaml:"url"`
	AccessToken string `yaml:"access_token"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}

	if cfg.JFrog.URL == "" {
		return nil, fmt.Errorf("config: jfrog.url is required")
	}
	if cfg.Source.PollInterval == 0 {
		cfg.Source.PollInterval = 60 * time.Second
	}
	if cfg.Source.CursorFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("config: resolve cursor file home dir: %w", err)
		}
		cfg.Source.CursorFile = filepath.Join(home, ".safedep", "integration-jfrog-cursor.json")
	}
	if tok := os.Getenv("SAFEDEP_JFROG_ACCESS_TOKEN"); tok != "" {
		cfg.JFrog.AccessToken = tok
	}
	if cfg.JFrog.AccessToken == "" {
		return nil, fmt.Errorf("config: jfrog.access_token is required (or set SAFEDEP_JFROG_ACCESS_TOKEN)")
	}

	return &cfg, nil
}

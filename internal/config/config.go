package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const configFileName = "config"

type Config struct {
	Tenant     string `mapstructure:"tenant"`
	DeviceName string `mapstructure:"device_name"`
	EndpointID string `mapstructure:"endpoint_id"`
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName(configFileName)
	v.SetConfigType("toml")
	v.AddConfigPath(Dir())

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("config: read: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}

	return &cfg, nil
}

func Save(cfg *Config) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}

	v := viper.New()
	v.Set("tenant", cfg.Tenant)
	v.Set("device_name", cfg.DeviceName)
	v.Set("endpoint_id", cfg.EndpointID)

	path := filepath.Join(Dir(), configFileName+".toml")
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}

	return nil
}

func Dir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "safedep")
	}

	return filepath.Join(base, "safedep")
}

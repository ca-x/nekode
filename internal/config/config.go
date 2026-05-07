package config

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultAddr    = ":18790"
	DefaultBaseURL = "http://localhost:18790"
)

type Config struct {
	Addr    string
	BaseURL string
	DataDir string
}

func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Addr:    env("NEKODE_ADDR", DefaultAddr),
		BaseURL: env("NEKODE_BASE_URL", DefaultBaseURL),
		DataDir: env("NEKODE_DATA_DIR", filepath.Join(home, ".nekode")),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Addr) == "" {
		return errors.New("addr is required")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return errors.New("data dir is required")
	}
	if _, err := url.ParseRequestURI(c.BaseURL); err != nil {
		return err
	}
	return nil
}

func env(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

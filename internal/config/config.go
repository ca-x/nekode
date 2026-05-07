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
	DefaultDBType  = "sqlite"
)

type Config struct {
	Addr         string
	BaseURL      string
	DataDir      string
	DBType       string
	DBDSN        string
	LegacyDBPath string
}

func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Addr:         env("NEKODE_ADDR", DefaultAddr),
		BaseURL:      env("NEKODE_BASE_URL", DefaultBaseURL),
		DataDir:      env("NEKODE_DATA_DIR", filepath.Join(home, ".nekode")),
		DBType:       env("NEKODE_DB_TYPE", DefaultDBType),
		DBDSN:        strings.TrimSpace(os.Getenv("NEKODE_DB_DSN")),
		LegacyDBPath: strings.TrimSpace(os.Getenv("NEKODE_DB_PATH")),
	}
	if cfg.DBDSN == "" && cfg.LegacyDBPath != "" {
		cfg.DBDSN = cfg.LegacyDBPath
	}
	if cfg.DBDSN == "" && normalizeDBType(cfg.DBType) == "sqlite" {
		cfg.DBDSN = filepath.Join(cfg.DataDir, "nekode.db")
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
	switch normalizeDBType(c.DBType) {
	case "sqlite":
		if strings.TrimSpace(c.DBDSN) == "" {
			return errors.New("sqlite database path or dsn is required")
		}
	case "postgres", "mysql":
		if strings.TrimSpace(c.DBDSN) == "" {
			return errors.New("database dsn is required")
		}
	default:
		return errors.New("database type must be sqlite, postgres, or mysql")
	}
	return nil
}

func (c Config) DatabaseType() string {
	return normalizeDBType(c.DBType)
}

func env(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func normalizeDBType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "sqlite", "sqlite3":
		return "sqlite"
	case "postgres", "postgresql":
		return "postgres"
	case "mysql":
		return "mysql"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

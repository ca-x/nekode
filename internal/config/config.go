package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAddr     = ":18790"
	DefaultGRPCAddr = "127.0.0.1:18789"
	DefaultBaseURL  = "http://localhost:18790"
	DefaultDBType   = "sqlite"
)

type Config struct {
	Addr            string
	GRPCAddr        string
	DaemonTransport string
	BaseURL         string
	DataDir         string
	DBType          string
	DBDSN           string
	LegacyDBPath    string
	CacheDriver     string
	CacheDir        string
	CacheTTL        time.Duration
	CacheKeyVersion string
	CacheRedisAddr  string
	CacheRedisUser  string
	CacheRedisPass  string
	CacheRedisDB    int
}

func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Addr:            env("NEKODE_ADDR", DefaultAddr),
		GRPCAddr:        env("NEKODE_GRPC_ADDR", DefaultGRPCAddr),
		DaemonTransport: env("NEKODE_DAEMON_TRANSPORT", "grpc"),
		BaseURL:         env("NEKODE_BASE_URL", DefaultBaseURL),
		DataDir:         env("NEKODE_DATA_DIR", filepath.Join(home, ".nekode")),
		DBType:          env("NEKODE_DB_TYPE", DefaultDBType),
		DBDSN:           strings.TrimSpace(os.Getenv("NEKODE_DB_DSN")),
		LegacyDBPath:    strings.TrimSpace(os.Getenv("NEKODE_DB_PATH")),
		CacheDriver:     env("NEKODE_CACHE_DRIVER", "badger"),
		CacheDir:        strings.TrimSpace(os.Getenv("NEKODE_CACHE_DIR")),
		CacheKeyVersion: env("NEKODE_CACHE_KEY_VERSION", "v1"),
		CacheRedisAddr:  strings.TrimSpace(os.Getenv("NEKODE_CACHE_REDIS_ADDR")),
		CacheRedisUser:  strings.TrimSpace(os.Getenv("NEKODE_CACHE_REDIS_USERNAME")),
		CacheRedisPass:  strings.TrimSpace(os.Getenv("NEKODE_CACHE_REDIS_PASSWORD")),
	}
	cacheTTL, err := durationEnv("NEKODE_CACHE_TTL", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	cfg.CacheTTL = cacheTTL
	cacheRedisDB, err := intEnv("NEKODE_CACHE_REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}
	cfg.CacheRedisDB = cacheRedisDB
	if cfg.DBDSN == "" && cfg.LegacyDBPath != "" {
		cfg.DBDSN = cfg.LegacyDBPath
	}
	if cfg.DBDSN == "" && normalizeDBType(cfg.DBType) == "sqlite" {
		cfg.DBDSN = filepath.Join(cfg.DataDir, "nekode.db")
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = filepath.Join(cfg.DataDir, "cache")
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
	if strings.TrimSpace(c.GRPCAddr) == "" {
		return errors.New("grpc addr is required")
	}
	if normalizeDaemonTransport(c.DaemonTransport) != "grpc" {
		return errors.New("daemon transport must be grpc")
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
	switch normalizeCacheDriver(c.CacheDriver) {
	case "badger":
		if strings.TrimSpace(c.CacheDir) == "" {
			return errors.New("badger cache dir is required")
		}
	case "redis":
		if strings.TrimSpace(c.CacheRedisAddr) == "" {
			return errors.New("redis cache addr is required")
		}
	case "none":
	default:
		return errors.New("cache driver must be badger, redis, or none")
	}
	if c.CacheTTL < 0 {
		return errors.New("cache ttl must not be negative")
	}
	if strings.TrimSpace(c.CacheKeyVersion) == "" {
		return errors.New("cache key version is required")
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

func normalizeCacheDriver(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "badger":
		return "badger"
	case "redis":
		return "redis"
	case "none", "off", "disabled":
		return "none"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeDaemonTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "grpc", "grpc_http2", "grpc-http2":
		return "grpc"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return duration, nil
}

func intEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}

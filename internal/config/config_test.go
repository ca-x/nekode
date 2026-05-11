package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Addr != DefaultAddr {
		t.Fatalf("Addr = %q, want %q", cfg.Addr, DefaultAddr)
	}
	if cfg.DaemonTransport != "connect" {
		t.Fatalf("DaemonTransport = %q, want connect", cfg.DaemonTransport)
	}
	if cfg.BaseURL != DefaultBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, DefaultBaseURL)
	}
	if filepath.Base(cfg.DataDir) != ".nekode" {
		t.Fatalf("DataDir = %q, want .nekode suffix", cfg.DataDir)
	}
	if cfg.DatabaseType() != "sqlite" {
		t.Fatalf("DatabaseType() = %q, want sqlite", cfg.DatabaseType())
	}
	if filepath.Base(cfg.DBDSN) != "nekode.db" {
		t.Fatalf("DBDSN = %q, want nekode.db suffix", cfg.DBDSN)
	}
	if cfg.CacheDriver != "badger" {
		t.Fatalf("CacheDriver = %q, want badger", cfg.CacheDriver)
	}
	if filepath.Base(cfg.CacheDir) != "cache" {
		t.Fatalf("CacheDir = %q, want cache suffix", cfg.CacheDir)
	}
	if cfg.CacheTTL != 5*time.Minute {
		t.Fatalf("CacheTTL = %v, want 5m", cfg.CacheTTL)
	}
	if cfg.BootstrapDisableWeb {
		t.Fatal("BootstrapDisableWeb = true, want false")
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("NEKODE_ADDR", ":19000")
	t.Setenv("NEKODE_DAEMON_RPC_URL", "https://daemon.example.test")
	t.Setenv("NEKODE_DAEMON_TRANSPORT", "connect-rpc")
	t.Setenv("NEKODE_BASE_URL", "https://nekode.example.test")
	t.Setenv("NEKODE_WEB_DIST_DIR", "/srv/nekode/web")
	t.Setenv("NEKODE_DATA_DIR", "/tmp/nekode-test")
	t.Setenv("NEKODE_DB_TYPE", "postgresql")
	t.Setenv("NEKODE_DB_DSN", "postgres://user:pass@localhost/nekode?sslmode=disable")
	t.Setenv("NEKODE_DB_PATH", "")
	t.Setenv("NEKODE_CACHE_DRIVER", "redis")
	t.Setenv("NEKODE_CACHE_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("NEKODE_CACHE_REDIS_DB", "2")
	t.Setenv("NEKODE_CACHE_TTL", "30s")
	t.Setenv("NEKODE_BOOTSTRAP_ADMIN_USERNAME", "root")
	t.Setenv("NEKODE_BOOTSTRAP_ADMIN_PASSWORD", "secret123")
	t.Setenv("NEKODE_BOOTSTRAP_ADMIN_NAME", "Root User")
	t.Setenv("NEKODE_BOOTSTRAP_DISABLE_WEB", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Addr != ":19000" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.DaemonTransport != "connect" {
		t.Fatalf("DaemonTransport = %q", cfg.DaemonTransport)
	}
	if cfg.DaemonRPCURL != "https://daemon.example.test" {
		t.Fatalf("DaemonRPCURL = %q", cfg.DaemonRPCURL)
	}
	if cfg.BaseURL != "https://nekode.example.test" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.WebDistDir != "/srv/nekode/web" {
		t.Fatalf("WebDistDir = %q", cfg.WebDistDir)
	}
	if cfg.DataDir != "/tmp/nekode-test" {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.DatabaseType() != "postgres" {
		t.Fatalf("DatabaseType() = %q", cfg.DatabaseType())
	}
	if cfg.DBDSN != "postgres://user:pass@localhost/nekode?sslmode=disable" {
		t.Fatalf("DBDSN = %q", cfg.DBDSN)
	}
	if cfg.CacheDriver != "redis" || cfg.CacheRedisAddr != "127.0.0.1:6379" || cfg.CacheRedisDB != 2 {
		t.Fatalf("cache config = %+v", cfg)
	}
	if cfg.CacheTTL != 30*time.Second {
		t.Fatalf("CacheTTL = %v", cfg.CacheTTL)
	}
	if cfg.BootstrapAdminUsername != "root" || cfg.BootstrapAdminPassword != "secret123" || cfg.BootstrapAdminName != "Root User" {
		t.Fatalf("bootstrap admin config mismatch: username=%q name=%q password_ok=%t", cfg.BootstrapAdminUsername, cfg.BootstrapAdminName, cfg.BootstrapAdminPassword == "secret123")
	}
	if !cfg.BootstrapDisableWeb {
		t.Fatal("BootstrapDisableWeb = false, want true")
	}
}

func TestLoadLegacyDBPath(t *testing.T) {
	clearEnv(t)
	t.Setenv("NEKODE_DB_TYPE", "")
	t.Setenv("NEKODE_DB_DSN", "")
	t.Setenv("NEKODE_DB_PATH", "/tmp/nekode-test/legacy.db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DBDSN != "/tmp/nekode-test/legacy.db" {
		t.Fatalf("DBDSN = %q", cfg.DBDSN)
	}
}

func TestValidateRejectsBadBaseURL(t *testing.T) {
	cfg := Config{
		Addr:            ":18790",
		DaemonTransport: "connect",
		BaseURL:         "://bad",
		DataDir:         "/tmp/nekode-test",
		DBType:          "sqlite",
		DBDSN:           "/tmp/nekode-test/nekode.db",
		CacheDir:        "/tmp/nekode-test/cache",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"NEKODE_ADDR",
		"NEKODE_DAEMON_RPC_URL",
		"NEKODE_DAEMON_TRANSPORT",
		"NEKODE_BASE_URL",
		"NEKODE_WEB_DIST_DIR",
		"NEKODE_DATA_DIR",
		"NEKODE_DB_TYPE",
		"NEKODE_DB_DSN",
		"NEKODE_DB_PATH",
		"NEKODE_CACHE_DRIVER",
		"NEKODE_CACHE_DIR",
		"NEKODE_CACHE_TTL",
		"NEKODE_CACHE_KEY_VERSION",
		"NEKODE_CACHE_REDIS_ADDR",
		"NEKODE_CACHE_REDIS_USERNAME",
		"NEKODE_CACHE_REDIS_PASSWORD",
		"NEKODE_CACHE_REDIS_DB",
		"NEKODE_BOOTSTRAP_ADMIN_USERNAME",
		"NEKODE_BOOTSTRAP_ADMIN_PASSWORD",
		"NEKODE_BOOTSTRAP_ADMIN_NAME",
		"NEKODE_BOOTSTRAP_DISABLE_WEB",
	} {
		t.Setenv(name, "")
	}
}

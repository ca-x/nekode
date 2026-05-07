package config

import (
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("NEKODE_ADDR", "")
	t.Setenv("NEKODE_BASE_URL", "")
	t.Setenv("NEKODE_DATA_DIR", "")
	t.Setenv("NEKODE_DB_TYPE", "")
	t.Setenv("NEKODE_DB_DSN", "")
	t.Setenv("NEKODE_DB_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Addr != DefaultAddr {
		t.Fatalf("Addr = %q, want %q", cfg.Addr, DefaultAddr)
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
}

func TestLoadFromEnvironment(t *testing.T) {
	t.Setenv("NEKODE_ADDR", ":19000")
	t.Setenv("NEKODE_BASE_URL", "https://nekode.example.test")
	t.Setenv("NEKODE_DATA_DIR", "/tmp/nekode-test")
	t.Setenv("NEKODE_DB_TYPE", "postgresql")
	t.Setenv("NEKODE_DB_DSN", "postgres://user:pass@localhost/nekode?sslmode=disable")
	t.Setenv("NEKODE_DB_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Addr != ":19000" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.BaseURL != "https://nekode.example.test" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
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
}

func TestLoadLegacyDBPath(t *testing.T) {
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
		Addr:    ":18790",
		BaseURL: "://bad",
		DataDir: "/tmp/nekode-test",
		DBType:  "sqlite",
		DBDSN:   "/tmp/nekode-test/nekode.db",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

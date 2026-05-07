package config

import (
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("NEKODE_ADDR", "")
	t.Setenv("NEKODE_BASE_URL", "")
	t.Setenv("NEKODE_DATA_DIR", "")

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
}

func TestLoadFromEnvironment(t *testing.T) {
	t.Setenv("NEKODE_ADDR", ":19000")
	t.Setenv("NEKODE_BASE_URL", "https://nekode.example.test")
	t.Setenv("NEKODE_DATA_DIR", "/tmp/nekode-test")

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
}

func TestValidateRejectsBadBaseURL(t *testing.T) {
	cfg := Config{
		Addr:    ":18790",
		BaseURL: "://bad",
		DataDir: "/tmp/nekode-test",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

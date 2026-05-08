package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigReadsGeneratedInstallConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "daemon.json")
	if err := os.WriteFile(configPath, []byte(`{
		"grpcAddr": "127.0.0.1:19999",
		"token": "generated-token",
		"daemonId": "daemon-test",
		"computerId": "computer-test",
		"displayName": "Test Computer",
		"hostname": "host-test",
		"heartbeatInterval": "5s",
		"agentId": "agent-test",
		"runtimeKind": "codex",
		"target": "#release"
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfig([]string{"--config", configPath, "--once"})
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.GRPCAddr != "127.0.0.1:19999" {
		t.Fatalf("GRPCAddr = %q, want config value", cfg.GRPCAddr)
	}
	if cfg.Token != "generated-token" {
		t.Fatalf("Token = %q, want generated token", cfg.Token)
	}
	if cfg.DaemonID != "daemon-test" || cfg.ComputerID != "computer-test" {
		t.Fatalf("identity = %q/%q, want generated config identity", cfg.DaemonID, cfg.ComputerID)
	}
	if cfg.HeartbeatInterval != 5*time.Second {
		t.Fatalf("HeartbeatInterval = %s, want 5s", cfg.HeartbeatInterval)
	}
	if cfg.RuntimeKind != "codex" {
		t.Fatalf("RuntimeKind = %q, want codex", cfg.RuntimeKind)
	}
	if !cfg.Once {
		t.Fatalf("Once = false, want true")
	}
}

func TestLoadConfigAllowsFlagOverrideForSmoke(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "daemon.json")
	if err := os.WriteFile(configPath, []byte(`{
		"grpcAddr": "127.0.0.1:19999",
		"token": "generated-token",
		"daemonId": "daemon-test",
		"computerId": "computer-test"
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfig([]string{
		"--config", configPath,
		"--grpc-addr", "127.0.0.1:20000",
		"--token", "override-token",
	})
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.GRPCAddr != "127.0.0.1:20000" {
		t.Fatalf("GRPCAddr = %q, want flag override", cfg.GRPCAddr)
	}
	if cfg.Token != "override-token" {
		t.Fatalf("Token = %q, want flag override", cfg.Token)
	}
}

func TestLoadConfigAcceptsServerGRPCAlias(t *testing.T) {
	cfg, err := loadConfig([]string{
		"--server-grpc", "127.0.0.1:20001",
		"--token", "install-token",
		"--daemon-id", "daemon-alias",
		"--computer-id", "computer-alias",
	})
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.GRPCAddr != "127.0.0.1:20001" {
		t.Fatalf("GRPCAddr = %q, want --server-grpc value", cfg.GRPCAddr)
	}
}

func TestFirstConfigPathArgIgnoresOtherFlags(t *testing.T) {
	got := firstConfigPathArg([]string{"--once", "--config", "/tmp/daemon.json"}, "fallback.json")
	if got != "/tmp/daemon.json" {
		t.Fatalf("firstConfigPathArg() = %q, want config value", got)
	}

	got = firstConfigPathArg([]string{"--config=/tmp/daemon-equals.json", "--once"}, "fallback.json")
	if got != "/tmp/daemon-equals.json" {
		t.Fatalf("firstConfigPathArg(equals) = %q, want config value", got)
	}
}

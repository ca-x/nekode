package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ca-x/nekode/internal/runtimeadapter"
)

func TestLoadConfigReadsGeneratedInstallConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "daemon.json")
	if err := os.WriteFile(configPath, []byte(`{
		"serverUrl": "http://127.0.0.1:19999",
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
	if cfg.ServerURL != "http://127.0.0.1:19999" {
		t.Fatalf("ServerURL = %q, want config value", cfg.ServerURL)
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

func TestLoadConfigReadsConnectServerURL(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "daemon.json")
	if err := os.WriteFile(configPath, []byte(`{
		"serverUrl": "https://nekode.example.test",
		"token": "generated-token",
		"daemonId": "daemon-test",
		"computerId": "computer-test"
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfig([]string{"--config", configPath})
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.ServerURL != "https://nekode.example.test" {
		t.Fatalf("ServerURL = %q, want config value", cfg.ServerURL)
	}
}

func TestLoadConfigAcceptsServerURLFlag(t *testing.T) {
	cfg, err := loadConfig([]string{
		"--server-url", "nekode.example.test",
		"--token", "install-token",
		"--daemon-id", "daemon-url",
		"--computer-id", "computer-url",
	})
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.ServerURL != "http://nekode.example.test" {
		t.Fatalf("ServerURL = %q, want normalized flag value", cfg.ServerURL)
	}
}

func TestLoadConfigAllowsFlagOverrideForSmoke(t *testing.T) {
	cfg, err := loadConfig([]string{
		"--server-url", "http://127.0.0.1:20000",
		"--token", "override-token",
	})
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.ServerURL != "http://127.0.0.1:20000" {
		t.Fatalf("ServerURL = %q, want flag override", cfg.ServerURL)
	}
	if cfg.Token != "override-token" {
		t.Fatalf("Token = %q, want flag override", cfg.Token)
	}
}

func TestDaemonInventoryUsesRuntimeAdapterTemplates(t *testing.T) {
	cfg, err := loadConfig([]string{
		"--daemon-id", "daemon-test",
		"--computer-id", "computer-test",
		"--agent-id", "agent-test",
		"--runtime-kind", "codex",
	})
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	session := &daemonSession{cfg: cfg}
	inventory := session.inventory()
	if len(inventory.GetRuntimes()) < 2 || len(inventory.GetRuntimeProfiles()) < 2 {
		t.Fatalf("inventory runtimes/profiles = %d/%d, want catalog runtime types and templates", len(inventory.GetRuntimes()), len(inventory.GetRuntimeProfiles()))
	}
	if len(inventory.GetAgents()) != 1 {
		t.Fatalf("inventory agents = %d, want bootstrap agent only", len(inventory.GetAgents()))
	}
	agent := inventory.GetAgents()[0]
	if agent.GetRuntimeProfileId() != runtimeadapter.DefaultTemplateID("codex") {
		t.Fatalf("agent runtime profile = %q, want codex template", agent.GetRuntimeProfileId())
	}
	for _, profile := range inventory.GetRuntimeProfiles() {
		if profile.GetKind() != "codex" {
			continue
		}
		var adapter runtimeadapter.AdapterConfig
		if err := json.Unmarshal([]byte(profile.GetAdapterConfigJson()), &adapter); err != nil {
			t.Fatalf("decode adapter config: %v", err)
		}
		if adapter.Template.RuntimeKind != "codex" || !adapter.Template.MultiInstance {
			t.Fatalf("adapter template = %+v, want codex multi-instance template", adapter.Template)
		}
		return
	}
	t.Fatal("codex runtime profile not found")
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

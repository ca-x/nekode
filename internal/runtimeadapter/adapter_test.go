package runtimeadapter

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ca-x/nekode/internal/version"
)

func TestComputerInventorySeparatesRuntimeTypesTemplatesAndBootstrapAgent(t *testing.T) {
	inventory := ComputerInventory(InventoryConfig{
		ComputerID:           "computer-1",
		DaemonVersion:        version.Current().Version,
		PreferredRuntimeKind: "codex",
		AgentID:              "agent-1",
		LookupPath: func(command string) (string, error) {
			if command == "codex" {
				return "/usr/local/bin/codex", nil
			}
			return "", errors.New("not found")
		},
		Env: func(string) string { return "" },
	})

	if len(inventory.GetRuntimes()) < 2 {
		t.Fatalf("runtimes = %d, want catalog runtime types", len(inventory.GetRuntimes()))
	}
	if len(inventory.GetRuntimeProfiles()) != len(inventory.GetRuntimes()) {
		t.Fatalf("profiles = %d runtimes = %d, want one template profile per runtime type", len(inventory.GetRuntimeProfiles()), len(inventory.GetRuntimes()))
	}
	if len(inventory.GetAgents()) != 1 {
		t.Fatalf("agents = %d, want one bootstrap agent instance", len(inventory.GetAgents()))
	}

	codexRuntime := inventory.GetRuntimes()[0]
	codexTemplate := inventory.GetRuntimeProfiles()[0]
	for _, runtime := range inventory.GetRuntimes() {
		if runtime.GetKind() == "codex" {
			codexRuntime = runtime
			break
		}
	}
	for _, profile := range inventory.GetRuntimeProfiles() {
		if profile.GetKind() == "codex" {
			codexTemplate = profile
			break
		}
	}
	if codexRuntime.GetRuntimeId() == codexTemplate.GetRuntimeProfileId() {
		t.Fatalf("runtime id and template id both %q; runtime type must not be the agent template", codexRuntime.GetRuntimeId())
	}
	if !codexRuntime.GetInstalled() || !codexRuntime.GetHealthy() {
		t.Fatalf("codex runtime installed/healthy = %v/%v, want true/true", codexRuntime.GetInstalled(), codexRuntime.GetHealthy())
	}
	if got := inventory.GetAgents()[0].GetRuntimeProfileId(); got != codexTemplate.GetRuntimeProfileId() {
		t.Fatalf("bootstrap agent runtime_profile_id = %q, want codex template %q", got, codexTemplate.GetRuntimeProfileId())
	}

	var adapter AdapterConfig
	if err := json.Unmarshal([]byte(codexTemplate.GetAdapterConfigJson()), &adapter); err != nil {
		t.Fatalf("decode adapter config: %v", err)
	}
	if adapter.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %q, want %q", adapter.SchemaVersion, SchemaVersion)
	}
	if adapter.RuntimeType.Kind != "codex" || adapter.Template.RuntimeKind != "codex" {
		t.Fatalf("adapter runtime/template = %+v / %+v, want codex split", adapter.RuntimeType, adapter.Template)
	}
	if !adapter.Template.MultiInstance {
		t.Fatalf("template MultiInstance = false, want true")
	}
	assertOption(t, adapter.Template.Options, "display_name", OptionString, true, false)
	assertOption(t, adapter.Template.Options, "reasoning_effort", OptionEnum, false, false)
	assertOption(t, adapter.Template.Options, "api_token", OptionString, false, true)
	assertOption(t, adapter.Template.Options, "system_message", OptionFreeText, false, false)
	assertOption(t, adapter.Template.Options, "max_turns", OptionNumber, false, false)
	assertOption(t, adapter.Template.Options, "allow_file_write", OptionBoolean, false, false)
	assertOption(t, adapter.Template.Options, "workdir", OptionPath, false, false)
}

func TestBuildWrapCommandValidatesOptions(t *testing.T) {
	template := DefaultInstanceTemplate(RuntimeType{Kind: "codex", DisplayName: "Codex CLI", Command: "codex"})
	cmd, err := BuildWrapCommand(template, map[string]string{
		"display_name":     "Release Bot",
		"model":            "gpt-5.5",
		"reasoning_effort": "high",
		"max_turns":        "12",
		"allow_file_write": "false",
	})
	if err != nil {
		t.Fatalf("BuildWrapCommand() error = %v", err)
	}
	if cmd.Command != "codex" {
		t.Fatalf("command = %q, want codex", cmd.Command)
	}
	if !containsPair(cmd.Args, "--model", "gpt-5.5") || !containsPair(cmd.Args, "--reasoning-effort", "high") {
		t.Fatalf("args = %v, want model and reasoning effort flags", cmd.Args)
	}

	if _, err := BuildWrapCommand(template, map[string]string{
		"display_name":     "Release Bot",
		"reasoning_effort": "extreme",
	}); err == nil {
		t.Fatal("BuildWrapCommand(invalid enum) error = nil, want error")
	}
	if _, err := BuildWrapCommand(template, map[string]string{
		"display_name": "Release Bot",
		"max_turns":    "many",
	}); err == nil {
		t.Fatal("BuildWrapCommand(invalid number) error = nil, want error")
	}
	if _, err := BuildWrapCommand(template, map[string]string{
		"display_name":      "Release Bot",
		"allow_file_write":  "sometimes",
		"reasoning_effort":  "medium",
		"runtime_extra_arg": "ignored",
	}); err == nil {
		t.Fatal("BuildWrapCommand(invalid boolean) error = nil, want error")
	}
}

func TestBuildWrapCommandDefaultsAndTrimsOptionValues(t *testing.T) {
	template := DefaultInstanceTemplate(RuntimeType{Kind: "codex", DisplayName: "Codex CLI", Command: "codex"})
	cmd, err := BuildWrapCommand(template, map[string]string{
		" display_name ":     " Release Bot ",
		" reasoning_effort ": " xhigh ",
		" workdir ":          " /tmp/nekode ",
		" system_message ":   " stay concise ",
		" api_token ":        " secret-token ",
	})
	if err != nil {
		t.Fatalf("BuildWrapCommand() error = %v", err)
	}
	if !containsPair(cmd.Args, "--model", "gpt-5.5") {
		t.Fatalf("args = %v, want default model flag", cmd.Args)
	}
	if !containsPair(cmd.Args, "--reasoning-effort", "xhigh") ||
		!containsPair(cmd.Args, "--workdir", "/tmp/nekode") ||
		!containsPair(cmd.Args, "--system-message", "stay concise") {
		t.Fatalf("args = %v, want trimmed option flags", cmd.Args)
	}
	if containsValue(cmd.Args, "secret-token") {
		t.Fatalf("args = %v, sensitive api_token must not be passed as a CLI argument", cmd.Args)
	}
}

func TestBuildWrapCommandPreservesTemplateWrapAndRejectsInvalidSchema(t *testing.T) {
	template := InstanceTemplate{
		TemplateID:  "template-test",
		RuntimeKind: "test",
		Wrap: WrapSpec{
			Command: "runner",
			Args:    []string{"start", "--json"},
			Env:     []string{"NEKODE_TEST=1"},
		},
		Options: []OptionSchema{
			{Name: "display_name", Type: OptionString, Required: true},
			{Name: "mode", Type: OptionEnum, Default: "fast", Enum: []string{"fast", "slow"}},
			{Name: "enabled", Type: OptionBoolean, Default: "true"},
			{Name: "notes", Type: OptionFreeText},
		},
	}
	cmd, err := BuildWrapCommand(template, map[string]string{
		"display_name": "Runner",
		"notes":        "hello",
	})
	if err != nil {
		t.Fatalf("BuildWrapCommand() error = %v", err)
	}
	if cmd.Command != "runner" || len(cmd.Args) < 2 || cmd.Args[0] != "start" || cmd.Args[1] != "--json" {
		t.Fatalf("wrap command = %+v, want template command and leading args preserved", cmd)
	}
	if len(cmd.Env) != 1 || cmd.Env[0] != "NEKODE_TEST=1" {
		t.Fatalf("env = %v, want template env preserved", cmd.Env)
	}

	missingRequired := template
	if _, err := BuildWrapCommand(missingRequired, map[string]string{"display_name": " "}); err == nil {
		t.Fatal("BuildWrapCommand(missing required after trim) error = nil, want error")
	}
	unsupported := template
	unsupported.Options = append(unsupported.Options, OptionSchema{Name: "mystery", Type: "object"})
	if _, err := BuildWrapCommand(unsupported, map[string]string{"display_name": "Runner", "mystery": "{}"}); err == nil {
		t.Fatal("BuildWrapCommand(unsupported option type) error = nil, want error")
	}
	noCommand := template
	noCommand.Wrap.Command = " "
	if _, err := BuildWrapCommand(noCommand, map[string]string{"display_name": "Runner"}); err == nil {
		t.Fatal("BuildWrapCommand(empty command) error = nil, want error")
	}
}

func assertOption(t *testing.T, options []OptionSchema, name, kind string, required, sensitive bool) {
	t.Helper()
	for _, option := range options {
		if option.Name == name {
			if option.Type != kind || option.Required != required || option.Sensitive != sensitive {
				t.Fatalf("option %s = %+v, want type=%s required=%v sensitive=%v", name, option, kind, required, sensitive)
			}
			if kind == OptionEnum && len(option.Enum) == 0 {
				t.Fatalf("option %s enum is empty", name)
			}
			return
		}
	}
	t.Fatalf("option %s not found in %+v", name, options)
}

func containsPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsValue(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

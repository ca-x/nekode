package runtimeadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/runtimecatalog"
	"google.golang.org/protobuf/proto"
)

const (
	SchemaVersion = "nekode.runtime-adapter.v1"

	OptionString   = "string"
	OptionFreeText = "free_text"
	OptionNumber   = "number"
	OptionBoolean  = "boolean"
	OptionPath     = "path"
	OptionEnum     = "enum"
)

type InventoryConfig struct {
	ComputerID           string
	DaemonVersion        string
	PreferredRuntimeKind string
	AgentID              string
	LookupPath           func(string) (string, error)
	Env                  func(string) string
}

type OptionSchema struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Default     string   `json:"default,omitempty"`
	Sensitive   bool     `json:"sensitive,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description,omitempty"`
}

type WrapSpec struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
}

type InstanceTemplate struct {
	TemplateID     string         `json:"templateId"`
	RuntimeKind    string         `json:"runtimeKind"`
	DisplayName    string         `json:"displayName"`
	Description    string         `json:"description"`
	Capabilities   []string       `json:"capabilities"`
	Options        []OptionSchema `json:"options"`
	Wrap           WrapSpec       `json:"wrap"`
	MultiInstance  bool           `json:"multiInstance"`
	InventoryRole  string         `json:"inventoryRole"`
	AgentIDPattern string         `json:"agentIdPattern"`
}

type RuntimeType struct {
	Kind         string   `json:"kind"`
	DisplayName  string   `json:"displayName"`
	Provider     string   `json:"provider"`
	Command      string   `json:"command"`
	Aliases      []string `json:"aliases,omitempty"`
	Installed    bool     `json:"installed"`
	Healthy      bool     `json:"healthy"`
	ResolvedPath string   `json:"resolvedPath,omitempty"`
	Capabilities []string `json:"capabilities"`
	Templates    []string `json:"templates"`
}

type AdapterConfig struct {
	SchemaVersion string           `json:"schemaVersion"`
	RuntimeType   RuntimeType      `json:"runtimeType"`
	Template      InstanceTemplate `json:"template"`
}

type WrapCommand struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []string `json:"env,omitempty"`
}

func ComputerInventory(cfg InventoryConfig) *daemonv1.ComputerInventory {
	if cfg.LookupPath == nil {
		cfg.LookupPath = exec.LookPath
	}
	if cfg.Env == nil {
		cfg.Env = os.Getenv
	}
	presets := runtimecatalog.List(true, "", 200)
	sort.SliceStable(presets, func(i, j int) bool {
		return presets[i].GetKind() < presets[j].GetKind()
	})

	runtimes := make([]*daemonv1.Runtime, 0, len(presets))
	profiles := make([]*daemonv1.RuntimeProfile, 0, len(presets))
	agents := make([]*daemonv1.AgentProfile, 0, 1)
	preferred := strings.TrimSpace(cfg.PreferredRuntimeKind)
	if preferred == "" {
		preferred = "codex"
	}

	for _, preset := range presets {
		rt := runtimeTypeFromPreset(preset, cfg)
		template := DefaultInstanceTemplate(rt)
		runtimes = append(runtimes, runtimeToProto(cfg.ComputerID, rt, preset))
		profiles = append(profiles, templateToProfile(rt, template, preset))
		if rt.Kind == preferred && strings.TrimSpace(cfg.AgentID) != "" {
			agents = append(agents, bootstrapAgentProfile(cfg, rt, template, preset))
		}
	}
	return &daemonv1.ComputerInventory{
		Runtimes:        runtimes,
		RuntimeProfiles: profiles,
		Agents:          agents,
	}
}

func DefaultTemplateID(kind string) string {
	return "template-" + sanitizeID(kind) + "-default"
}

func DefaultInstanceTemplate(rt RuntimeType) InstanceTemplate {
	command := rt.Command
	if command == "" {
		command = rt.Kind
	}
	return InstanceTemplate{
		TemplateID:    DefaultTemplateID(rt.Kind),
		RuntimeKind:   rt.Kind,
		DisplayName:   rt.DisplayName + " agent",
		Description:   "Create a new agent instance backed by the " + rt.DisplayName + " runtime.",
		Capabilities:  append([]string(nil), rt.Capabilities...),
		MultiInstance: true,
		InventoryRole: "agent_instance_template",
		Wrap: WrapSpec{
			Command: command,
			Args:    []string{"run"},
		},
		AgentIDPattern: rt.Kind + "-{slug}",
		Options: []OptionSchema{
			{Name: "display_name", Label: "Display name", Type: OptionString, Required: true, Default: rt.DisplayName + " Agent", Description: "Human-readable agent name shown in Web and chat."},
			{Name: "model", Label: "Model", Type: OptionString, Default: defaultModel(rt.Kind), Description: "Runtime model identifier; keep as an open string."},
			{Name: "reasoning_effort", Label: "Reasoning effort", Type: OptionEnum, Default: "medium", Enum: []string{"low", "medium", "high", "xhigh"}, Description: "Provider-specific reasoning effort hint."},
			{Name: "workdir", Label: "Working directory", Type: OptionPath, Description: "Workspace directory for the agent process."},
			{Name: "max_turns", Label: "Max turns", Type: OptionNumber, Default: "0", Description: "Optional turn budget; zero means runtime default."},
			{Name: "allow_file_write", Label: "Allow file writes", Type: OptionBoolean, Default: "true", Description: "Whether the instance may edit files when the runtime supports it."},
			{Name: "api_token", Label: "Runtime API token", Type: OptionString, Sensitive: true, Description: "Optional runtime credential stored as a sensitive value."},
			{Name: "system_message", Label: "System message", Type: OptionFreeText, Description: "Optional long-form startup instructions."},
		},
	}
}

func BuildWrapCommand(template InstanceTemplate, values map[string]string) (WrapCommand, error) {
	if strings.TrimSpace(template.Wrap.Command) == "" {
		return WrapCommand{}, errors.New("wrap command is required")
	}
	clean := make(map[string]string, len(values))
	for key, value := range values {
		clean[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	for _, option := range template.Options {
		value := clean[option.Name]
		if value == "" {
			value = option.Default
		}
		if option.Required && value == "" {
			return WrapCommand{}, fmt.Errorf("%s is required", option.Name)
		}
		if err := validateOption(option, value); err != nil {
			return WrapCommand{}, err
		}
	}
	args := make([]string, 0, len(template.Wrap.Args)+8)
	args = append(args, template.Wrap.Args...)
	appendFlag := func(name, value string) {
		if value != "" {
			args = append(args, "--"+name, value)
		}
	}
	appendFlag("model", valueOrDefault(template, clean, "model"))
	appendFlag("reasoning-effort", valueOrDefault(template, clean, "reasoning_effort"))
	appendFlag("workdir", valueOrDefault(template, clean, "workdir"))
	if systemMessage := valueOrDefault(template, clean, "system_message"); systemMessage != "" {
		appendFlag("system-message", systemMessage)
	}
	return WrapCommand{Command: template.Wrap.Command, Args: args, Env: append([]string(nil), template.Wrap.Env...)}, nil
}

func runtimeTypeFromPreset(preset *daemonv1.RuntimePreset, cfg InventoryConfig) RuntimeType {
	command := preset.GetCommand()
	resolved, installed := resolveCommand(command, preset.GetEnvVarNames(), cfg)
	capabilities := capabilityNames(preset.GetCapabilities())
	kind := preset.GetKind()
	displayName := preset.GetDisplayName()
	if displayName == "" {
		displayName = kind
	}
	return RuntimeType{
		Kind:         kind,
		DisplayName:  displayName,
		Provider:     preset.GetProvider(),
		Command:      command,
		Aliases:      append([]string(nil), preset.GetAliases()...),
		Installed:    installed,
		Healthy:      installed,
		ResolvedPath: resolved,
		Capabilities: capabilities,
		Templates:    []string{DefaultTemplateID(kind)},
	}
}

func resolveCommand(command string, envNames []string, cfg InventoryConfig) (string, bool) {
	for _, envName := range envNames {
		if value := strings.TrimSpace(cfg.Env(envName)); value != "" {
			return value, true
		}
	}
	if strings.TrimSpace(command) == "" {
		return "", false
	}
	path, err := cfg.LookupPath(command)
	if err != nil {
		return "", false
	}
	return path, true
}

func runtimeToProto(computerID string, rt RuntimeType, preset *daemonv1.RuntimePreset) *daemonv1.Runtime {
	return &daemonv1.Runtime{
		RuntimeId:           "runtime-" + sanitizeID(computerID) + "-" + sanitizeID(rt.Kind),
		ComputerId:          computerID,
		Kind:                rt.Kind,
		DisplayName:         rt.DisplayName,
		Aliases:             append([]string(nil), rt.Aliases...),
		Tool:                rt.Kind,
		Command:             firstNonEmpty(rt.ResolvedPath, rt.Command),
		Installed:           rt.Installed,
		Healthy:             rt.Healthy,
		SupportsAutoInstall: false,
		InstallHint:         append([]string(nil), preset.GetInstallHint()...),
		ConfigDir:           filepath.Join(".nekode", "agents", rt.Kind),
		Capabilities:        cloneCapabilities(preset.GetCapabilities()),
	}
}

func templateToProfile(rt RuntimeType, template InstanceTemplate, preset *daemonv1.RuntimePreset) *daemonv1.RuntimeProfile {
	configJSON, _ := json.Marshal(AdapterConfig{
		SchemaVersion: SchemaVersion,
		RuntimeType:   rt,
		Template:      template,
	})
	return &daemonv1.RuntimeProfile{
		RuntimeProfileId:  template.TemplateID,
		Kind:              rt.Kind,
		Provider:          rt.Provider,
		Model:             defaultModel(rt.Kind),
		AdapterConfigJson: string(configJSON),
		Capabilities:      cloneCapabilities(preset.GetCapabilities()),
	}
}

func bootstrapAgentProfile(cfg InventoryConfig, rt RuntimeType, template InstanceTemplate, preset *daemonv1.RuntimePreset) *daemonv1.AgentProfile {
	return &daemonv1.AgentProfile{
		AgentId:          cfg.AgentID,
		Name:             cfg.AgentID,
		DisplayName:      cfg.AgentID,
		Enabled:          true,
		Provider:         rt.Provider,
		Model:            defaultModel(rt.Kind),
		ComputerId:       cfg.ComputerID,
		RuntimeProfileId: template.TemplateID,
		RuntimeKind:      rt.Kind,
		DaemonVersion:    cfg.DaemonVersion,
		Status:           daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE,
		Capabilities:     cloneCapabilities(preset.GetCapabilities()),
	}
}

func validateOption(option OptionSchema, value string) error {
	if value == "" {
		return nil
	}
	switch option.Type {
	case OptionEnum:
		for _, item := range option.Enum {
			if value == item {
				return nil
			}
		}
		return fmt.Errorf("%s must be one of %s", option.Name, strings.Join(option.Enum, ", "))
	case OptionNumber:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("%s must be numeric", option.Name)
		}
	case OptionBoolean:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s must be boolean", option.Name)
		}
	case OptionPath, OptionString, OptionFreeText, "":
	default:
		return fmt.Errorf("%s has unsupported option type %q", option.Name, option.Type)
	}
	return nil
}

func valueOrDefault(template InstanceTemplate, values map[string]string, name string) string {
	if value := values[name]; value != "" {
		return value
	}
	for _, option := range template.Options {
		if option.Name == name {
			return option.Default
		}
	}
	return ""
}

func capabilityNames(capabilities []*daemonv1.Capability) []string {
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability.GetEnabled() && capability.GetName() != "" {
			out = append(out, capability.GetName())
		}
	}
	sort.Strings(out)
	return out
}

func cloneCapabilities(capabilities []*daemonv1.Capability) []*daemonv1.Capability {
	out := make([]*daemonv1.Capability, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability == nil {
			continue
		}
		out = append(out, proto.Clone(capability).(*daemonv1.Capability))
	}
	return out
}

func defaultModel(kind string) string {
	switch strings.TrimSpace(kind) {
	case "codex":
		return "gpt-5.5"
	case "claude":
		return "default"
	case "gemini":
		return "default"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sanitizeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}

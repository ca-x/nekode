package runtimeadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
	SmokeRuntime         func(RuntimeSmokeCheck) RuntimeSmokeResult
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
	Kind               string              `json:"kind"`
	DisplayName        string              `json:"displayName"`
	Provider           string              `json:"provider"`
	Command            string              `json:"command"`
	Aliases            []string            `json:"aliases,omitempty"`
	Installed          bool                `json:"installed"`
	Healthy            bool                `json:"healthy"`
	Canonical          bool                `json:"canonical"`
	ResolvedPath       string              `json:"resolvedPath,omitempty"`
	Availability       string              `json:"availability"`
	AvailabilityReason string              `json:"availabilityReason,omitempty"`
	Smoke              RuntimeSmokeResult  `json:"smoke"`
	Contract           RuntimeContractInfo `json:"contract"`
	Capabilities       []string            `json:"capabilities"`
	Templates          []string            `json:"templates"`
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
	Dir     string   `json:"dir,omitempty"`
	Stdin   string   `json:"stdin,omitempty"`
	Kind    string   `json:"kind,omitempty"`
}

type RuntimeSmokeCheck struct {
	Kind        string
	Command     string
	Resolved    string
	VersionArgs []string
	Timeout     time.Duration
}

type RuntimeSmokeResult struct {
	OK       bool   `json:"ok"`
	Status   string `json:"status"`
	Category string `json:"category,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type RuntimeContractInfo struct {
	Kind                  string   `json:"kind"`
	Command               string   `json:"command"`
	Canonical             bool     `json:"canonical"`
	Surface               string   `json:"surface"`
	DirectRunSupported    bool     `json:"directRunSupported"`
	VersionArgs           []string `json:"versionArgs,omitempty"`
	PromptInjection       string   `json:"promptInjection"`
	SystemPromptInjection string   `json:"systemPromptInjection"`
	WorkspaceMapping      string   `json:"workspaceMapping"`
	ModelMapping          string   `json:"modelMapping"`
	ReasoningMapping      string   `json:"reasoningMapping"`
	SessionMapping        string   `json:"sessionMapping"`
	TimeoutPolicy         string   `json:"timeoutPolicy"`
	ExitCodePolicy        string   `json:"exitCodePolicy"`
	OutputPolicy          string   `json:"outputPolicy"`
	SecretRedaction       string   `json:"secretRedaction"`
}

type runtimeContract struct {
	RuntimeContractInfo
	baseArgs func(values map[string]string) ([]string, string, string, []string, error)
}

const (
	availabilityAvailable   = "available"
	availabilityUnavailable = "unavailable"
	availabilityUnsupported = "unsupported"
	availabilityReference   = "reference_only"
)

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
	contract := ContractForKind(rt.Kind)
	wrapArgs := []string{}
	if contract.DirectRunSupported {
		wrapArgs = append([]string(nil), runtimeContractForKind(rt.Kind).defaultArgs()...)
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
			Args:    wrapArgs,
		},
		AgentIDPattern: rt.Kind + "-{slug}",
		Options: []OptionSchema{
			{Name: "display_name", Label: "Display name", Type: OptionString, Required: true, Default: rt.DisplayName + " Agent", Description: "Human-readable agent name shown in Web and chat."},
			{Name: "model", Label: "Model", Type: OptionString, Default: defaultModel(rt.Kind), Description: "Runtime model identifier; keep as an open string."},
			{Name: "reasoning_effort", Label: "Reasoning effort", Type: OptionEnum, Default: "medium", Enum: []string{"low", "medium", "high", "xhigh"}, Description: "Provider-specific reasoning effort hint."},
			{Name: "workdir", Label: "Working directory", Type: OptionPath, Description: "Workspace directory for the agent process."},
			{Name: "session_id", Label: "Session id", Type: OptionString, Description: "Optional provider-specific session/resume identifier."},
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
		if value != "" {
			clean[option.Name] = value
		}
		if option.Required && value == "" {
			return WrapCommand{}, fmt.Errorf("%s is required", option.Name)
		}
		if err := validateOption(option, value); err != nil {
			return WrapCommand{}, err
		}
	}
	contract := runtimeContractForKind(template.RuntimeKind)
	if contract.Kind != "" {
		if !contract.DirectRunSupported {
			return WrapCommand{}, fmt.Errorf("%s runtime is not available for direct execution: %s", template.RuntimeKind, contract.PromptInjection)
		}
		args, dir, stdin, env, err := contract.baseArgs(clean)
		if err != nil {
			return WrapCommand{}, err
		}
		return WrapCommand{
			Command: strings.TrimSpace(template.Wrap.Command),
			Args:    args,
			Env:     append(append([]string(nil), template.Wrap.Env...), env...),
			Dir:     dir,
			Stdin:   stdin,
			Kind:    template.RuntimeKind,
		}, nil
	}
	return WrapCommand{
		Command: strings.TrimSpace(template.Wrap.Command),
		Args:    append([]string(nil), template.Wrap.Args...),
		Env:     append([]string(nil), template.Wrap.Env...),
		Dir:     valueOrDefault(template, clean, "workdir"),
		Kind:    template.RuntimeKind,
	}, nil
}

func runtimeTypeFromPreset(preset *daemonv1.RuntimePreset, cfg InventoryConfig) RuntimeType {
	if cfg.SmokeRuntime == nil {
		cfg.SmokeRuntime = defaultSmokeRuntime
	}
	command := preset.GetCommand()
	resolved, installed := resolveCommand(command, preset.GetEnvVarNames(), cfg)
	capabilities := capabilityNames(preset.GetCapabilities())
	kind := preset.GetKind()
	displayName := preset.GetDisplayName()
	if displayName == "" {
		displayName = kind
	}
	contract := ContractForKind(kind)
	availability := availabilityUnavailable
	reason := "executable not found"
	smoke := RuntimeSmokeResult{OK: false, Status: "not_run", Category: "missing_executable", Detail: reason}
	healthy := false
	if strings.TrimSpace(command) == "" {
		reason = "custom runtime requires an explicit command"
		smoke = RuntimeSmokeResult{OK: false, Status: "not_run", Category: "custom_command_required", Detail: reason}
	} else if installed && !contract.Canonical {
		availability = availabilityReference
		reason = "runtime kind is non-canonical reference-only; proto canonical runtime kinds are codex, claude, opencode, kimi, gemini, and custom"
		smoke = RuntimeSmokeResult{OK: false, Status: "not_run", Category: "non_canonical", Detail: reason}
	} else if installed && !contract.DirectRunSupported {
		availability = availabilityUnsupported
		reason = "direct-run contract is not verified for this runtime kind"
		smoke = RuntimeSmokeResult{OK: false, Status: "not_run", Category: "unsupported_contract", Detail: reason}
	} else if installed {
		smoke = cfg.SmokeRuntime(RuntimeSmokeCheck{
			Kind:        kind,
			Command:     command,
			Resolved:    resolved,
			VersionArgs: append([]string(nil), contract.VersionArgs...),
			Timeout:     5 * time.Second,
		})
		if smoke.OK {
			availability = availabilityAvailable
			reason = ""
			healthy = true
		} else {
			availability = availabilityUnavailable
			reason = firstNonEmpty(smoke.Detail, "executable smoke failed")
		}
	}
	return RuntimeType{
		Kind:               kind,
		DisplayName:        displayName,
		Provider:           preset.GetProvider(),
		Command:            command,
		Aliases:            append([]string(nil), preset.GetAliases()...),
		Installed:          installed,
		Healthy:            healthy,
		Canonical:          contract.Canonical,
		ResolvedPath:       resolved,
		Availability:       availability,
		AvailabilityReason: reason,
		Smoke:              smoke,
		Contract:           contract,
		Capabilities:       capabilities,
		Templates:          []string{DefaultTemplateID(kind)},
	}
}

func defaultSmokeRuntime(check RuntimeSmokeCheck) RuntimeSmokeResult {
	command := firstNonEmpty(check.Resolved, check.Command)
	if strings.TrimSpace(command) == "" {
		return RuntimeSmokeResult{OK: false, Status: "not_run", Category: "missing_executable", Detail: "runtime command is empty"}
	}
	args := check.VersionArgs
	if len(args) == 0 {
		args = []string{"--version"}
	}
	timeout := check.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	detail := strings.TrimSpace(string(output))
	if ctx.Err() == context.DeadlineExceeded {
		return RuntimeSmokeResult{OK: false, Status: "failed", Category: "timeout", Detail: "version smoke timed out"}
	}
	if err != nil {
		if detail != "" {
			detail = err.Error() + ": " + detail
		} else {
			detail = err.Error()
		}
		return RuntimeSmokeResult{OK: false, Status: "failed", Category: classifySmokeFailure(detail), Detail: truncateRuntimeDetail(detail)}
	}
	return RuntimeSmokeResult{OK: true, Status: "passed", Category: "version", Detail: truncateRuntimeDetail(detail)}
}

func classifySmokeFailure(detail string) string {
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "not found"), strings.Contains(lower, "no such file"):
		return "missing_executable"
	case strings.Contains(lower, "unknown option"), strings.Contains(lower, "unknown flag"), strings.Contains(lower, "invalid option"):
		return "argv_contract"
	case strings.Contains(lower, "auth"), strings.Contains(lower, "login"), strings.Contains(lower, "permission denied"), strings.Contains(lower, "unauthorized"):
		return "provider_auth"
	default:
		return "runtime_exit"
	}
}

func truncateRuntimeDetail(value string) string {
	const max = 2048
	if len(value) <= max {
		return value
	}
	return value[:max] + "\n...truncated..."
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

func ContractForKind(kind string) RuntimeContractInfo {
	return runtimeContractForKind(kind).RuntimeContractInfo
}

func runtimeContractForKind(kind string) runtimeContract {
	kind = strings.ToLower(strings.TrimSpace(kind))
	common := RuntimeContractInfo{
		Kind:               kind,
		Surface:            "non-canonical-reference",
		DirectRunSupported: true,
		VersionArgs:        []string{"--version"},
		TimeoutPolicy:      "daemon run timeout",
		ExitCodePolicy:     "non-zero exit code fails the run with stdout/stderr tail",
		OutputPolicy:       "combined stdout/stderr captured and truncated before reporting",
		SecretRedaction:    "prompt/system-prompt values and stdin are redacted from command summaries",
	}
	switch kind {
	case "codex":
		common.Canonical = true
		common.Surface = "proto-canonical"
		common.Command = "codex"
		common.VersionArgs = []string{"exec", "--help"}
		common.PromptInjection = "stdin via `codex exec -`"
		common.SystemPromptInjection = "prepended to stdin because Codex exec has no system prompt flag"
		common.WorkspaceMapping = "`--cd <workdir>` plus process cwd"
		common.ModelMapping = "`--model <model>`"
		common.ReasoningMapping = "unsupported by CLI contract; ignored"
		common.SessionMapping = "ephemeral exec; resume unsupported"
		return runtimeContract{RuntimeContractInfo: common, baseArgs: buildCodexArgs}
	case "claude":
		common.Canonical = true
		common.Surface = "proto-canonical"
		common.Command = "claude"
		common.VersionArgs = []string{"--help"}
		common.PromptInjection = "positional prompt with `--print`"
		common.SystemPromptInjection = "`--append-system-prompt <prompt>`"
		common.WorkspaceMapping = "process cwd"
		common.ModelMapping = "`--model <model>`"
		common.ReasoningMapping = "`--effort <effort>`"
		common.SessionMapping = "`--resume <session_id>`"
		return runtimeContract{RuntimeContractInfo: common, baseArgs: buildClaudeArgs}
	case "opencode":
		common.Canonical = true
		common.Surface = "proto-canonical"
		common.Command = "opencode"
		common.VersionArgs = []string{"run", "--help"}
		common.PromptInjection = "positional message with `opencode run --format json`"
		common.SystemPromptInjection = "`--prompt <prompt>`"
		common.WorkspaceMapping = "`--dir <workdir>` plus process cwd"
		common.ModelMapping = "`--model <model>`"
		common.ReasoningMapping = "`--variant <effort>`"
		common.SessionMapping = "`--session <session_id>`"
		return runtimeContract{RuntimeContractInfo: common, baseArgs: buildOpenCodeArgs}
	case "gemini":
		common.Canonical = true
		common.Surface = "proto-canonical"
		common.Command = "gemini"
		common.VersionArgs = []string{"--help"}
		common.PromptInjection = "stdin one-shot invocation; avoids Windows argv limits for long wake prompts"
		common.SystemPromptInjection = "prepended to stdin because Gemini CLI stream-json path has no separate system prompt flag"
		common.WorkspaceMapping = "process cwd"
		common.ModelMapping = "`-m <model>`"
		common.ReasoningMapping = "unsupported by CLI contract; ignored"
		common.SessionMapping = "`-r <session_id>`"
		return runtimeContract{RuntimeContractInfo: common, baseArgs: buildGeminiArgs}
	case "kimi":
		common.Canonical = true
		common.Surface = "proto-canonical"
		common.Command = "kimi"
		common.DirectRunSupported = false
		common.VersionArgs = []string{"--help"}
		common.PromptInjection = "canonical but unavailable: Kimi uses ACP (`kimi acp`), which needs a protocol adapter rather than the simple process runner"
		common.SystemPromptInjection = "requires ACP session prompt support"
		common.WorkspaceMapping = "requires ACP session/new cwd"
		common.ModelMapping = "requires ACP session/set_model"
		common.ReasoningMapping = "not verified"
		common.SessionMapping = "requires ACP session/resume"
		return runtimeContract{RuntimeContractInfo: common, baseArgs: unsupportedRuntimeArgs}
	case "cursor-agent", "copilot", "openclaw", "hermes", "pi", "kiro-cli":
		common.Command = kind
		common.DirectRunSupported = false
		if kind == "kiro-cli" {
			common.Command = "kiro-cli"
		}
		common.PromptInjection = "non-canonical reference runtime; unavailable until proto/API docs promote it or a separate experimental path is added"
		common.SystemPromptInjection = "not verified"
		common.WorkspaceMapping = "not verified"
		common.ModelMapping = "not verified"
		common.ReasoningMapping = "not verified"
		common.SessionMapping = "not verified"
		return runtimeContract{RuntimeContractInfo: common, baseArgs: unsupportedRuntimeArgs}
	case "custom":
		common.Canonical = true
		common.Surface = "proto-canonical"
		common.Command = ""
		common.DirectRunSupported = false
		common.PromptInjection = "custom runtime requires an explicit user command before it can be smoke-tested"
		common.SystemPromptInjection = "custom"
		common.WorkspaceMapping = "custom"
		common.ModelMapping = "custom"
		common.ReasoningMapping = "custom"
		common.SessionMapping = "custom"
		return runtimeContract{RuntimeContractInfo: common, baseArgs: unsupportedRuntimeArgs}
	default:
		return runtimeContract{}
	}
}

func (c runtimeContract) defaultArgs() []string {
	if c.baseArgs == nil || !c.DirectRunSupported {
		return nil
	}
	args, _, _, _, err := c.baseArgs(map[string]string{})
	if err != nil {
		return nil
	}
	return args
}

func buildCodexArgs(values map[string]string) ([]string, string, string, []string, error) {
	args := []string{"exec", "--json", "--skip-git-repo-check"}
	if model := values["model"]; model != "" {
		args = append(args, "--model", model)
	}
	workdir := values["workdir"]
	if workdir != "" {
		args = append(args, "--cd", workdir)
	}
	args = append(args, "-")
	return args, workdir, values["system_message"], nil, nil
}

func buildClaudeArgs(values map[string]string) ([]string, string, string, []string, error) {
	args := []string{"--print", "--output-format", "stream-json", "--verbose", "--permission-mode", "bypassPermissions"}
	if model := values["model"]; model != "" && model != "default" {
		args = append(args, "--model", model)
	}
	if effort := values["reasoning_effort"]; effort != "" {
		args = append(args, "--effort", effort)
	}
	if sessionID := values["session_id"]; sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	if systemMessage := values["system_message"]; systemMessage != "" {
		args = append(args, "--append-system-prompt", systemMessage)
	}
	return args, values["workdir"], "", nil, nil
}

func buildOpenCodeArgs(values map[string]string) ([]string, string, string, []string, error) {
	args := []string{"run", "--format", "json", "--dangerously-skip-permissions"}
	if model := values["model"]; model != "" {
		args = append(args, "--model", model)
	}
	if effort := values["reasoning_effort"]; effort != "" {
		args = append(args, "--variant", effort)
	}
	workdir := values["workdir"]
	if workdir != "" {
		args = append(args, "--dir", workdir)
	}
	if sessionID := values["session_id"]; sessionID != "" {
		args = append(args, "--session", sessionID)
	}
	if systemMessage := values["system_message"]; systemMessage != "" {
		args = append(args, "--prompt", systemMessage)
	}
	return args, workdir, "", []string{`OPENCODE_PERMISSION={"*":"allow"}`}, nil
}

func buildGeminiArgs(values map[string]string) ([]string, string, string, []string, error) {
	prompt := values["system_message"]
	args := []string{"--yolo", "-o", "stream-json"}
	if model := values["model"]; model != "" {
		args = append(args, "-m", model)
	}
	if sessionID := values["session_id"]; sessionID != "" {
		args = append(args, "-r", sessionID)
	}
	return args, values["workdir"], prompt, nil, nil
}

func unsupportedRuntimeArgs(map[string]string) ([]string, string, string, []string, error) {
	return nil, "", "", nil, errors.New("runtime direct-run contract is not verified")
}

func WithRunPrompt(wrap WrapCommand, prompt string) WrapCommand {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return wrap
	}
	switch strings.ToLower(strings.TrimSpace(wrap.Kind)) {
	case "codex":
		wrap.Stdin = combinePrompt(wrap.Stdin, prompt)
	case "gemini":
		wrap.Stdin = combinePrompt(wrap.Stdin, prompt)
	default:
		wrap.Args = append(wrap.Args, prompt)
	}
	return wrap
}

func combinePrompt(systemPrompt string, prompt string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	prompt = strings.TrimSpace(prompt)
	switch {
	case systemPrompt == "":
		return prompt
	case prompt == "":
		return systemPrompt
	default:
		return systemPrompt + "\n\n---\n\n" + prompt
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

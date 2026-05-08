package runtimecatalog

import (
	"strings"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
)

func List(includeExperimental bool, kindPrefix string, limit uint32) []*daemonv1.RuntimePreset {
	prefix := strings.ToLower(strings.TrimSpace(kindPrefix))
	if limit == 0 || limit > 200 {
		limit = 200
	}
	out := make([]*daemonv1.RuntimePreset, 0, len(presets))
	for _, preset := range presets {
		if !includeExperimental && !preset.GetRecommended() {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(preset.GetKind()), prefix) {
			continue
		}
		out = append(out, clonePreset(preset))
		if uint32(len(out)) >= limit {
			break
		}
	}
	return out
}

func clonePreset(preset *daemonv1.RuntimePreset) *daemonv1.RuntimePreset {
	if preset == nil {
		return nil
	}
	capabilities := make([]*daemonv1.Capability, 0, len(preset.GetCapabilities()))
	for _, capability := range preset.GetCapabilities() {
		if capability == nil {
			continue
		}
		capabilities = append(capabilities, &daemonv1.Capability{
			Name:        capability.GetName(),
			Description: capability.GetDescription(),
			Enabled:     capability.GetEnabled(),
		})
	}
	return &daemonv1.RuntimePreset{
		Kind:             preset.GetKind(),
		DisplayName:      preset.GetDisplayName(),
		Provider:         preset.GetProvider(),
		DefaultModel:     preset.GetDefaultModel(),
		Command:          preset.GetCommand(),
		Aliases:          append([]string(nil), preset.GetAliases()...),
		DefaultArgs:      append([]string(nil), preset.GetDefaultArgs()...),
		EnvVarNames:      append([]string(nil), preset.GetEnvVarNames()...),
		InstallHint:      append([]string(nil), preset.GetInstallHint()...),
		Capabilities:     capabilities,
		SlockSupported:   preset.GetSlockSupported(),
		MulticaSupported: preset.GetMulticaSupported(),
		Recommended:      preset.GetRecommended(),
		Description:      preset.GetDescription(),
	}
}

func capability(name, description string) *daemonv1.Capability {
	return &daemonv1.Capability{Name: name, Description: description, Enabled: true}
}

var commonCodingCapabilities = []*daemonv1.Capability{
	capability("code_execution", "Can inspect and modify code through a local workspace"),
	capability("file_write", "Can write files when runtime policy allows it"),
	capability("shell", "Can run local commands through runtime tooling"),
}

var presets = []*daemonv1.RuntimePreset{
	{
		Kind:             "codex",
		DisplayName:      "Codex CLI",
		Provider:         "openai",
		DefaultModel:     "gpt-5.5",
		Command:          "codex",
		Aliases:          []string{"codex-cli"},
		EnvVarNames:      []string{"NEKODE_CODEX_PATH"},
		InstallHint:      []string{"Install the Codex CLI and ensure `codex` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		SlockSupported:   true,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "OpenAI coding runtime used by Slock and Multica agents.",
	},
	{
		Kind:             "claude",
		DisplayName:      "Claude Code",
		Provider:         "anthropic",
		Command:          "claude",
		Aliases:          []string{"claude-code"},
		EnvVarNames:      []string{"NEKODE_CLAUDE_PATH"},
		InstallHint:      []string{"Install Claude Code and ensure `claude` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		SlockSupported:   true,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "Anthropic coding runtime used by Slock and Multica agents.",
	},
	{
		Kind:             "opencode",
		DisplayName:      "OpenCode",
		Provider:         "opencode",
		Command:          "opencode",
		EnvVarNames:      []string{"NEKODE_OPENCODE_PATH"},
		InstallHint:      []string{"Install OpenCode and ensure `opencode` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		SlockSupported:   true,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "OpenCode CLI runtime with Slock transport compatibility.",
	},
	{
		Kind:             "kimi",
		DisplayName:      "Kimi CLI",
		Provider:         "moonshot",
		Command:          "kimi",
		EnvVarNames:      []string{"NEKODE_KIMI_PATH"},
		InstallHint:      []string{"Install Kimi CLI and ensure `kimi` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		SlockSupported:   true,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "Moonshot Kimi coding runtime supported by Slock and Multica.",
	},
	{
		Kind:             "gemini",
		DisplayName:      "Gemini CLI",
		Provider:         "google",
		Command:          "gemini",
		EnvVarNames:      []string{"NEKODE_GEMINI_PATH"},
		InstallHint:      []string{"Install Gemini CLI and ensure `gemini` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		SlockSupported:   true,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "Google Gemini coding runtime using the shared Slock CLI transport.",
	},
	{
		Kind:             "cursor-agent",
		DisplayName:      "Cursor Agent",
		Provider:         "cursor",
		Command:          "cursor-agent",
		Aliases:          []string{"cursor"},
		EnvVarNames:      []string{"NEKODE_CURSOR_PATH"},
		InstallHint:      []string{"Install Cursor Agent and ensure `cursor-agent` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		SlockSupported:   true,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "Cursor headless agent runtime with dynamic model detection.",
	},
	{
		Kind:             "copilot",
		DisplayName:      "GitHub Copilot CLI",
		Provider:         "github",
		Command:          "copilot",
		EnvVarNames:      []string{"NEKODE_COPILOT_PATH", "NEKODE_COPILOT_MODEL"},
		InstallHint:      []string{"Install GitHub Copilot CLI and ensure `copilot` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "GitHub Copilot CLI runtime from the Multica compatibility catalog.",
	},
	{
		Kind:             "openclaw",
		DisplayName:      "OpenClaw",
		Provider:         "openclaw",
		Command:          "openclaw",
		EnvVarNames:      []string{"NEKODE_OPENCLAW_PATH"},
		InstallHint:      []string{"Install OpenClaw and ensure `openclaw` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "OpenClaw runtime from the Multica compatibility catalog.",
	},
	{
		Kind:             "hermes",
		DisplayName:      "Hermes",
		Provider:         "nous",
		Command:          "hermes",
		EnvVarNames:      []string{"NEKODE_HERMES_PATH"},
		InstallHint:      []string{"Install Hermes and ensure `hermes` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "Hermes runtime from the Multica compatibility catalog.",
	},
	{
		Kind:             "pi",
		DisplayName:      "Pi",
		Provider:         "pi",
		Command:          "pi",
		EnvVarNames:      []string{"NEKODE_PI_PATH"},
		InstallHint:      []string{"Install Pi CLI and ensure `pi` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "Pi runtime from the Multica compatibility catalog.",
	},
	{
		Kind:             "kiro-cli",
		DisplayName:      "Kiro CLI",
		Provider:         "kiro",
		Command:          "kiro-cli",
		Aliases:          []string{"kiro"},
		EnvVarNames:      []string{"NEKODE_KIRO_PATH"},
		InstallHint:      []string{"Install Kiro CLI and ensure `kiro-cli` is on PATH."},
		Capabilities:     commonCodingCapabilities,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "Kiro ACP runtime from the Multica compatibility catalog.",
	},
	{
		Kind:             "custom",
		DisplayName:      "Custom Runtime",
		Provider:         "custom",
		Command:          "",
		Capabilities:     commonCodingCapabilities,
		SlockSupported:   true,
		MulticaSupported: true,
		Recommended:      true,
		Description:      "User-defined runtime adapter. Keep kind/model/provider as open strings.",
	},
}

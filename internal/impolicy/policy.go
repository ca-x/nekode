package impolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	GroupModeMention  = "mention"
	GroupModeAlways   = "always"
	GroupModeDisabled = "disabled"
)

var ErrInvalidPolicy = errors.New("invalid im group policy")

type GroupOverride struct {
	ConversationID   string         `json:"conversation_id"`
	DisplayName      string         `json:"display_name,omitempty"`
	Target           string         `json:"target,omitempty"`
	ThreadID         string         `json:"thread_id,omitempty"`
	GroupMode        string         `json:"group_mode,omitempty"`
	AgentProfileID   string         `json:"agent_profile_id,omitempty"`
	SystemPromptID   string         `json:"system_prompt_id,omitempty"`
	SystemPrompt     string         `json:"system_prompt,omitempty"`
	ToolPolicyID     string         `json:"tool_policy_id,omitempty"`
	ToolPolicy       map[string]any `json:"tool_policy,omitempty"`
	AllowedTools     []string       `json:"allowed_tools,omitempty"`
	DisabledTools    []string       `json:"disabled_tools,omitempty"`
	DefaultTarget    string         `json:"default_target,omitempty"`
	DefaultThreadID  string         `json:"default_thread_id,omitempty"`
	DefaultAgentName string         `json:"default_agent_name,omitempty"`
}

type Config struct {
	GroupMode        string
	DefaultTarget    string
	DefaultThreadID  string
	DefaultAgentName string
	AgentProfileID   string
	SystemPromptID   string
	SystemPrompt     string
	ToolPolicyID     string
	ToolPolicy       map[string]any
	AllowedTools     []string
	DisabledTools    []string
	Groups           []GroupOverride
}

type EffectivePolicy struct {
	EndpointID       string         `json:"endpointId"`
	Provider         string         `json:"provider"`
	ConversationID   string         `json:"conversationId,omitempty"`
	Matched          bool           `json:"matched"`
	GroupMode        string         `json:"groupMode"`
	Target           string         `json:"target,omitempty"`
	ThreadID         string         `json:"threadId,omitempty"`
	AgentProfileID   string         `json:"agentProfileId,omitempty"`
	SystemPromptID   string         `json:"systemPromptId,omitempty"`
	SystemPrompt     string         `json:"systemPrompt,omitempty"`
	ToolPolicyID     string         `json:"toolPolicyId,omitempty"`
	ToolPolicy       map[string]any `json:"toolPolicy,omitempty"`
	AllowedTools     []string       `json:"allowedTools,omitempty"`
	DisabledTools    []string       `json:"disabledTools,omitempty"`
	DefaultAgentName string         `json:"defaultAgentName,omitempty"`
}

func ParseConfig(rawConfig string) (Config, error) {
	rawConfig = strings.TrimSpace(rawConfig)
	if rawConfig == "" {
		rawConfig = "{}"
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawConfig), &raw); err != nil {
		return Config{}, fmt.Errorf("%w: invalid config JSON: %v", ErrInvalidPolicy, err)
	}
	cfg := Config{
		GroupMode: GroupModeMention,
	}
	cfg.GroupMode = strings.ToLower(stringValue(raw, "group_mode", cfg.GroupMode))
	cfg.DefaultTarget = stringValue(raw, "default_target", "")
	cfg.DefaultThreadID = stringValue(raw, "default_thread_id", "")
	cfg.DefaultAgentName = stringValue(raw, "default_agent_name", "")
	cfg.AgentProfileID = stringValue(raw, "agent_profile_id", "")
	cfg.SystemPromptID = stringValue(raw, "system_prompt_id", "")
	cfg.SystemPrompt = stringValue(raw, "system_prompt", "")
	cfg.ToolPolicyID = stringValue(raw, "tool_policy_id", "")
	cfg.AllowedTools = stringSliceValue(raw, "allowed_tools")
	cfg.DisabledTools = stringSliceValue(raw, "disabled_tools")
	if value, ok := raw["tool_policy"]; ok {
		if err := json.Unmarshal(value, &cfg.ToolPolicy); err != nil {
			return Config{}, fmt.Errorf("%w: tool_policy must be an object", ErrInvalidPolicy)
		}
	}
	if value, ok := raw["groups"]; ok {
		groups, err := parseGroups(value)
		if err != nil {
			return Config{}, err
		}
		cfg.Groups = groups
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	if !validGroupMode(cfg.GroupMode) {
		return fmt.Errorf("%w: unsupported group_mode %q", ErrInvalidPolicy, cfg.GroupMode)
	}
	for i, group := range cfg.Groups {
		group = group.Normalize()
		if group.GroupMode != "" && !validGroupMode(group.GroupMode) {
			return fmt.Errorf("%w: groups[%d] unsupported group_mode %q", ErrInvalidPolicy, i, group.GroupMode)
		}
		if group.ConversationID == "" {
			return fmt.Errorf("%w: groups[%d] conversation_id is required", ErrInvalidPolicy, i)
		}
	}
	return nil
}

func Resolve(endpointID, provider, rawConfig, conversationID string) (EffectivePolicy, error) {
	cfg, err := ParseConfig(rawConfig)
	if err != nil {
		return EffectivePolicy{}, err
	}
	conversationID = strings.TrimSpace(conversationID)
	policy := EffectivePolicy{
		EndpointID:       strings.TrimSpace(endpointID),
		Provider:         strings.TrimSpace(provider),
		ConversationID:   conversationID,
		GroupMode:        cfg.GroupMode,
		Target:           cfg.DefaultTarget,
		ThreadID:         cfg.DefaultThreadID,
		AgentProfileID:   cfg.AgentProfileID,
		SystemPromptID:   cfg.SystemPromptID,
		SystemPrompt:     cfg.SystemPrompt,
		ToolPolicyID:     cfg.ToolPolicyID,
		ToolPolicy:       cloneMap(cfg.ToolPolicy),
		AllowedTools:     cleanStrings(cfg.AllowedTools...),
		DisabledTools:    cleanStrings(cfg.DisabledTools...),
		DefaultAgentName: cfg.DefaultAgentName,
	}
	for _, group := range cfg.Groups {
		group = group.Normalize()
		if group.ConversationID != conversationID {
			continue
		}
		policy.Matched = true
		policy.Target = group.Target
		policy.ThreadID = group.ThreadID
		if group.GroupMode != "" {
			policy.GroupMode = group.GroupMode
		}
		if group.AgentProfileID != "" {
			policy.AgentProfileID = group.AgentProfileID
		}
		if group.SystemPromptID != "" {
			policy.SystemPromptID = group.SystemPromptID
		}
		if group.SystemPrompt != "" {
			policy.SystemPrompt = group.SystemPrompt
		}
		if group.ToolPolicyID != "" {
			policy.ToolPolicyID = group.ToolPolicyID
		}
		if len(group.ToolPolicy) > 0 {
			policy.ToolPolicy = cloneMap(group.ToolPolicy)
		}
		if len(group.AllowedTools) > 0 {
			policy.AllowedTools = cleanStrings(group.AllowedTools...)
		}
		if len(group.DisabledTools) > 0 {
			policy.DisabledTools = cleanStrings(group.DisabledTools...)
		}
		if group.DefaultTarget != "" && policy.Target == "" {
			policy.Target = group.DefaultTarget
		}
		if group.DefaultThreadID != "" && policy.ThreadID == "" {
			policy.ThreadID = group.DefaultThreadID
		}
		policy.DefaultAgentName = group.DefaultAgentName
		break
	}
	return policy, nil
}

func (g GroupOverride) Normalize() GroupOverride {
	g.ConversationID = strings.TrimSpace(g.ConversationID)
	g.DisplayName = strings.TrimSpace(g.DisplayName)
	g.Target = strings.TrimSpace(g.Target)
	g.ThreadID = strings.TrimSpace(g.ThreadID)
	g.GroupMode = strings.TrimSpace(strings.ToLower(g.GroupMode))
	g.AgentProfileID = strings.TrimSpace(g.AgentProfileID)
	g.SystemPromptID = strings.TrimSpace(g.SystemPromptID)
	g.SystemPrompt = strings.TrimSpace(g.SystemPrompt)
	g.ToolPolicyID = strings.TrimSpace(g.ToolPolicyID)
	g.AllowedTools = cleanStrings(g.AllowedTools...)
	g.DisabledTools = cleanStrings(g.DisabledTools...)
	g.DefaultTarget = strings.TrimSpace(g.DefaultTarget)
	g.DefaultThreadID = strings.TrimSpace(g.DefaultThreadID)
	g.DefaultAgentName = strings.TrimSpace(g.DefaultAgentName)
	return g
}

func parseGroups(raw json.RawMessage) ([]GroupOverride, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] == '[' {
		var groups []GroupOverride
		if err := json.Unmarshal(raw, &groups); err != nil {
			return nil, fmt.Errorf("%w: groups must be an array or object", ErrInvalidPolicy)
		}
		return normalizeGroups(groups), nil
	}
	var keyed map[string]GroupOverride
	if err := json.Unmarshal(raw, &keyed); err != nil {
		return nil, fmt.Errorf("%w: groups must be an array or object", ErrInvalidPolicy)
	}
	groups := make([]GroupOverride, 0, len(keyed))
	for conversationID, group := range keyed {
		if strings.TrimSpace(group.ConversationID) == "" {
			group.ConversationID = conversationID
		}
		groups = append(groups, group)
	}
	return normalizeGroups(groups), nil
}

func normalizeGroups(groups []GroupOverride) []GroupOverride {
	out := make([]GroupOverride, 0, len(groups))
	for _, group := range groups {
		out = append(out, group.Normalize())
	}
	return out
}

func stringValue(raw map[string]json.RawMessage, key, fallback string) string {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	var out string
	if err := json.Unmarshal(value, &out); err != nil {
		return fallback
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return fallback
	}
	return out
}

func stringSliceValue(raw map[string]json.RawMessage, key string) []string {
	value, ok := raw[key]
	if !ok {
		return nil
	}
	var values []string
	if err := json.Unmarshal(value, &values); err != nil {
		return nil
	}
	return cleanStrings(values...)
}

func validGroupMode(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", GroupModeMention, GroupModeAlways, GroupModeDisabled:
		return true
	default:
		return false
	}
}

func cleanStrings(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func cloneMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

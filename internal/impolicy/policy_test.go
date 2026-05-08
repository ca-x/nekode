package impolicy

import (
	"errors"
	"testing"
)

func TestResolveGroupOverrideFromKeyedGroups(t *testing.T) {
	policy, err := Resolve("iep-feishu", "feishu", `{
		"group_mode":"mention",
		"agent_profile_id":"agent-default",
		"system_prompt_id":"prompt-default",
		"tool_policy_id":"tools-default",
		"allowed_tools":["search","search","shell"],
		"groups":{
			"oc_123":{
				"target":"#ops",
				"thread_id":"thread-1",
				"group_mode":"always",
				"agent_profile_id":"agent-ops",
				"system_prompt":"You are on-call.",
				"tool_policy":{"allow":["search"]},
				"disabled_tools":["shell"]
			}
		}
	}`, "oc_123")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !policy.Matched || policy.GroupMode != GroupModeAlways || policy.Target != "#ops" || policy.ThreadID != "thread-1" {
		t.Fatalf("policy routing = %+v", policy)
	}
	if policy.AgentProfileID != "agent-ops" || policy.SystemPrompt != "You are on-call." || policy.ToolPolicyID != "tools-default" {
		t.Fatalf("policy overrides = %+v", policy)
	}
	if len(policy.AllowedTools) != 2 || policy.AllowedTools[0] != "search" || policy.AllowedTools[1] != "shell" {
		t.Fatalf("allowed tools = %+v", policy.AllowedTools)
	}
	if len(policy.DisabledTools) != 1 || policy.DisabledTools[0] != "shell" {
		t.Fatalf("disabled tools = %+v", policy.DisabledTools)
	}
}

func TestResolveFallsBackToEndpointDefaults(t *testing.T) {
	policy, err := Resolve("iep-telegram", "telegram", `{
		"group_mode":"disabled",
		"default_target":"#general",
		"default_agent_name":"assistant",
		"agent_profile_id":"agent-default",
		"allowed_tools":["search"]
	}`, "chat-unknown")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if policy.Matched || policy.GroupMode != GroupModeDisabled || policy.AgentProfileID != "agent-default" {
		t.Fatalf("fallback policy = %+v", policy)
	}
	if policy.Target != "#general" || policy.DefaultAgentName != "assistant" {
		t.Fatalf("fallback route = %+v", policy)
	}
}

func TestParseArrayGroups(t *testing.T) {
	policy, err := Resolve("iep-terminal", "terminal", `{
		"groups":[{"conversation_id":"session-1","group_mode":"mention","default_agent_name":"assistant"}]
	}`, "session-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !policy.Matched || policy.DefaultAgentName != "assistant" || policy.GroupMode != GroupModeMention {
		t.Fatalf("array group policy = %+v", policy)
	}
}

func TestValidateRejectsInvalidGroupMode(t *testing.T) {
	if _, err := ParseConfig(`{"groups":{"oc_1":{"group_mode":"loud"}}}`); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("ParseConfig() error = %v, want ErrInvalidPolicy", err)
	}
}

package imadapter

import (
	"strings"
	"testing"
)

func TestInteractionPlannerChooseAction(t *testing.T) {
	planner := NewInteractionPlanner()

	// Test Telegram: should use rich inline buttons
	telegramPlan, err := planner.PlanChooseAction(ProviderTelegram, ChooseActionRequest{
		Prompt:  "Choose an action:",
		Actions: []string{"Approve", "Reject", "Review"},
	})
	if err != nil {
		t.Fatalf("PlanChooseAction(telegram) error = %v", err)
	}
	if telegramPlan.RenderMode != "rich" {
		t.Fatalf("telegram should use rich mode, got %q", telegramPlan.RenderMode)
	}
	if telegramPlan.RichContent == nil || telegramPlan.RichContent.Type != "inline_buttons" {
		t.Fatalf("telegram should have inline_buttons content")
	}

	// Test Weixin: should fall back to numbered list
	weixinPlan, err := planner.PlanChooseAction(ProviderWeixin, ChooseActionRequest{
		Prompt:  "Choose an action:",
		Actions: []string{"Approve", "Reject"},
	})
	if err != nil {
		t.Fatalf("PlanChooseAction(weixin) error = %v", err)
	}
	if weixinPlan.RenderMode != "fallback" {
		t.Fatalf("weixin should use fallback mode, got %q", weixinPlan.RenderMode)
	}
	if !strings.Contains(weixinPlan.FallbackText, "1. Approve") {
		t.Fatalf("weixin fallback should have numbered list, got %q", weixinPlan.FallbackText)
	}

	// Test unknown provider
	_, err = planner.PlanChooseAction("unknown", ChooseActionRequest{})
	if err == nil {
		t.Fatalf("PlanChooseAction(unknown) should error")
	}
}

func TestInteractionPlannerRequestConfig(t *testing.T) {
	planner := NewInteractionPlanner()

	// All providers should fall back to plain text for config requests
	for _, provider := range []string{ProviderTelegram, ProviderWeixin, ProviderServerChan} {
		plan, err := planner.PlanRequestConfig(provider, RequestConfigRequest{
			Prompt: "Please provide configuration:",
			Fields: []string{"api_key", "endpoint"},
			Examples: map[string]string{
				"api_key":  "sk-...",
				"endpoint": "https://api.example.com",
			},
		})
		if err != nil {
			t.Fatalf("PlanRequestConfig(%s) error = %v", provider, err)
		}
		if plan.RenderMode != "fallback" {
			t.Fatalf("%s should use fallback mode for config, got %q", provider, plan.RenderMode)
		}
		if !strings.Contains(plan.FallbackText, "api_key") {
			t.Fatalf("%s fallback should mention api_key", provider)
		}
	}
}

func TestInteractionPlannerAcknowledgeStatus(t *testing.T) {
	planner := NewInteractionPlanner()

	// Test Telegram with edit support: should use rich status card
	telegramPlan, err := planner.PlanAcknowledgeStatus(ProviderTelegram, AcknowledgeStatusRequest{
		RunID:    "run-123",
		Status:   "running",
		Message:  "Processing...",
		CanEdit:  true,
	})
	if err != nil {
		t.Fatalf("PlanAcknowledgeStatus(telegram) error = %v", err)
	}
	if telegramPlan.RenderMode != "rich" {
		t.Fatalf("telegram with edit should use rich mode, got %q", telegramPlan.RenderMode)
	}

	// Test Telegram without edit support: should fall back to text
	telegramPlanNoEdit, err := planner.PlanAcknowledgeStatus(ProviderTelegram, AcknowledgeStatusRequest{
		RunID:    "run-123",
		Status:   "running",
		Message:  "Processing...",
		CanEdit:  false,
	})
	if err != nil {
		t.Fatalf("PlanAcknowledgeStatus(telegram no edit) error = %v", err)
	}
	if telegramPlanNoEdit.RenderMode != "fallback" {
		t.Fatalf("telegram without edit should use fallback mode, got %q", telegramPlanNoEdit.RenderMode)
	}
	if !strings.Contains(telegramPlanNoEdit.FallbackText, "run-123") {
		t.Fatalf("fallback should include run ID")
	}

	// Test Weixin: should always fall back to text
	weixinPlan, err := planner.PlanAcknowledgeStatus(ProviderWeixin, AcknowledgeStatusRequest{
		RunID:    "run-456",
		Status:   "done",
		Message:  "Completed",
		CanEdit:  true,
	})
	if err != nil {
		t.Fatalf("PlanAcknowledgeStatus(weixin) error = %v", err)
	}
	if weixinPlan.RenderMode != "fallback" {
		t.Fatalf("weixin should use fallback mode, got %q", weixinPlan.RenderMode)
	}
}

func TestInteractionPlannerMentionAgent(t *testing.T) {
	planner := NewInteractionPlanner()

	// Test Telegram in group: should use rich mention
	telegramGroupPlan, err := planner.PlanMentionAgent(ProviderTelegram, MentionAgentRequest{
		AgentName: "bot-assistant",
		ChannelID: "chat-123",
		IsGroup:   true,
	})
	if err != nil {
		t.Fatalf("PlanMentionAgent(telegram group) error = %v", err)
	}
	if telegramGroupPlan.RenderMode != "rich" {
		t.Fatalf("telegram group should use rich mode, got %q", telegramGroupPlan.RenderMode)
	}

	// Test Telegram in DM: should fall back to text
	telegramDMPlan, err := planner.PlanMentionAgent(ProviderTelegram, MentionAgentRequest{
		AgentName: "bot-assistant",
		ChannelID: "user-123",
		IsGroup:   false,
	})
	if err != nil {
		t.Fatalf("PlanMentionAgent(telegram dm) error = %v", err)
	}
	if telegramDMPlan.RenderMode != "fallback" {
		t.Fatalf("telegram dm should use fallback mode, got %q", telegramDMPlan.RenderMode)
	}
	if !strings.Contains(telegramDMPlan.FallbackText, "bot-assistant") {
		t.Fatalf("fallback should mention agent name")
	}

	// Test Weixin: should always fall back to text
	weixinPlan, err := planner.PlanMentionAgent(ProviderWeixin, MentionAgentRequest{
		AgentName: "bot-assistant",
		ChannelID: "chat-456",
		IsGroup:   true,
	})
	if err != nil {
		t.Fatalf("PlanMentionAgent(weixin) error = %v", err)
	}
	if weixinPlan.RenderMode != "fallback" {
		t.Fatalf("weixin should use fallback mode, got %q", weixinPlan.RenderMode)
	}
}

func TestInteractionPlannerSendTaskAction(t *testing.T) {
	planner := NewInteractionPlanner()

	// Test Telegram: should use rich command
	telegramPlan, err := planner.PlanSendTaskAction(ProviderTelegram, SendTaskActionRequest{
		TaskID:   "task-123",
		Action:   "claim",
		Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatalf("PlanSendTaskAction(telegram) error = %v", err)
	}
	if telegramPlan.RenderMode != "rich" {
		t.Fatalf("telegram should use rich mode, got %q", telegramPlan.RenderMode)
	}

	// Test Weixin: should fall back to text command
	weixinPlan, err := planner.PlanSendTaskAction(ProviderWeixin, SendTaskActionRequest{
		TaskID:   "task-456",
		Action:   "status",
		Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatalf("PlanSendTaskAction(weixin) error = %v", err)
	}
	if weixinPlan.RenderMode != "fallback" {
		t.Fatalf("weixin should use fallback mode, got %q", weixinPlan.RenderMode)
	}
	if !strings.Contains(weixinPlan.FallbackText, "/task") {
		t.Fatalf("fallback should use /task command format")
	}
}

func TestInteractionPlannerConsistency(t *testing.T) {
	planner := NewInteractionPlanner()

	// Verify all providers are supported
	for _, schema := range ListProviders() {
		_, err := planner.PlanChooseAction(schema.Provider, ChooseActionRequest{
			Prompt:  "Test",
			Actions: []string{"A", "B"},
		})
		if err != nil {
			t.Fatalf("planner should support provider %q, got error: %v", schema.Provider, err)
		}
	}
}

func TestInteractionPlannerFallbackQuality(t *testing.T) {
	planner := NewInteractionPlanner()

	// Verify fallback text is well-formatted and readable
	plan, _ := planner.PlanChooseAction(ProviderWeixin, ChooseActionRequest{
		Prompt:  "What would you like to do?",
		Actions: []string{"Start", "Stop", "Pause"},
	})

	if !strings.Contains(plan.FallbackText, "What would you like to do?") {
		t.Fatalf("fallback should include prompt")
	}
	if !strings.Contains(plan.FallbackText, "1. Start") {
		t.Fatalf("fallback should have numbered items")
	}
	if !strings.Contains(plan.FallbackText, "2. Stop") {
		t.Fatalf("fallback should have all actions")
	}
}

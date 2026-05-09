package imadapter

import (
	"fmt"
	"strings"
)

// InteractionIntent describes what kind of interaction is desired.
type InteractionIntent string

const (
	IntentChooseAction      InteractionIntent = "choose_action"
	IntentRequestConfig     InteractionIntent = "request_config"
	IntentAcknowledgeStatus InteractionIntent = "acknowledge_status"
	IntentMentionAgent      InteractionIntent = "mention_agent"
	IntentSendTaskAction    InteractionIntent = "send_task_action"
)

// InteractionPlan describes how to render an interaction for a specific provider.
type InteractionPlan struct {
	Intent       InteractionIntent `json:"intent"`
	Provider     string            `json:"provider"`
	RenderMode   string            `json:"renderMode"` // "rich" or "fallback"
	RichContent  *RichContent      `json:"richContent,omitempty"`
	FallbackText string            `json:"fallbackText"`
}

// RichContent describes rich interaction elements (buttons, cards, etc).
type RichContent struct {
	Type     string        `json:"type"` // "inline_buttons", "form", "status_card", "mention", "command"
	Elements []interface{} `json:"elements,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ChooseActionRequest describes a request to choose one action.
type ChooseActionRequest struct {
	Prompt  string
	Actions []string // e.g., ["Approve", "Reject", "Review"]
}

// RequestConfigRequest describes a request for missing configuration.
type RequestConfigRequest struct {
	Prompt       string
	Fields       []string // e.g., ["api_key", "endpoint"]
	Examples     map[string]string
}

// AcknowledgeStatusRequest describes a status acknowledgment.
type AcknowledgeStatusRequest struct {
	RunID     string
	Status    string // "running", "done", "failed"
	Message   string
	CanEdit   bool // whether the provider supports message editing
}

// MentionAgentRequest describes a mention request.
type MentionAgentRequest struct {
	AgentName string
	ChannelID string
	IsGroup   bool
}

// SendTaskActionRequest describes a task action request.
type SendTaskActionRequest struct {
	TaskID   string
	Action   string // "claim", "status", "update"
	Metadata map[string]any
}

// InteractionPlanner plans how to render interactions based on provider capabilities.
type InteractionPlanner struct {
	capabilities map[string]*InteractionCapabilities
}

// NewInteractionPlanner creates a new planner with provider capabilities.
func NewInteractionPlanner() *InteractionPlanner {
	capabilities := make(map[string]*InteractionCapabilities)
	for _, schema := range ListProviders() {
		if schema.InteractionCapabilities != nil {
			capabilities[schema.Provider] = schema.InteractionCapabilities
		}
	}
	return &InteractionPlanner{
		capabilities: capabilities,
	}
}

// PlanChooseAction plans how to render a choose-action interaction.
func (p *InteractionPlanner) PlanChooseAction(provider string, req ChooseActionRequest) (*InteractionPlan, error) {
	caps, ok := p.capabilities[CanonicalProvider(provider)]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}

	plan := &InteractionPlan{
		Intent:   IntentChooseAction,
		Provider: CanonicalProvider(provider),
	}

	// Check if provider supports inline buttons
	if caps.InlineButtons != nil && inScope(caps.InlineButtons.Scope, "dm,group") {
		plan.RenderMode = "rich"
		plan.RichContent = &RichContent{
			Type: "inline_buttons",
			Elements: toButtonElements(req.Actions),
			Metadata: map[string]any{
				"prompt": req.Prompt,
			},
		}
	} else {
		// Fallback to numbered text list
		plan.RenderMode = "fallback"
		plan.FallbackText = formatNumberedList(req.Prompt, req.Actions)
	}

	return plan, nil
}

// PlanRequestConfig plans how to render a config request interaction.
func (p *InteractionPlanner) PlanRequestConfig(provider string, req RequestConfigRequest) (*InteractionPlan, error) {
	_, ok := p.capabilities[CanonicalProvider(provider)]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}

	plan := &InteractionPlan{
		Intent:   IntentRequestConfig,
		Provider: CanonicalProvider(provider),
	}

	// For now, all providers fall back to plain text config requests
	// (Rich form support would require provider-specific card/form APIs)
	plan.RenderMode = "fallback"
	plan.FallbackText = formatConfigRequest(req.Prompt, req.Fields, req.Examples)

	return plan, nil
}

// PlanAcknowledgeStatus plans how to render a status acknowledgment.
func (p *InteractionPlanner) PlanAcknowledgeStatus(provider string, req AcknowledgeStatusRequest) (*InteractionPlan, error) {
	caps, ok := p.capabilities[CanonicalProvider(provider)]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}

	plan := &InteractionPlan{
		Intent:   IntentAcknowledgeStatus,
		Provider: CanonicalProvider(provider),
	}

	// Check if provider supports message edit for status updates
	if caps.MessageEdit != nil && inScope(caps.MessageEdit.Scope, "dm,group") && req.CanEdit {
		plan.RenderMode = "rich"
		plan.RichContent = &RichContent{
			Type: "status_card",
			Metadata: map[string]any{
				"run_id":  req.RunID,
				"status":  req.Status,
				"message": req.Message,
				"editable": true,
			},
		}
	} else {
		// Fallback to plain text status line
		plan.RenderMode = "fallback"
		plan.FallbackText = formatStatusLine(req.RunID, req.Status, req.Message)
	}

	return plan, nil
}

// PlanMentionAgent plans how to render an agent mention.
func (p *InteractionPlanner) PlanMentionAgent(provider string, req MentionAgentRequest) (*InteractionPlan, error) {
	caps, ok := p.capabilities[CanonicalProvider(provider)]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}

	plan := &InteractionPlan{
		Intent:   IntentMentionAgent,
		Provider: CanonicalProvider(provider),
	}

	// Check if provider supports native mentions in groups
	if req.IsGroup && caps.Mentions != nil && inScope(caps.Mentions.Scope, "group") {
		plan.RenderMode = "rich"
		plan.RichContent = &RichContent{
			Type: "mention",
			Metadata: map[string]any{
				"agent_name": req.AgentName,
				"channel_id": req.ChannelID,
			},
		}
	} else {
		// Fallback to plain text mention
		plan.RenderMode = "fallback"
		plan.FallbackText = fmt.Sprintf("@%s", req.AgentName)
	}

	return plan, nil
}

// PlanSendTaskAction plans how to render a task action.
func (p *InteractionPlanner) PlanSendTaskAction(provider string, req SendTaskActionRequest) (*InteractionPlan, error) {
	caps, ok := p.capabilities[CanonicalProvider(provider)]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}

	plan := &InteractionPlan{
		Intent:   IntentSendTaskAction,
		Provider: CanonicalProvider(provider),
	}

	// Check if provider supports native commands
	if caps.NativeCommands != nil && inScope(caps.NativeCommands.Scope, "all") {
		plan.RenderMode = "rich"
		plan.RichContent = &RichContent{
			Type: "command",
			Metadata: map[string]any{
				"task_id": req.TaskID,
				"action":  req.Action,
			},
		}
	} else {
		// Fallback to text command format
		plan.RenderMode = "fallback"
		plan.FallbackText = formatTaskCommand(req.TaskID, req.Action)
	}

	return plan, nil
}

// Helper functions

func inScope(scope Scope, targets string) bool {
	if scope == ScopeAll {
		return true
	}
	scopeStr := string(scope)
	// Handle comma-separated scopes like "dm,group"
	for _, scopeItem := range strings.Split(scopeStr, ",") {
		scopeItem = strings.TrimSpace(scopeItem)
		for _, target := range strings.Split(targets, ",") {
			target = strings.TrimSpace(target)
			if scopeItem == target {
				return true
			}
		}
	}
	return false
}

func toButtonElements(actions []string) []interface{} {
	elements := make([]interface{}, len(actions))
	for i, action := range actions {
		elements[i] = map[string]string{
			"label":    action,
			"callback": fmt.Sprintf("action_%d", i),
		}
	}
	return elements
}

func formatNumberedList(prompt string, items []string) string {
	var sb strings.Builder
	sb.WriteString(prompt)
	sb.WriteString("\n")
	for i, item := range items {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, item))
	}
	return sb.String()
}

func formatConfigRequest(prompt string, fields []string, examples map[string]string) string {
	var sb strings.Builder
	sb.WriteString(prompt)
	sb.WriteString("\n\nRequired fields:\n")
	for _, field := range fields {
		sb.WriteString(fmt.Sprintf("- %s", field))
		if example, ok := examples[field]; ok {
			sb.WriteString(fmt.Sprintf(" (example: %s)", example))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatStatusLine(runID, status, message string) string {
	return fmt.Sprintf("[%s] %s: %s", runID, strings.ToUpper(status), message)
}

func formatTaskCommand(taskID, action string) string {
	return fmt.Sprintf("/task %s %s", action, taskID)
}

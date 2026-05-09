package imadapter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildActionButtons(t *testing.T) {
	// Test with 2 actions (single row)
	buttons := BuildActionButtons([]string{"Approve", "Reject"})
	if len(buttons) != 1 {
		t.Fatalf("expected 1 row, got %d", len(buttons))
	}
	if len(buttons[0]) != 2 {
		t.Fatalf("expected 2 buttons in row, got %d", len(buttons[0]))
	}
	if buttons[0][0].Text != "Approve" || buttons[0][0].CallbackData != "action_0" {
		t.Fatalf("first button incorrect: %+v", buttons[0][0])
	}

	// Test with 4 actions (2 rows)
	buttons = BuildActionButtons([]string{"A", "B", "C", "D"})
	if len(buttons) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(buttons))
	}
	if len(buttons[0]) != 2 || len(buttons[1]) != 2 {
		t.Fatalf("expected 2 buttons per row")
	}

	// Test with 5 actions (3 rows, last row has 1 button)
	buttons = BuildActionButtons([]string{"A", "B", "C", "D", "E"})
	if len(buttons) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(buttons))
	}
	if len(buttons[2]) != 1 {
		t.Fatalf("expected 1 button in last row, got %d", len(buttons[2]))
	}

	// Test with empty actions
	buttons = BuildActionButtons([]string{})
	if buttons != nil {
		t.Fatalf("expected nil for empty actions")
	}
}

func TestBuildTaskCommandButtons(t *testing.T) {
	buttons := BuildTaskCommandButtons("task-123")
	if len(buttons) != 1 {
		t.Fatalf("expected 1 row, got %d", len(buttons))
	}
	if len(buttons[0]) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(buttons[0]))
	}

	if !strings.Contains(buttons[0][0].CallbackData, "task-123") {
		t.Fatalf("callback data should contain task ID")
	}
	if !strings.Contains(buttons[0][1].CallbackData, "task-123") {
		t.Fatalf("callback data should contain task ID")
	}
}

func TestParseTelegramCallbackData(t *testing.T) {
	tests := []struct {
		data       string
		wantType   string
		wantParams map[string]string
	}{
		{
			data:     "action_0",
			wantType: "action",
			wantParams: map[string]string{
				"index": "0",
			},
		},
		{
			data:     "task_claim_task-123",
			wantType: "task",
			wantParams: map[string]string{
				"command": "claim",
				"task_id": "task-123",
			},
		},
		{
			data:     "task_status_task-456",
			wantType: "task",
			wantParams: map[string]string{
				"command": "status",
				"task_id": "task-456",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.data, func(t *testing.T) {
			actionType, params := ParseTelegramCallbackData(tt.data)
			if actionType != tt.wantType {
				t.Fatalf("action type = %q, want %q", actionType, tt.wantType)
			}
			for key, wantVal := range tt.wantParams {
				if params[key] != wantVal {
					t.Fatalf("param %s = %q, want %q", key, params[key], wantVal)
				}
			}
		})
	}
}

func TestRenderTelegramRichOutbound(t *testing.T) {
	input := OutboundRenderInput{
		Provider:       ProviderTelegram,
		ConversationID: "123456",
		Text:           "Choose an action:",
	}

	buttons := BuildActionButtons([]string{"Approve", "Reject"})
	frames := RenderTelegramRichOutbound(input, buttons)

	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}

	frame := frames[0]
	if frame.Provider != ProviderTelegram {
		t.Fatalf("provider = %q, want telegram", frame.Provider)
	}
	if frame.TargetID != "123456" {
		t.Fatalf("target ID = %q, want 123456", frame.TargetID)
	}
	if frame.Text != "Choose an action:" {
		t.Fatalf("text = %q, want 'Choose an action:'", frame.Text)
	}

	// Verify reply_markup is in metadata
	if frame.Metadata == nil {
		t.Fatalf("metadata should not be nil")
	}
	replyMarkup, ok := frame.Metadata["reply_markup"]
	if !ok {
		t.Fatalf("reply_markup not in metadata")
	}

	// Verify it's valid JSON
	var keyboard TelegramInlineKeyboard
	if err := json.Unmarshal([]byte(replyMarkup.(string)), &keyboard); err != nil {
		t.Fatalf("reply_markup JSON invalid: %v", err)
	}
	if len(keyboard.InlineKeyboard) != 1 || len(keyboard.InlineKeyboard[0]) != 2 {
		t.Fatalf("keyboard structure incorrect")
	}
}

func TestRenderTelegramRichOutboundWithoutButtons(t *testing.T) {
	input := OutboundRenderInput{
		Provider:       ProviderTelegram,
		ConversationID: "123456",
		Text:           "Simple message",
	}

	frames := RenderTelegramRichOutbound(input, nil)

	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}

	frame := frames[0]
	if frame.Metadata != nil && frame.Metadata["reply_markup"] != nil {
		t.Fatalf("should not have reply_markup when buttons are nil")
	}
}

func TestValidateTelegramCallbackQuery(t *testing.T) {
	validQuery := TelegramCallbackQuery{
		ID:   "callback-123",
		Data: "action_0",
	}

	if !ValidateTelegramCallbackQuery(validQuery, "token") {
		t.Fatalf("valid query should pass validation")
	}

	invalidQuery := TelegramCallbackQuery{
		ID:   "",
		Data: "action_0",
	}

	if ValidateTelegramCallbackQuery(invalidQuery, "token") {
		t.Fatalf("invalid query should fail validation")
	}
}

func TestAnswerTelegramCallbackQuery(t *testing.T) {
	response := AnswerTelegramCallbackQuery("callback-123", "Action approved", false)

	if response["callback_query_id"] != "callback-123" {
		t.Fatalf("callback_query_id incorrect")
	}
	if response["text"] != "Action approved" {
		t.Fatalf("text incorrect")
	}
	if response["show_alert"] != false {
		t.Fatalf("show_alert should be false")
	}
}

func TestTelegramInlineKeyboardJSON(t *testing.T) {
	keyboard := TelegramInlineKeyboard{
		InlineKeyboard: [][]TelegramInlineButton{
			{
				{Text: "Yes", CallbackData: "yes"},
				{Text: "No", CallbackData: "no"},
			},
		},
	}

	data, err := json.Marshal(keyboard)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var unmarshaled TelegramInlineKeyboard
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(unmarshaled.InlineKeyboard) != 1 || len(unmarshaled.InlineKeyboard[0]) != 2 {
		t.Fatalf("keyboard structure not preserved")
	}
}

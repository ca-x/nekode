package imadapter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TelegramInlineButton represents a Telegram inline button.
type TelegramInlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

// TelegramInlineKeyboard represents a Telegram inline keyboard markup.
type TelegramInlineKeyboard struct {
	InlineKeyboard [][]TelegramInlineButton `json:"inline_keyboard"`
}

// TelegramCallbackQuery represents a Telegram callback query.
type TelegramCallbackQuery struct {
	ID              string `json:"id"`
	From            map[string]interface{} `json:"from"`
	ChatInstance    string `json:"chat_instance"`
	MessageID       int64  `json:"message_id,omitempty"`
	InlineMessageID string `json:"inline_message_id,omitempty"`
	Data            string `json:"data,omitempty"`
	GameShortName   string `json:"game_short_name,omitempty"`
}

// TelegramRichOutboundFrame extends OutboundFrame with Telegram-specific rich content.
type TelegramRichOutboundFrame struct {
	OutboundFrame
	ReplyMarkup *TelegramInlineKeyboard `json:"replyMarkup,omitempty"`
}

// RenderTelegramRichOutbound renders an outbound message with inline buttons.
func RenderTelegramRichOutbound(input OutboundRenderInput, buttons [][]TelegramInlineButton) []OutboundFrame {
	targetID := strings.TrimSpace(input.ConversationID)
	text := outboundText(input, TelegramMaxMessageLen)
	chunks := splitProviderMessage(text, TelegramMaxMessageLen)
	frames := make([]OutboundFrame, 0, len(chunks))

	for i, chunk := range chunks {
		frame := OutboundFrame{
			Provider:                 ProviderTelegram,
			TargetID:                 targetID,
			TargetKind:               "chat",
			ReplyToExternalMessageID: strings.TrimSpace(input.ReplyToExternalMessageID),
			Text:                     chunk,
			Sequence:                 uint32(i + 1),
			Done:                     input.Stream == nil || input.Stream.Done,
			Silent:                   input.Silent,
			ParseMode:                telegramParseModeMarkdownV2,
		}

		// Add keyboard markup to the last frame only
		if i == len(chunks)-1 && len(buttons) > 0 {
			keyboard := &TelegramInlineKeyboard{
				InlineKeyboard: buttons,
			}
			keyboardJSON, _ := json.Marshal(keyboard)
			frame.Metadata = map[string]any{
				"reply_markup": string(keyboardJSON),
			}
		}

		frames = append(frames, frame)
	}

	return frames
}

// BuildActionButtons builds inline buttons for action selection.
func BuildActionButtons(actions []string) [][]TelegramInlineButton {
	if len(actions) == 0 {
		return nil
	}

	// Create buttons in a single row if 3 or fewer, otherwise 2 per row
	var buttons [][]TelegramInlineButton
	if len(actions) <= 3 {
		row := make([]TelegramInlineButton, len(actions))
		for i, action := range actions {
			row[i] = TelegramInlineButton{
				Text:         action,
				CallbackData: fmt.Sprintf("action_%d", i),
			}
		}
		buttons = append(buttons, row)
	} else {
		for i := 0; i < len(actions); i += 2 {
			row := make([]TelegramInlineButton, 0, 2)
			row = append(row, TelegramInlineButton{
				Text:         actions[i],
				CallbackData: fmt.Sprintf("action_%d", i),
			})
			if i+1 < len(actions) {
				row = append(row, TelegramInlineButton{
					Text:         actions[i+1],
					CallbackData: fmt.Sprintf("action_%d", i+1),
				})
			}
			buttons = append(buttons, row)
		}
	}

	return buttons
}

// BuildTaskCommandButtons builds inline buttons for task commands.
func BuildTaskCommandButtons(taskID string) [][]TelegramInlineButton {
	return [][]TelegramInlineButton{
		{
			{
				Text:         "Claim",
				CallbackData: fmt.Sprintf("task_claim_%s", taskID),
			},
			{
				Text:         "Status",
				CallbackData: fmt.Sprintf("task_status_%s", taskID),
			},
		},
	}
}

// ParseTelegramCallbackData parses callback data into action type and parameters.
func ParseTelegramCallbackData(data string) (actionType string, params map[string]string) {
	params = make(map[string]string)
	parts := strings.Split(data, "_")
	if len(parts) == 0 {
		return "", params
	}

	actionType = parts[0]
	switch actionType {
	case "action":
		if len(parts) >= 2 {
			params["index"] = parts[1]
		}
	case "task":
		if len(parts) >= 3 {
			params["command"] = parts[1]
			params["task_id"] = strings.Join(parts[2:], "_")
		}
	}

	return actionType, params
}

// ValidateTelegramCallbackQuery validates a callback query for authorization.
func ValidateTelegramCallbackQuery(query TelegramCallbackQuery, expectedBotToken string) bool {
	// In a real implementation, this would verify the query signature
	// For now, we just check that required fields are present
	if query.ID == "" || query.Data == "" {
		return false
	}
	return true
}

// AnswerTelegramCallbackQuery creates a response to a callback query.
func AnswerTelegramCallbackQuery(queryID string, text string, showAlert bool) map[string]interface{} {
	return map[string]interface{}{
		"callback_query_id": queryID,
		"text":              text,
		"show_alert":        showAlert,
	}
}

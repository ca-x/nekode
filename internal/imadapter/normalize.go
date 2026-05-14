package imadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/iminbound"
)

type Normalizer struct {
	Now func() time.Time
}

func (n Normalizer) NormalizeInbound(ctx context.Context, event iminbound.RawEvent) (iminbound.Message, error) {
	provider := normalizeProvider(firstNonEmpty(event.Provider, event.EndpointKind))
	switch provider {
	case ProviderTelegram:
		return n.normalizeTelegram(event)
	case ProviderQQ:
		return n.normalizeQQ(event)
	case ProviderFeishu:
		return n.normalizeFeishu(event)
	case ProviderWeixin:
		return n.normalizeWeixin(event)
	case ProviderWeCom:
		return n.normalizeWeCom(event)
	case ProviderTerminal:
		return n.normalizeTerminal(event)
	case ProviderServerChan:
		return n.normalizeServerChan(event)
	default:
		return iminbound.Message{}, fmt.Errorf("unsupported IM provider %q", event.Provider)
	}
}

func (n Normalizer) receivedUnix(event iminbound.RawEvent) int64 {
	if event.ReceivedUnix != 0 {
		return event.ReceivedUnix
	}
	if n.Now != nil {
		return n.Now().Unix()
	}
	return time.Now().Unix()
}

func (n Normalizer) baseMessage(event iminbound.RawEvent, provider string) iminbound.Message {
	return iminbound.Message{
		EndpointID:        strings.TrimSpace(event.EndpointID),
		EndpointKind:      "im",
		Provider:          provider,
		ExternalMessageID: strings.TrimSpace(event.ExternalMessageID),
		ReceivedUnix:      n.receivedUnix(event),
		Metadata: map[string]any{
			"provider": provider,
		},
	}
}

func (n Normalizer) normalizeTelegram(event iminbound.RawEvent) (iminbound.Message, error) {
	var payload struct {
		UpdateID int64 `json:"update_id"`
		Message  struct {
			MessageID int64 `json:"message_id"`
			Date      int64 `json:"date"`
			Text      string
			Caption   string
			Chat      struct {
				ID       int64
				Type     string
				Title    string
				Username string
			}
			From struct {
				ID        int64
				Username  string
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
			}
			Photo    []map[string]any
			Document *struct {
				FileID   string `json:"file_id"`
				FileName string `json:"file_name"`
				MimeType string `json:"mime_type"`
				FileSize int64  `json:"file_size"`
			}
		}
	}
	if err := json.Unmarshal(event.Body, &payload); err != nil {
		return iminbound.Message{}, err
	}
	msg := n.baseMessage(event, ProviderTelegram)
	if msg.ExternalMessageID == "" {
		msg.ExternalMessageID = firstNonEmpty(formatInt(payload.Message.MessageID), formatInt(payload.UpdateID))
	}
	if payload.Message.Date > 0 {
		msg.ReceivedUnix = payload.Message.Date
	}
	chatID := formatInt(payload.Message.Chat.ID)
	senderID := formatInt(payload.Message.From.ID)
	msg.Conversation = iminbound.Conversation{
		ExternalID:  chatID,
		DisplayName: firstNonEmpty(payload.Message.Chat.Title, payload.Message.Chat.Username),
		IsGroup:     payload.Message.Chat.Type == "group" || payload.Message.Chat.Type == "supergroup",
	}
	msg.Sender = iminbound.Sender{
		ExternalID:  senderID,
		DisplayName: strings.TrimSpace(payload.Message.From.FirstName + " " + payload.Message.From.LastName),
		Username:    payload.Message.From.Username,
		Kind:        "human",
	}
	if msg.Sender.DisplayName == "" {
		msg.Sender.DisplayName = payload.Message.From.Username
	}
	text := firstNonEmpty(payload.Message.Text, payload.Message.Caption)
	if text != "" {
		msg.Content = append(msg.Content, iminbound.ContentBlock{Type: iminbound.ContentTypeText, Text: text})
	}
	if len(payload.Message.Photo) > 0 {
		msg.Content = append(msg.Content, iminbound.ContentBlock{
			Type:     iminbound.ContentTypeUnknown,
			Text:     "[image]",
			Metadata: map[string]any{"telegram_photo": payload.Message.Photo[len(payload.Message.Photo)-1]},
		})
	}
	if payload.Message.Document != nil {
		msg.Content = append(msg.Content, iminbound.ContentBlock{
			Type:      iminbound.ContentTypeUnknown,
			Text:      "[file: " + firstNonEmpty(payload.Message.Document.FileName, payload.Message.Document.FileID) + "]",
			Filename:  payload.Message.Document.FileName,
			MimeType:  payload.Message.Document.MimeType,
			SizeBytes: payload.Message.Document.FileSize,
			Metadata:  map[string]any{"telegram_file_id": payload.Message.Document.FileID},
		})
	}
	return normalizeAndValidate(msg)
}

func (n Normalizer) normalizeQQ(event iminbound.RawEvent) (iminbound.Message, error) {
	var payload struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Author  struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"author"`
		GroupID     string `json:"group_id"`
		ChannelID   string `json:"channel_id"`
		Timestamp   string `json:"timestamp"`
		Attachments []struct {
			URL         string `json:"url"`
			Filename    string `json:"filename"`
			ContentType string `json:"content_type"`
			Size        int64  `json:"size"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(event.Body, &payload); err != nil {
		return iminbound.Message{}, err
	}
	msg := n.baseMessage(event, ProviderQQ)
	if msg.ExternalMessageID == "" {
		msg.ExternalMessageID = payload.ID
	}
	if ts := unixFromRFC3339(payload.Timestamp); ts > 0 {
		msg.ReceivedUnix = ts
	}
	chatID := firstNonEmpty(payload.GroupID, payload.ChannelID, payload.Author.ID)
	msg.Conversation = iminbound.Conversation{ExternalID: chatID, IsGroup: payload.GroupID != ""}
	msg.Sender = iminbound.Sender{ExternalID: payload.Author.ID, DisplayName: payload.Author.Username, Kind: "human"}
	if strings.TrimSpace(payload.Content) != "" {
		msg.Content = append(msg.Content, iminbound.ContentBlock{Type: iminbound.ContentTypeText, Text: payload.Content})
	}
	for _, attachment := range payload.Attachments {
		blockType := iminbound.ContentTypeFile
		if strings.HasPrefix(attachment.ContentType, "image/") {
			blockType = iminbound.ContentTypeImage
		} else if strings.HasPrefix(attachment.ContentType, "video/") {
			blockType = iminbound.ContentTypeVideo
		} else if strings.HasPrefix(attachment.ContentType, "audio/") {
			blockType = iminbound.ContentTypeAudio
		}
		msg.Content = append(msg.Content, iminbound.ContentBlock{
			Type:        blockType,
			ExternalURL: attachment.URL,
			Filename:    attachment.Filename,
			MimeType:    attachment.ContentType,
			SizeBytes:   attachment.Size,
		})
	}
	return normalizeAndValidate(msg)
}

func (n Normalizer) normalizeFeishu(event iminbound.RawEvent) (iminbound.Message, error) {
	var payload struct {
		Header struct {
			EventID string `json:"event_id"`
		} `json:"header"`
		Event struct {
			Sender struct {
				SenderID struct {
					OpenID  string `json:"open_id"`
					UnionID string `json:"union_id"`
					UserID  string `json:"user_id"`
				} `json:"sender_id"`
				SenderType string `json:"sender_type"`
			} `json:"sender"`
			Message struct {
				MessageID   string            `json:"message_id"`
				CreateTime  string            `json:"create_time"`
				RootID      string            `json:"root_id"`
				ChatID      string            `json:"chat_id"`
				ChatType    string            `json:"chat_type"`
				MessageType string            `json:"message_type"`
				Content     string            `json:"content"`
				Mentions    []FeishuMention   `json:"mentions"`
				Metadata    map[string]string `json:"metadata"`
			} `json:"message"`
		} `json:"event"`
	}
	if err := json.Unmarshal(event.Body, &payload); err != nil {
		return iminbound.Message{}, err
	}
	msg := n.baseMessage(event, ProviderFeishu)
	if msg.ExternalMessageID == "" {
		msg.ExternalMessageID = firstNonEmpty(payload.Event.Message.MessageID, payload.Header.EventID)
	}
	if ts := unixFromMillisString(payload.Event.Message.CreateTime); ts > 0 {
		msg.ReceivedUnix = ts
	}
	text := feishuContentText(payload.Event.Message.MessageType, payload.Event.Message.Content)
	if payload.Event.Message.ChatType == "group" {
		text = StripFeishuMentions(text, payload.Event.Message.Mentions)
	}
	msg.Conversation = iminbound.Conversation{
		ExternalID:       payload.Event.Message.ChatID,
		IsGroup:          payload.Event.Message.ChatType == "group",
		ExternalThreadID: payload.Event.Message.RootID,
		RootMessageID:    payload.Event.Message.RootID,
	}
	msg.Sender = iminbound.Sender{
		ExternalID:   firstNonEmpty(payload.Event.Sender.SenderID.UnionID, payload.Event.Sender.SenderID.OpenID, payload.Event.Sender.SenderID.UserID),
		CandidateIDs: []string{payload.Event.Sender.SenderID.UnionID, payload.Event.Sender.SenderID.OpenID, payload.Event.Sender.SenderID.UserID},
		Kind:         firstNonEmpty(payload.Event.Sender.SenderType, "human"),
	}
	msg.Content = []iminbound.ContentBlock{{Type: mapFeishuContentType(payload.Event.Message.MessageType, text), Text: text}}
	msg.Metadata["mentions"] = payload.Event.Message.Mentions
	msg.Metadata["message_type"] = payload.Event.Message.MessageType
	return normalizeAndValidate(msg)
}

func (n Normalizer) normalizeWeixin(event iminbound.RawEvent) (iminbound.Message, error) {
	var payload struct {
		Seq          int64  `json:"seq"`
		MessageID    int64  `json:"message_id"`
		CreateTime   int64  `json:"create_time"`
		CreateTimeMs int64  `json:"create_time_ms"`
		FromUserID   string `json:"from_user_id"`
		SessionID    string `json:"session_id"`
		GroupID      string `json:"group_id"`
		ContextToken string `json:"context_token"`
		ItemList     []struct {
			Type     int `json:"type"`
			TextItem *struct {
				Text string `json:"text"`
			} `json:"text_item"`
			ImageItem *struct {
				URL     string `json:"url"`
				MediaID string `json:"media_id"`
			} `json:"image_item"`
			VoiceItem *struct {
				Text    string `json:"text"`
				MediaID string `json:"media_id"`
				Format  string `json:"format"`
			} `json:"voice_item"`
			FileItem *struct {
				FileName string `json:"file_name"`
				Len      string `json:"len"`
				MediaID  string `json:"media_id"`
			} `json:"file_item"`
			VideoItem *struct {
				MediaID string `json:"media_id"`
			} `json:"video_item"`
		} `json:"item_list"`
		OfficialAccount struct {
			CreateTime int64 `json:"create_time"`
		} `json:"official_account"`
	}
	if err := json.Unmarshal(event.Body, &payload); err != nil {
		return iminbound.Message{}, err
	}
	msg := n.baseMessage(event, ProviderWeixin)
	if msg.ExternalMessageID == "" {
		msg.ExternalMessageID = firstNonEmpty(formatInt(payload.MessageID), formatInt(payload.Seq))
	}
	if ts := unixFromMaybeMillis(firstNonEmpty(formatInt(payload.CreateTimeMs), formatInt(payload.CreateTime), formatInt(payload.OfficialAccount.CreateTime))); ts > 0 {
		msg.ReceivedUnix = ts
	}
	chatID := firstNonEmpty(payload.GroupID, payload.SessionID, payload.FromUserID)
	msg.Conversation = iminbound.Conversation{ExternalID: chatID, IsGroup: payload.GroupID != ""}
	msg.Sender = iminbound.Sender{ExternalID: payload.FromUserID, Kind: "human"}
	for _, item := range payload.ItemList {
		switch {
		case item.TextItem != nil && strings.TrimSpace(item.TextItem.Text) != "":
			msg.Content = append(msg.Content, iminbound.ContentBlock{Type: iminbound.ContentTypeText, Text: item.TextItem.Text})
		case item.ImageItem != nil:
			blockType := iminbound.ContentTypeImage
			if strings.TrimSpace(item.ImageItem.URL) == "" {
				blockType = iminbound.ContentTypeUnknown
			}
			msg.Content = append(msg.Content, iminbound.ContentBlock{
				Type:        blockType,
				ExternalURL: item.ImageItem.URL,
				Text:        "[image]",
				Metadata:    cleanMetadata(map[string]any{"media_id": item.ImageItem.MediaID}),
			})
		case item.VoiceItem != nil:
			msg.Content = append(msg.Content, iminbound.ContentBlock{
				Type:     iminbound.ContentTypeUnknown,
				Text:     firstNonEmpty(item.VoiceItem.Text, "[voice]"),
				Metadata: cleanMetadata(map[string]any{"media_id": item.VoiceItem.MediaID, "format": item.VoiceItem.Format}),
			})
		case item.FileItem != nil:
			msg.Content = append(msg.Content, iminbound.ContentBlock{
				Type:     iminbound.ContentTypeUnknown,
				Text:     "[file: " + item.FileItem.FileName + "]",
				Filename: item.FileItem.FileName,
				Metadata: cleanMetadata(map[string]any{
					"media_id": item.FileItem.MediaID,
					"len":      item.FileItem.Len,
				}),
			})
		case item.VideoItem != nil:
			msg.Content = append(msg.Content, iminbound.ContentBlock{
				Type:     iminbound.ContentTypeUnknown,
				Text:     "[video]",
				Metadata: cleanMetadata(map[string]any{"media_id": item.VideoItem.MediaID}),
			})
		}
	}
	msg.Metadata["context_token"] = payload.ContextToken
	return normalizeAndValidate(msg)
}

func (n Normalizer) normalizeWeCom(event iminbound.RawEvent) (iminbound.Message, error) {
	var payload struct {
		MessageID  int64  `json:"message_id"`
		CreateTime int64  `json:"create_time"`
		FromUserID string `json:"from_user_id"`
		SessionID  string `json:"session_id"`
		ChatID     string `json:"chat_id"`
		ChatType   string `json:"chat_type"`
		ItemList   []struct {
			Type     int `json:"type"`
			TextItem *struct {
				Text string `json:"text"`
			} `json:"text_item"`
			ImageItem *struct {
				URL     string `json:"url"`
				MediaID string `json:"media_id"`
			} `json:"image_item"`
			VoiceItem *struct {
				Text    string `json:"text"`
				MediaID string `json:"media_id"`
				Format  string `json:"format"`
			} `json:"voice_item"`
			FileItem *struct {
				FileName string `json:"file_name"`
				MediaID  string `json:"media_id"`
			} `json:"file_item"`
			VideoItem *struct {
				MediaID string `json:"media_id"`
			} `json:"video_item"`
		} `json:"item_list"`
		WeCom struct {
			CreateTime int64  `json:"create_time"`
			MsgType    string `json:"msg_type"`
			AgentID    int64  `json:"agent_id"`
		} `json:"wecom"`
	}
	if err := json.Unmarshal(event.Body, &payload); err != nil {
		return iminbound.Message{}, err
	}
	msg := n.baseMessage(event, ProviderWeCom)
	if msg.ExternalMessageID == "" {
		msg.ExternalMessageID = formatInt(payload.MessageID)
	}
	if ts := unixFromMaybeMillis(firstNonEmpty(formatInt(payload.CreateTime), formatInt(payload.WeCom.CreateTime))); ts > 0 {
		msg.ReceivedUnix = ts
	}
	chatID := firstNonEmpty(payload.ChatID, payload.SessionID, payload.FromUserID)
	msg.Conversation = iminbound.Conversation{ExternalID: chatID, IsGroup: payload.ChatType == "group"}
	msg.Sender = iminbound.Sender{ExternalID: payload.FromUserID, Kind: "human"}
	for _, item := range payload.ItemList {
		switch {
		case item.TextItem != nil && strings.TrimSpace(item.TextItem.Text) != "":
			msg.Content = append(msg.Content, iminbound.ContentBlock{Type: iminbound.ContentTypeText, Text: item.TextItem.Text})
		case item.ImageItem != nil:
			blockType := iminbound.ContentTypeImage
			if strings.TrimSpace(item.ImageItem.URL) == "" {
				blockType = iminbound.ContentTypeUnknown
			}
			msg.Content = append(msg.Content, iminbound.ContentBlock{
				Type:        blockType,
				ExternalURL: item.ImageItem.URL,
				Text:        "[image]",
				Metadata:    cleanMetadata(map[string]any{"media_id": item.ImageItem.MediaID}),
			})
		case item.VoiceItem != nil:
			msg.Content = append(msg.Content, iminbound.ContentBlock{
				Type:     iminbound.ContentTypeUnknown,
				Text:     firstNonEmpty(item.VoiceItem.Text, "[voice]"),
				Metadata: cleanMetadata(map[string]any{"media_id": item.VoiceItem.MediaID, "format": item.VoiceItem.Format}),
			})
		case item.FileItem != nil:
			msg.Content = append(msg.Content, iminbound.ContentBlock{
				Type:     iminbound.ContentTypeUnknown,
				Text:     "[file: " + item.FileItem.FileName + "]",
				Filename: item.FileItem.FileName,
				Metadata: cleanMetadata(map[string]any{"media_id": item.FileItem.MediaID}),
			})
		case item.VideoItem != nil:
			msg.Content = append(msg.Content, iminbound.ContentBlock{
				Type:     iminbound.ContentTypeUnknown,
				Text:     "[video]",
				Metadata: cleanMetadata(map[string]any{"media_id": item.VideoItem.MediaID}),
			})
		}
	}
	msg.Metadata["message_type"] = payload.WeCom.MsgType
	msg.Metadata["agent_id"] = payload.WeCom.AgentID
	return normalizeAndValidate(msg)
}

func (n Normalizer) normalizeTerminal(event iminbound.RawEvent) (iminbound.Message, error) {
	var payload struct {
		ID           string `json:"id"`
		MessageID    string `json:"message_id"`
		SessionID    string `json:"session_id"`
		OperatorID   string `json:"operator_id"`
		Operator     string `json:"operator"`
		OperatorName string `json:"operator_name"`
		Text         string `json:"text"`
		Target       string `json:"target"`
		ThreadID     string `json:"thread_id"`
	}
	if err := json.Unmarshal(event.Body, &payload); err != nil {
		return iminbound.Message{}, err
	}
	msg := n.baseMessage(event, ProviderTerminal)
	if msg.ExternalMessageID == "" {
		msg.ExternalMessageID = firstNonEmpty(payload.MessageID, payload.ID)
	}
	msg.Conversation = iminbound.Conversation{ExternalID: firstNonEmpty(payload.SessionID, "local"), TargetHint: payload.Target, ThreadID: payload.ThreadID}
	msg.Sender = iminbound.Sender{ExternalID: firstNonEmpty(payload.OperatorID, payload.Operator, "terminal"), DisplayName: firstNonEmpty(payload.OperatorName, payload.Operator), Kind: "human"}
	msg.Content = []iminbound.ContentBlock{{Type: iminbound.ContentTypeText, Text: payload.Text}}
	return normalizeAndValidate(msg)
}

func (n Normalizer) normalizeServerChan(event iminbound.RawEvent) (iminbound.Message, error) {
	var payload struct {
		UpdateID int64 `json:"update_id"`
		Message  struct {
			MessageID int64  `json:"message_id"`
			ChatID    int64  `json:"chat_id"`
			Text      string `json:"text"`
			Date      int64  `json:"date"`
			Chat      struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
			From struct {
				ID        int64  `json:"id"`
				Username  string `json:"username"`
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
			} `json:"from"`
		} `json:"message"`
	}
	if err := json.Unmarshal(event.Body, &payload); err != nil {
		return iminbound.Message{}, err
	}
	msg := n.baseMessage(event, ProviderServerChan)
	if msg.ExternalMessageID == "" {
		msg.ExternalMessageID = firstNonEmpty(formatInt(payload.Message.MessageID), formatInt(payload.UpdateID))
	}
	if payload.Message.Date > 0 {
		msg.ReceivedUnix = payload.Message.Date
	}
	chatID := payload.Message.Chat.ID
	if chatID == 0 {
		chatID = payload.Message.ChatID
	}
	userID := payload.Message.From.ID
	if userID == 0 {
		userID = chatID
	}
	msg.Conversation = iminbound.Conversation{
		ExternalID: formatInt(chatID),
		IsGroup:    payload.Message.Chat.Type == "group" || payload.Message.Chat.Type == "supergroup",
	}
	msg.Sender = iminbound.Sender{
		ExternalID:  formatInt(userID),
		DisplayName: strings.TrimSpace(payload.Message.From.FirstName + " " + payload.Message.From.LastName),
		Username:    payload.Message.From.Username,
		Kind:        "human",
	}
	if msg.Sender.DisplayName == "" {
		msg.Sender.DisplayName = payload.Message.From.Username
	}
	msg.Content = []iminbound.ContentBlock{{Type: iminbound.ContentTypeText, Text: payload.Message.Text}}
	return normalizeAndValidate(msg)
}

type FeishuMention struct {
	Key string `json:"key"`
	ID  struct {
		OpenID  string `json:"open_id"`
		UnionID string `json:"union_id"`
		UserID  string `json:"user_id"`
	} `json:"id"`
	Name string `json:"name"`
}

func StripFeishuMentions(text string, mentions []FeishuMention) string {
	for _, mention := range mentions {
		if mention.Key != "" {
			text = strings.ReplaceAll(text, mention.Key, "")
		}
	}
	for {
		start := strings.Index(text, "@_user_")
		if start < 0 {
			break
		}
		end := start + len("@_user_")
		for end < len(text) && text[end] >= '0' && text[end] <= '9' {
			end++
		}
		text = text[:start] + text[end:]
	}
	return strings.TrimSpace(text)
}

func feishuContentText(messageType string, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return placeholderForType(messageType)
	}
	var content map[string]any
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		return raw
	}
	if text, _ := content["text"].(string); text != "" {
		return text
	}
	for _, key := range []string{"file_name", "name", "image_key", "file_key"} {
		if value, _ := content[key].(string); value != "" {
			return placeholderForType(messageType) + ": " + value
		}
	}
	return placeholderForType(messageType)
}

func mapFeishuContentType(messageType string, text string) iminbound.ContentType {
	switch strings.ToLower(strings.TrimSpace(messageType)) {
	case "text", "post":
		return iminbound.ContentTypeText
	case "image":
		if text != "" {
			return iminbound.ContentTypeUnknown
		}
		return iminbound.ContentTypeImage
	case "file":
		if text != "" {
			return iminbound.ContentTypeUnknown
		}
		return iminbound.ContentTypeFile
	case "audio":
		if text != "" {
			return iminbound.ContentTypeUnknown
		}
		return iminbound.ContentTypeAudio
	case "media", "video":
		if text != "" {
			return iminbound.ContentTypeUnknown
		}
		return iminbound.ContentTypeVideo
	case "sticker":
		if text != "" {
			return iminbound.ContentTypeUnknown
		}
		return iminbound.ContentTypeSticker
	case "location":
		return iminbound.ContentTypeLocation
	default:
		return iminbound.ContentTypeUnknown
	}
}

func placeholderForType(messageType string) string {
	switch strings.ToLower(strings.TrimSpace(messageType)) {
	case "":
		return "[message]"
	default:
		return "[" + strings.ToLower(strings.TrimSpace(messageType)) + "]"
	}
}

func normalizeAndValidate(msg iminbound.Message) (iminbound.Message, error) {
	msg = msg.Normalize()
	if err := msg.Validate(); err != nil {
		return iminbound.Message{}, err
	}
	return msg, nil
}

func unixFromRFC3339(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0
	}
	return parsed.Unix()
}

func unixFromMillisString(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed / 1000
}

func unixFromMaybeMillis(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0
	}
	if parsed > 9999999999 {
		return parsed / 1000
	}
	return parsed
}

func cleanMetadata(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
			out[key] = strings.TrimSpace(typed)
		default:
			if value != nil {
				out[key] = value
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func formatInt(value int64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

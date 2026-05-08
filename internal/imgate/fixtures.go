package imgate

import (
	"strings"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/iminbound"
)

type Fixture struct {
	Provider           string
	EndpointID         string
	EndpointName       string
	ExternalMessageID  string
	Target             string
	ThreadID           string
	ConversationID     string
	ConversationName   string
	ExternalThreadID   string
	RootMessageID      string
	IsGroup            bool
	SenderID           string
	SenderName         string
	SenderUsername     string
	SenderKind         string
	Text               string
	AttachmentFilename string
	AttachmentMimeType string
	AttachmentContent  string
}

func ProviderFixtures() []Fixture {
	return []Fixture{
		{
			Provider:          imadapter.ProviderTelegram,
			EndpointID:        "iep-im-telegram",
			EndpointName:      "Telegram Engineering",
			ExternalMessageID: "tg-msg-1001",
			Target:            "#engineering",
			ThreadID:          "im-telegram-engineering",
			ConversationID:    "tg-chat-42",
			ConversationName:  "Engineering",
			IsGroup:           true,
			SenderID:          "tg-user-7",
			SenderName:        "Alice TG",
			SenderUsername:    "alice",
			SenderKind:        "user",
			Text:              "@nekode please triage the deploy alert",
		},
		{
			Provider:           imadapter.ProviderQQ,
			EndpointID:         "iep-im-qq",
			EndpointName:       "QQ Ops",
			ExternalMessageID:  "qq-msg-2001",
			Target:             "#ops",
			ThreadID:           "im-qq-ops",
			ConversationID:     "qq-group-88",
			ConversationName:   "Ops Group",
			IsGroup:            true,
			SenderID:           "qq-user-9",
			SenderName:         "Bob QQ",
			SenderKind:         "user",
			Text:               "disk usage screenshot attached",
			AttachmentFilename: "disk.png",
			AttachmentMimeType: "image/png",
			AttachmentContent:  "fake-png-bytes",
		},
		{
			Provider:          imadapter.ProviderFeishu,
			EndpointID:        "iep-im-feishu",
			EndpointName:      "Feishu Product",
			ExternalMessageID: "fs-msg-3001",
			Target:            "#product",
			ThreadID:          "im-feishu-product",
			ConversationID:    "oc_feishu_product",
			ConversationName:  "Product Room",
			IsGroup:           true,
			SenderID:          "ou_feishu_1",
			SenderName:        "Carol Feishu",
			SenderUsername:    "carol",
			SenderKind:        "user",
			Text:              "@planner summarize the customer thread",
		},
		{
			Provider:          imadapter.ProviderWeixin,
			EndpointID:        "iep-im-weixin",
			EndpointName:      "WeChat Support",
			ExternalMessageID: "wx-msg-4001",
			Target:            "inbox:im/weixin/support-user",
			ThreadID:          "im-weixin-support-user",
			ConversationID:    "wx-openid-support",
			ConversationName:  "WeChat private chat",
			IsGroup:           false,
			SenderID:          "wx-user-12",
			SenderName:        "Dana WeChat",
			SenderKind:        "user",
			Text:              "can an agent help with this account",
		},
		{
			Provider:          imadapter.ProviderTerminal,
			EndpointID:        "iep-im-terminal",
			EndpointName:      "Terminal Channel",
			ExternalMessageID: "term-msg-5001",
			Target:            "inbox:im/terminal/session-1",
			ThreadID:          "im-terminal-session-1",
			ConversationID:    "term-session-1",
			ConversationName:  "user@host",
			ExternalThreadID:  "term-shell-1",
			IsGroup:           false,
			SenderID:          "terminal-user",
			SenderName:        "Terminal User",
			SenderKind:        "terminal",
			Text:              "start a new terminal session",
		},
	}
}

func (f Fixture) Message(attachmentIDs ...string) iminbound.Message {
	blocks := []iminbound.ContentBlock{{Type: iminbound.ContentTypeText, Text: f.Text}}
	for _, id := range attachmentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		blocks = append(blocks, iminbound.ContentBlock{
			Type:         contentTypeForMime(f.AttachmentMimeType),
			AttachmentID: id,
			Filename:     f.AttachmentFilename,
			MimeType:     f.AttachmentMimeType,
		})
	}
	return iminbound.Message{
		EndpointID:        f.EndpointID,
		EndpointKind:      "im",
		Provider:          f.Provider,
		ExternalMessageID: f.ExternalMessageID,
		Conversation: iminbound.Conversation{
			ExternalID:       f.ConversationID,
			DisplayName:      f.ConversationName,
			IsGroup:          f.IsGroup,
			TargetHint:       f.Target,
			ThreadID:         f.ThreadID,
			ExternalThreadID: f.ExternalThreadID,
			RootMessageID:    f.RootMessageID,
		},
		Sender: iminbound.Sender{
			ExternalID:  f.SenderID,
			DisplayName: f.SenderName,
			Username:    f.SenderUsername,
			Kind:        f.SenderKind,
		},
		Content:       blocks,
		AttachmentIDs: attachmentIDs,
	}
}

func contentTypeForMime(mimeType string) iminbound.ContentType {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return iminbound.ContentTypeImage
	case strings.HasPrefix(mimeType, "video/"):
		return iminbound.ContentTypeVideo
	case strings.HasPrefix(mimeType, "audio/"):
		return iminbound.ContentTypeAudio
	case strings.TrimSpace(mimeType) != "":
		return iminbound.ContentTypeFile
	default:
		return iminbound.ContentTypeUnknown
	}
}

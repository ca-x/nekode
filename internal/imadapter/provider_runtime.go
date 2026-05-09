package imadapter

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ca-x/nekode/internal/iminbound"
)

const (
	TelegramMaxMessageLen   = 4000
	QQMaxMessageLen         = 3500
	ServerChanMaxMessageLen = 4000

	telegramParseModeMarkdownV2 = "MarkdownV2"
	serverChanParseModeMarkdown = "markdown"
	qqStreamStateGenerating     = 1
	qqStreamStateDone           = 10
)

type ProviderRawEventInput struct {
	EndpointID        string
	ExternalMessageID string
	ReceivedUnix      int64
	Headers           map[string][]string
	Body              []byte
	Metadata          map[string]any
}

type OutboundRenderInput struct {
	Provider                 string
	ConversationID           string
	ReplyToExternalMessageID string
	Text                     string
	Silent                   bool
	Stream                   *StreamState
	StreamID                 string
	SequenceStart            uint32
}

type OutboundFrame struct {
	Provider                 string
	TargetID                 string
	TargetKind               string
	ReplyToExternalMessageID string
	Text                     string
	Sequence                 uint32
	Done                     bool
	Silent                   bool
	ParseMode                string
	StreamID                 string
	StreamState              int
	Metadata                 map[string]any
}

func TelegramRawEvent(input ProviderRawEventInput) iminbound.RawEvent {
	return providerRawEvent(ProviderTelegram, input)
}

func QQRawEvent(input ProviderRawEventInput) iminbound.RawEvent {
	return providerRawEvent(ProviderQQ, input)
}

func FeishuRawEvent(input ProviderRawEventInput) iminbound.RawEvent {
	return providerRawEvent(ProviderFeishu, input)
}

func WeChatRawEvent(input ProviderRawEventInput) iminbound.RawEvent {
	return providerRawEvent(ProviderWeixin, input)
}

func ServerChanRawEvent(input ProviderRawEventInput) iminbound.RawEvent {
	return providerRawEvent(ProviderServerChan, input)
}

func providerRawEvent(provider string, input ProviderRawEventInput) iminbound.RawEvent {
	return iminbound.RawEvent{
		EndpointID:        strings.TrimSpace(input.EndpointID),
		EndpointKind:      "im",
		Provider:          provider,
		ExternalMessageID: strings.TrimSpace(input.ExternalMessageID),
		ReceivedUnix:      input.ReceivedUnix,
		Headers:           input.Headers,
		Body:              input.Body,
		Metadata:          input.Metadata,
	}
}

func RenderProviderOutbound(input OutboundRenderInput) ([]OutboundFrame, error) {
	switch normalizeProvider(input.Provider) {
	case ProviderTelegram:
		return RenderTelegramOutbound(input), nil
	case ProviderQQ:
		return RenderQQOutbound(input), nil
	case ProviderServerChan:
		return RenderServerChanOutbound(input), nil
	default:
		return nil, fmt.Errorf("unsupported IM provider %q", input.Provider)
	}
}

func RenderTelegramOutbound(input OutboundRenderInput) []OutboundFrame {
	targetID := strings.TrimSpace(input.ConversationID)
	text := outboundText(input, TelegramMaxMessageLen)
	chunks := splitProviderMessage(text, TelegramMaxMessageLen)
	frames := make([]OutboundFrame, 0, len(chunks))
	for i, chunk := range chunks {
		frames = append(frames, OutboundFrame{
			Provider:                 ProviderTelegram,
			TargetID:                 targetID,
			TargetKind:               "chat",
			ReplyToExternalMessageID: strings.TrimSpace(input.ReplyToExternalMessageID),
			Text:                     chunk,
			Sequence:                 uint32(i + 1),
			Done:                     input.Stream == nil || input.Stream.Done,
			Silent:                   input.Silent,
			ParseMode:                telegramParseModeMarkdownV2,
		})
	}
	return frames
}

func RenderQQOutbound(input OutboundRenderInput) []OutboundFrame {
	targetKind, targetID := qqTarget(strings.TrimSpace(input.ConversationID))
	sequenceStart := input.SequenceStart
	if sequenceStart == 0 {
		sequenceStart = 100
	}
	text := outboundText(input, QQMaxMessageLen)
	chunks := splitProviderMessage(text, QQMaxMessageLen)
	frames := make([]OutboundFrame, 0, len(chunks))
	for i, chunk := range chunks {
		done := input.Stream == nil || input.Stream.Done
		streamState := 0
		if input.Stream != nil {
			streamState = qqStreamStateGenerating
			if input.Stream.Done {
				streamState = qqStreamStateDone
			}
		}
		frames = append(frames, OutboundFrame{
			Provider:                 ProviderQQ,
			TargetID:                 targetID,
			TargetKind:               targetKind,
			ReplyToExternalMessageID: strings.TrimSpace(input.ReplyToExternalMessageID),
			Text:                     chunk,
			Sequence:                 sequenceStart + uint32(i),
			Done:                     done,
			Silent:                   input.Silent,
			StreamID:                 strings.TrimSpace(input.StreamID),
			StreamState:              streamState,
		})
	}
	return frames
}

func RenderServerChanOutbound(input OutboundRenderInput) []OutboundFrame {
	targetID := strings.TrimPrefix(strings.TrimSpace(input.ConversationID), "serverchan:")
	text := outboundText(input, ServerChanMaxMessageLen)
	chunks := splitProviderMessage(text, ServerChanMaxMessageLen)
	frames := make([]OutboundFrame, 0, len(chunks))
	for i, chunk := range chunks {
		frames = append(frames, OutboundFrame{
			Provider:                 ProviderServerChan,
			TargetID:                 targetID,
			TargetKind:               "chat",
			ReplyToExternalMessageID: strings.TrimSpace(input.ReplyToExternalMessageID),
			Text:                     chunk,
			Sequence:                 uint32(i + 1),
			Done:                     input.Stream == nil || input.Stream.Done,
			Silent:                   input.Silent,
			ParseMode:                serverChanParseModeMarkdown,
		})
	}
	return frames
}

func outboundText(input OutboundRenderInput, maxLen int) string {
	if input.Stream != nil {
		return RenderStream(*input.Stream, maxLen)
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return "(empty response)"
	}
	return text
}

func qqTarget(conversationID string) (string, string) {
	switch {
	case strings.HasPrefix(conversationID, "qq:group:"):
		return "group", strings.TrimPrefix(conversationID, "qq:group:")
	case strings.HasPrefix(conversationID, "qq:c2c:"):
		return "c2c", strings.TrimPrefix(conversationID, "qq:c2c:")
	case strings.HasPrefix(conversationID, "group:"):
		return "group", strings.TrimPrefix(conversationID, "group:")
	default:
		return "c2c", conversationID
	}
}

func splitProviderMessage(text string, maxLen int) []string {
	if strings.TrimSpace(text) == "" {
		return []string{"(empty response)"}
	}
	if maxLen <= 0 || len(text) <= maxLen {
		return []string{text}
	}
	var chunks []string
	rest := text
	for len(rest) > maxLen {
		cutAt := maxLen
		if newline := strings.LastIndex(rest[:cutAt], "\n"); newline > 0 {
			cutAt = newline + 1
		}
		for cutAt > 0 && !utf8.RuneStart(rest[cutAt]) {
			cutAt--
		}
		if cutAt <= 0 {
			cutAt = maxLen
		}
		chunks = append(chunks, strings.TrimSpace(rest[:cutAt]))
		rest = strings.TrimSpace(rest[cutAt:])
	}
	if rest != "" {
		chunks = append(chunks, rest)
	}
	return chunks
}

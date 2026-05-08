package imadapter

import (
	"context"
	"strings"
	"testing"
)

func TestProviderRawEventsUseStableProviderEnvelope(t *testing.T) {
	tg := TelegramRawEvent(ProviderRawEventInput{
		EndpointID:        " ep-tg ",
		ExternalMessageID: " msg-1 ",
		ReceivedUnix:      123,
		Body:              []byte(`{"ok":true}`),
	})
	if tg.EndpointID != "ep-tg" ||
		tg.EndpointKind != "im" ||
		tg.Provider != ProviderTelegram ||
		tg.ExternalMessageID != "msg-1" ||
		tg.ReceivedUnix != 123 ||
		string(tg.Body) != `{"ok":true}` {
		t.Fatalf("TelegramRawEvent() = %+v", tg)
	}

	qq := QQRawEvent(ProviderRawEventInput{EndpointID: "ep-qq"})
	if qq.EndpointKind != "im" || qq.Provider != ProviderQQ || qq.EndpointID != "ep-qq" {
		t.Fatalf("QQRawEvent() = %+v", qq)
	}
}

func TestProviderRawEventsFeedNormalizer(t *testing.T) {
	normalizer := Normalizer{}
	tg, err := normalizer.NormalizeInbound(context.Background(), TelegramRawEvent(ProviderRawEventInput{
		EndpointID: "ep-tg",
		Body:       []byte(`{"update_id":9,"message":{"message_id":10,"text":"hello tg","chat":{"id":-100,"type":"supergroup","title":"Ops"},"from":{"id":42,"username":"alice"}}}`),
	}))
	if err != nil {
		t.Fatalf("NormalizeInbound(telegram) error = %v", err)
	}
	if tg.Provider != ProviderTelegram || tg.Conversation.ExternalID != "-100" || tg.Sender.ExternalID != "42" {
		t.Fatalf("telegram normalized message = %+v", tg)
	}

	qq, err := normalizer.NormalizeInbound(context.Background(), QQRawEvent(ProviderRawEventInput{
		EndpointID: "ep-qq",
		Body:       []byte(`{"id":"qq-msg","content":"hi qq","author":{"id":"u1","username":"Alice"},"group_id":"g1"}`),
	}))
	if err != nil {
		t.Fatalf("NormalizeInbound(qq) error = %v", err)
	}
	if qq.Provider != ProviderQQ || qq.Conversation.ExternalID != "g1" || qq.Sender.ExternalID != "u1" {
		t.Fatalf("qq normalized message = %+v", qq)
	}
}

func TestRenderTelegramOutboundFrames(t *testing.T) {
	text := strings.Repeat("a", TelegramMaxMessageLen+10)
	frames := RenderTelegramOutbound(OutboundRenderInput{
		Provider:                 ProviderTelegram,
		ConversationID:           "-100",
		ReplyToExternalMessageID: "tg-msg-1",
		Text:                     text,
		Silent:                   true,
	})
	if len(frames) != 2 {
		t.Fatalf("frames len = %d, want 2", len(frames))
	}
	if frames[0].Provider != ProviderTelegram ||
		frames[0].TargetID != "-100" ||
		frames[0].TargetKind != "chat" ||
		frames[0].ReplyToExternalMessageID != "tg-msg-1" ||
		frames[0].ParseMode != telegramParseModeMarkdownV2 ||
		!frames[0].Silent ||
		frames[0].Sequence != 1 ||
		len(frames[0].Text) > TelegramMaxMessageLen {
		t.Fatalf("first frame = %+v", frames[0])
	}
	if frames[1].Sequence != 2 || len(frames[1].Text) != 10 {
		t.Fatalf("second frame = %+v", frames[1])
	}
}

func TestRenderQQOutboundFrames(t *testing.T) {
	frames := RenderQQOutbound(OutboundRenderInput{
		Provider:                 ProviderQQ,
		ConversationID:           "qq:group:g1",
		ReplyToExternalMessageID: "qq-msg-1",
		Text:                     strings.Repeat("你", QQMaxMessageLen),
		SequenceStart:            200,
	})
	if len(frames) < 2 {
		t.Fatalf("frames len = %d, want split frames", len(frames))
	}
	if frames[0].Provider != ProviderQQ ||
		frames[0].TargetKind != "group" ||
		frames[0].TargetID != "g1" ||
		frames[0].ReplyToExternalMessageID != "qq-msg-1" ||
		frames[0].Sequence != 200 ||
		len(frames[0].Text) > QQMaxMessageLen {
		t.Fatalf("first frame = %+v", frames[0])
	}
	if frames[1].Sequence != 201 {
		t.Fatalf("second frame sequence = %d, want 201", frames[1].Sequence)
	}
}

func TestRenderProviderOutboundStreamFrames(t *testing.T) {
	frames, err := RenderProviderOutbound(OutboundRenderInput{
		Provider:       "qq",
		ConversationID: "qq:c2c:u1",
		StreamID:       "stream-1",
		Stream: &StreamState{
			Text:       "working",
			ActiveTool: "bash: go test",
		},
	})
	if err != nil {
		t.Fatalf("RenderProviderOutbound() error = %v", err)
	}
	if len(frames) != 1 ||
		frames[0].TargetKind != "c2c" ||
		frames[0].TargetID != "u1" ||
		frames[0].StreamID != "stream-1" ||
		frames[0].StreamState != qqStreamStateGenerating ||
		frames[0].Done ||
		!strings.Contains(frames[0].Text, "bash: go test") ||
		!strings.HasSuffix(frames[0].Text, typingCursor) {
		t.Fatalf("stream frame = %+v", frames)
	}
}

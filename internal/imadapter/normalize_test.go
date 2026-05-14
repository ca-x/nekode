package imadapter

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ca-x/nekode/internal/iminbound"
)

func TestNormalizeStellaProviderFixtures(t *testing.T) {
	normalizer := Normalizer{Now: func() time.Time { return time.Unix(100, 0) }}
	tests := []struct {
		name        string
		provider    string
		body        string
		wantText    string
		wantGroup   bool
		wantSender  string
		wantChat    string
		wantMsgID   string
		wantContent iminbound.ContentType
	}{
		{
			name:       "telegram text",
			provider:   ProviderTelegram,
			body:       `{"update_id":9,"message":{"message_id":10,"text":"hello tg","chat":{"id":-100,"type":"supergroup","title":"Ops"},"from":{"id":42,"username":"alice","first_name":"Alice"},"photo":[{"file_id":"p1"}]}}`,
			wantText:   "hello tg\n[image]",
			wantGroup:  true,
			wantSender: "42",
			wantChat:   "-100",
			wantMsgID:  "10",
		},
		{
			name:       "qq group image",
			provider:   ProviderQQ,
			body:       `{"id":"qq-msg","content":"hi qq","author":{"id":"u1","username":"Alice"},"group_id":"g1","attachments":[{"url":"https://example.test/a.png","filename":"a.png","content_type":"image/png","size":12}]}`,
			wantText:   "hi qq\n[image: a.png]",
			wantGroup:  true,
			wantSender: "u1",
			wantChat:   "g1",
			wantMsgID:  "qq-msg",
		},
		{
			name:       "feishu mention stripped",
			provider:   ProviderFeishu,
			body:       `{"header":{"event_id":"evt-1"},"event":{"sender":{"sender_id":{"open_id":"ou_1","union_id":"on_1"},"sender_type":"user"},"message":{"message_id":"om_1","chat_id":"oc_1","chat_type":"group","message_type":"text","content":"{\"text\":\"@_user_1 deploy\"}","mentions":[{"key":"@_user_1","id":{"open_id":"ou_bot"}}]}}}`,
			wantText:   "deploy",
			wantGroup:  true,
			wantSender: "on_1",
			wantChat:   "oc_1",
			wantMsgID:  "om_1",
		},
		{
			name:       "weixin mixed items",
			provider:   ProviderWeixin,
			body:       `{"message_id":12,"from_user_id":"wx-u","group_id":"wx-g","item_list":[{"type":1,"text_item":{"text":"hello wx"}},{"type":4,"file_item":{"file_name":"report.pdf","len":"10"}}]}`,
			wantText:   "hello wx\n[file: report.pdf]",
			wantGroup:  true,
			wantSender: "wx-u",
			wantChat:   "wx-g",
			wantMsgID:  "12",
		},
		{
			name:       "wecom callback file",
			provider:   ProviderWeCom,
			body:       `{"message_id":13,"from_user_id":"wecom-u","session_id":"wecom-u","item_list":[{"type":1,"text_item":{"text":"hello wecom"}},{"type":4,"file_item":{"file_name":"report.pdf","media_id":"mid-file"}}],"wecom":{"create_time":123,"msg_type":"file","agent_id":1000002}}`,
			wantText:   "hello wecom\n[file: report.pdf]",
			wantGroup:  false,
			wantSender: "wecom-u",
			wantChat:   "wecom-u",
			wantMsgID:  "13",
		},
		{
			name:       "terminal",
			provider:   ProviderTerminal,
			body:       `{"message_id":"term-1","session_id":"local-1","operator_id":"czyt","operator_name":"CZYT","text":"hello term","target":"#ops"}`,
			wantText:   "hello term",
			wantGroup:  false,
			wantSender: "czyt",
			wantChat:   "local-1",
			wantMsgID:  "term-1",
		},
		{
			name:       "serverchan",
			provider:   ProviderServerChan,
			body:       `{"update_id":12,"message":{"message_id":13,"text":"hello serverchan","chat":{"id":1001,"type":"private"},"from":{"id":42,"username":"alice","first_name":"Alice"}}}`,
			wantText:   "hello serverchan",
			wantGroup:  false,
			wantSender: "42",
			wantChat:   "1001",
			wantMsgID:  "13",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := normalizer.NormalizeInbound(context.Background(), iminbound.RawEvent{
				EndpointID: "ep-" + tt.provider,
				Provider:   tt.provider,
				Body:       []byte(tt.body),
			})
			if err != nil {
				t.Fatalf("NormalizeInbound() error = %v", err)
			}
			if msg.Provider != tt.provider || msg.ExternalMessageID != tt.wantMsgID {
				t.Fatalf("message provider/id = %s/%s", msg.Provider, msg.ExternalMessageID)
			}
			if msg.Conversation.ExternalID != tt.wantChat || msg.Conversation.IsGroup != tt.wantGroup {
				t.Fatalf("conversation = %+v", msg.Conversation)
			}
			if msg.Sender.ExternalID != tt.wantSender {
				t.Fatalf("sender = %+v", msg.Sender)
			}
			if got := msg.Text(); got != tt.wantText {
				t.Fatalf("Text() = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestNormalizeFeishuFallsBackToEventID(t *testing.T) {
	msg, err := (Normalizer{}).NormalizeInbound(context.Background(), iminbound.RawEvent{
		EndpointID: "ep-feishu",
		Provider:   ProviderFeishu,
		Body:       []byte(`{"header":{"event_id":"evt-1"},"event":{"sender":{"sender_id":{"open_id":"ou_1"}},"message":{"chat_id":"oc_1","chat_type":"p2p","message_type":"text","content":"{\"text\":\"hello\"}"}}}`),
	})
	if err != nil {
		t.Fatalf("NormalizeInbound() error = %v", err)
	}
	if msg.ExternalMessageID != "evt-1" {
		t.Fatalf("ExternalMessageID = %q, want event fallback", msg.ExternalMessageID)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(mustMetadataJSON(t, msg)), &metadata); err != nil {
		t.Fatalf("metadata invalid: %v", err)
	}
	if metadata["im"] == nil {
		t.Fatalf("metadata missing im envelope: %#v", metadata)
	}
}

func TestNormalizeUsesProviderMessageTimestamps(t *testing.T) {
	normalizer := Normalizer{Now: func() time.Time { return time.Unix(200, 0) }}
	tests := []struct {
		name     string
		provider string
		body     string
		wantUnix int64
	}{
		{
			name:     "telegram date",
			provider: ProviderTelegram,
			body:     `{"message":{"message_id":1,"date":123,"text":"hi","chat":{"id":2},"from":{"id":3}}}`,
			wantUnix: 123,
		},
		{
			name:     "qq rfc3339 timestamp",
			provider: ProviderQQ,
			body:     `{"id":"qq-1","timestamp":"1970-01-01T00:02:03Z","content":"hi","author":{"id":"u1"},"group_id":"g1"}`,
			wantUnix: 123,
		},
		{
			name:     "feishu millisecond create time",
			provider: ProviderFeishu,
			body:     `{"header":{"event_id":"evt-1"},"event":{"sender":{"sender_id":{"open_id":"ou_1"}},"message":{"message_id":"om_1","create_time":"123000","chat_id":"oc_1","chat_type":"p2p","message_type":"text","content":"{\"text\":\"hi\"}"}}}`,
			wantUnix: 123,
		},
		{
			name:     "weixin official create time",
			provider: ProviderWeixin,
			body:     `{"message_id":12,"from_user_id":"wx-u","session_id":"wx-s","official_account":{"create_time":123},"item_list":[{"type":1,"text_item":{"text":"hi"}}]}`,
			wantUnix: 123,
		},
		{
			name:     "wecom callback create time",
			provider: ProviderWeCom,
			body:     `{"message_id":13,"from_user_id":"wecom-u","session_id":"wecom-u","wecom":{"create_time":123},"item_list":[{"type":1,"text_item":{"text":"hi"}}]}`,
			wantUnix: 123,
		},
		{
			name:     "serverchan date",
			provider: ProviderServerChan,
			body:     `{"update_id":1,"message":{"message_id":2,"date":123,"text":"hi","chat":{"id":1001},"from":{"id":42}}}`,
			wantUnix: 123,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := normalizer.NormalizeInbound(context.Background(), iminbound.RawEvent{
				EndpointID: "ep-" + tt.provider,
				Provider:   tt.provider,
				Body:       []byte(tt.body),
			})
			if err != nil {
				t.Fatalf("NormalizeInbound() error = %v", err)
			}
			if msg.ReceivedUnix != tt.wantUnix {
				t.Fatalf("ReceivedUnix = %d, want %d", msg.ReceivedUnix, tt.wantUnix)
			}
		})
	}
}

func TestNormalizeWeixinPreservesMediaMetadata(t *testing.T) {
	msg, err := (Normalizer{}).NormalizeInbound(context.Background(), iminbound.RawEvent{
		EndpointID: "ep-weixin",
		Provider:   ProviderWeixin,
		Body:       []byte(`{"message_id":12,"from_user_id":"wx-u","session_id":"wx-s","item_list":[{"type":2,"image_item":{"url":"https://example.test/a.jpg","media_id":"mid-img"}},{"type":4,"file_item":{"file_name":"report.pdf","len":"10","media_id":"mid-file"}}]}`),
	})
	if err != nil {
		t.Fatalf("NormalizeInbound() error = %v", err)
	}
	if len(msg.Content) != 2 ||
		msg.Content[0].Type != iminbound.ContentTypeImage ||
		msg.Content[0].ExternalURL != "https://example.test/a.jpg" ||
		msg.Content[0].Metadata["media_id"] != "mid-img" ||
		msg.Content[1].Metadata["media_id"] != "mid-file" {
		t.Fatalf("weixin media content = %+v", msg.Content)
	}
}

func TestNormalizeProviderAliasesAndMetadataEnvelope(t *testing.T) {
	normalizer := Normalizer{Now: func() time.Time { return time.Unix(1234, 0) }}
	msg, err := normalizer.NormalizeInbound(context.Background(), iminbound.RawEvent{
		EndpointID:   " ep-wechat ",
		EndpointKind: "wechat",
		Body:         []byte(`{"seq":99,"from_user_id":"wx-u","session_id":"wx-s","item_list":[{"type":1,"text_item":{"text":"hello alias"}}]}`),
		Metadata:     map[string]any{"raw_provider": "wechat"},
	})
	if err != nil {
		t.Fatalf("NormalizeInbound(wechat alias) error = %v", err)
	}
	if msg.Provider != ProviderWeixin || msg.EndpointID != "ep-wechat" || msg.ExternalMessageID != "99" {
		t.Fatalf("normalized alias message = %+v", msg)
	}
	metadataJSON, err := msg.MetadataJSON()
	if err != nil {
		t.Fatalf("MetadataJSON() error = %v", err)
	}
	var metadata struct {
		Provider string `json:"provider"`
		IM       struct {
			Provider      string                 `json:"provider"`
			EndpointID    string                 `json:"endpoint_id"`
			ReceivedUnix  int64                  `json:"received_unix"`
			Conversation  map[string]any         `json:"conversation"`
			Sender        map[string]any         `json:"sender"`
			Content       []map[string]any       `json:"content"`
			UnknownFields map[string]interface{} `json:"-"`
		} `json:"im"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		t.Fatalf("metadata invalid: %v", err)
	}
	if metadata.Provider != ProviderWeixin ||
		metadata.IM.Provider != ProviderWeixin ||
		metadata.IM.EndpointID != "ep-wechat" ||
		metadata.IM.ReceivedUnix != 1234 ||
		metadata.IM.Conversation["external_id"] != "wx-s" ||
		metadata.IM.Sender["external_id"] != "wx-u" ||
		len(metadata.IM.Content) != 1 {
		t.Fatalf("metadata envelope = %#v from %s", metadata, metadataJSON)
	}
}

func TestNormalizeRejectsInvalidProviderMessages(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		body     string
	}{
		{
			name:     "telegram missing sender",
			provider: ProviderTelegram,
			body:     `{"message":{"message_id":1,"text":"hello","chat":{"id":2}}}`,
		},
		{
			name:     "terminal empty text",
			provider: ProviderTerminal,
			body:     `{"message_id":"m1","session_id":"s1","operator_id":"op1"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (Normalizer{}).NormalizeInbound(context.Background(), iminbound.RawEvent{
				EndpointID: "ep-" + tt.provider,
				Provider:   tt.provider,
				Body:       []byte(tt.body),
			}); err == nil {
				t.Fatalf("NormalizeInbound() error = nil, want invalid message")
			}
		})
	}
}

func mustMetadataJSON(t *testing.T, msg iminbound.Message) string {
	t.Helper()
	value, err := msg.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

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
			name:       "terminal",
			provider:   ProviderTerminal,
			body:       `{"message_id":"term-1","session_id":"local-1","operator_id":"czyt","operator_name":"CZYT","text":"hello term","target":"#ops"}`,
			wantText:   "hello term",
			wantGroup:  false,
			wantSender: "czyt",
			wantChat:   "local-1",
			wantMsgID:  "term-1",
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

func mustMetadataJSON(t *testing.T, msg iminbound.Message) string {
	t.Helper()
	value, err := msg.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

package imtelegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/daemonrpc"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/storage"
)

func TestWebhookValidatesSecretAndStoresTelegramMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createTelegramEndpoint(t, store)
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	webhook := Webhook{
		Config:      cfg,
		Coordinator: imcoord.New(store, nil),
		Now:         func() time.Time { return time.Unix(1700000010, 0) },
	}
	body := []byte(`{"update_id":100,"message":{"message_id":55,"text":"@nekode_bot deploy please","chat":{"id":-1001,"type":"supergroup","title":"Ops"},"from":{"id":42,"username":"alice","first_name":"Alice"}}}`)
	headers := http.Header{SecretTokenHeader: []string{"secret-1"}}
	result, err := webhook.Handle(ctx, headers, body)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Message.Target != "#ops" ||
		result.Message.ThreadID != "telegram-thread" ||
		result.Message.SourceEndpointID != endpoint.ID ||
		result.Message.ExternalMessageID != "55" ||
		result.Message.SenderDisplayName != "Alice" {
		t.Fatalf("stored message = %+v", result.Message)
	}

	_, err = webhook.Handle(ctx, http.Header{SecretTokenHeader: []string{"wrong"}}, body)
	if err == nil {
		t.Fatal("Handle(wrong secret) error = nil, want unauthorized")
	}
}

func TestWebhookGroupMentionModeCanIgnoreUnmentionedMessages(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createTelegramEndpoint(t, store)
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	webhook := Webhook{Config: cfg, Coordinator: imcoord.New(store, nil)}
	result, err := webhook.Handle(ctx, http.Header{SecretTokenHeader: []string{"secret-1"}}, []byte(`{"message":{"message_id":56,"text":"deploy please","chat":{"id":-1001,"type":"supergroup","title":"Ops"},"from":{"id":42,"username":"alice","first_name":"Alice"}}}`))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !result.Ignored || result.Reason == "" {
		t.Fatalf("Handle() = %+v, want ignored group message", result)
	}
}

func TestRuntimeSendsPendingTelegramDeliveryAndMarksDelivered(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createTelegramEndpoint(t, store)
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		ThreadID:          "telegram-thread",
		Role:              "user",
		Content:           "hello",
		SenderDisplayName: "Alice",
		SenderKind:        "endpoint",
		SourceEndpointID:  endpoint.ID,
		ExternalMessageID: "55",
		MetadataJSON:      `{"im":{"provider":"telegram","conversation":{"external_id":"-1001"},"sender":{"external_id":"42"}}}`,
		RequestID:         endpoint.ID + ":55",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}
	rpc := daemonrpc.New(store, "srv-telegram")
	reply, err := rpc.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           inbound.Target,
		Content:          "acknowledged via telegram",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "tg-reply",
		IdempotencyKey:   "tg-reply",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-telegram",
			DisplayName: "Telegram Agent",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	var requestBody map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottelegram-token/sendMessage" {
			t.Fatalf("telegram API path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode telegram request: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":9001}}`))
	}))
	t.Cleanup(api.Close)

	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	cfg.APIBaseURL = api.URL
	results, err := (Runtime{Config: cfg, Store: store, HTTPClient: api.Client()}).SendPending(ctx, 10)
	if err != nil {
		t.Fatalf("SendPending() error = %v", err)
	}
	if len(results) != 1 || results[0].Delivery.Status != "delivered" || len(results[0].Messages) != 1 {
		t.Fatalf("SendPending() = %+v, want one delivered Telegram send", results)
	}
	if requestBody["chat_id"] != "-1001" ||
		!strings.Contains(requestBody["text"].(string), "acknowledged via telegram") ||
		requestBody["parse_mode"] != "MarkdownV2" {
		t.Fatalf("telegram request = %#v", requestBody)
	}
	if reply.GetMessage().GetSourceEndpointId() != "" {
		t.Fatalf("reply source endpoint = %q, want web-visible reply", reply.GetMessage().GetSourceEndpointId())
	}
}

func createTelegramEndpoint(t *testing.T, store *storage.Store) storage.InteractionEndpoint {
	t.Helper()
	endpoint, err := store.CreateInteractionEndpoint(context.Background(), storage.InteractionEndpoint{
		ID:              "iep-telegram-live",
		Kind:            "im",
		Provider:        "telegram",
		DisplayName:     "Telegram Live",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "secret_token",
		ConfigJSON:      `{"token":"telegram-token","secret_token":"secret-1","bot_username":"nekode_bot","default_target":"#ops","default_thread_id":"telegram-thread","group_mode":"mention"}`,
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	return endpoint
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("imtelegram_test")+"?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}

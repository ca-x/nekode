package serverchan

import (
	"context"
	"encoding/json"
	"errors"
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

func TestRuntimeFetchesAndStoresServerChanMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createServerChanEndpoint(t, store)
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	runtime := Runtime{
		Config:      cfg,
		Store:       store,
		Coordinator: imcoord.New(store, nil),
		Now:         func() time.Time { return time.Unix(1700000030, 0) },
	}
	msg, err := runtime.HandleUpdate(ctx, Update{
		UpdateID: 101,
		Message: Message{
			MessageID: 202,
			Text:      "hello from serverchan",
			Chat:      MessageChat{ID: 1001, Type: "private"},
			From:      MessageFrom{ID: 42, Username: "alice", FirstName: "Alice"},
		},
	})
	if err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if msg.Target != "#ops" ||
		msg.ThreadID != "serverchan-thread" ||
		msg.SourceEndpointID != endpoint.ID ||
		msg.ExternalMessageID != "202" ||
		msg.SenderDisplayName != "Alice" {
		t.Fatalf("stored message = %+v", msg)
	}
}

func TestRuntimeIgnoresDisallowedServerChanSender(t *testing.T) {
	endpoint := storage.InteractionEndpoint{
		ID:         "iep-serverchan",
		ConfigJSON: `{"bot_token":"serverchan-token","allow_from":["1001"]}`,
	}
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	_, err = (Runtime{Config: cfg}).HandleUpdate(context.Background(), Update{
		UpdateID: 1,
		Message:  Message{MessageID: 2, Text: "blocked", Chat: MessageChat{ID: 2002}, From: MessageFrom{ID: 42}},
	})
	if !errors.Is(err, ErrIgnoredUpdate) {
		t.Fatalf("HandleUpdate(disallowed) error = %v, want ignored", err)
	}
}

func TestRuntimeFetchUpdatesUsesServerChanBotAPIShape(t *testing.T) {
	var gotPath, gotQuery string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":7,"message":{"message_id":8,"chat":{"id":1001},"from":{"id":42},"text":"ping"}}]}`))
	}))
	t.Cleanup(api.Close)

	updates, err := (Runtime{
		Config:     Config{BotToken: "serverchan-token", APIBaseURL: api.URL},
		HTTPClient: api.Client(),
	}).FetchUpdates(context.Background(), 6)
	if err != nil {
		t.Fatalf("FetchUpdates() error = %v", err)
	}
	if gotPath != "/botserverchan-token/getUpdates" || !strings.Contains(gotQuery, "offset=6") || len(updates) != 1 || updates[0].Message.Text != "ping" {
		t.Fatalf("FetchUpdates path=%q query=%q updates=%+v", gotPath, gotQuery, updates)
	}
}

func TestRuntimeSendsPendingServerChanDeliveryAndMarksDelivered(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createServerChanEndpoint(t, store)
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		ThreadID:          "serverchan-thread",
		Role:              "user",
		Content:           "hello",
		SenderDisplayName: "Alice",
		SenderKind:        "endpoint",
		SourceEndpointID:  endpoint.ID,
		ExternalMessageID: "202",
		MetadataJSON:      `{"im":{"provider":"serverchan","conversation":{"external_id":"1001"},"sender":{"external_id":"42"}}}`,
		RequestID:         endpoint.ID + ":202",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}
	rpc := daemonrpc.New(store, "srv-serverchan")
	reply, err := rpc.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           inbound.Target,
		Content:          "acknowledged via serverchan",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "serverchan-reply",
		IdempotencyKey:   "serverchan-reply",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-serverchan",
			DisplayName: "ServerChan Agent",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	var requestBody map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botserverchan-token/sendMessage" {
			t.Fatalf("serverchan API path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode serverchan request: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":9002}}`))
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
		t.Fatalf("SendPending() = %+v, want one delivered ServerChan send", results)
	}
	if requestBody["chat_id"] != float64(1001) ||
		!strings.Contains(requestBody["text"].(string), "acknowledged via serverchan") ||
		requestBody["parse_mode"] != "markdown" {
		t.Fatalf("serverchan request = %#v", requestBody)
	}
	if reply.GetMessage().GetSourceEndpointId() != "" {
		t.Fatalf("reply source endpoint = %q, want web-visible reply", reply.GetMessage().GetSourceEndpointId())
	}
}

func createServerChanEndpoint(t *testing.T, store *storage.Store) storage.InteractionEndpoint {
	t.Helper()
	endpoint, err := store.CreateInteractionEndpoint(context.Background(), storage.InteractionEndpoint{
		ID:              "iep-serverchan-live",
		Kind:            "im",
		Provider:        "serverchan",
		DisplayName:     "ServerChan Live",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "bot_token",
		ConfigJSON:      `{"bot_token":"serverchan-token","default_target":"#ops","default_thread_id":"serverchan-thread"}`,
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	return endpoint
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("imserverchan_test")+"?mode=memory&cache=shared&_fk=1")
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

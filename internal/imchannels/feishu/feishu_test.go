package feishu

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

func TestCallbackValidatesChallengeToken(t *testing.T) {
	webhook := Callback{Config: Config{VerificationToken: "verify-token"}}
	result, err := webhook.Handle(context.Background(), nil, []byte(`{"challenge":"challenge-code","token":"verify-token","type":"url_verification"}`))
	if err != nil {
		t.Fatalf("Handle(challenge) error = %v", err)
	}
	if result.Challenge != "challenge-code" {
		t.Fatalf("challenge = %q, want challenge-code", result.Challenge)
	}

	_, err = webhook.Handle(context.Background(), nil, []byte(`{"challenge":"challenge-code","token":"wrong","type":"url_verification"}`))
	if !errors.Is(err, ErrUnauthorizedCallback) {
		t.Fatalf("Handle(wrong token) error = %v, want unauthorized", err)
	}
}

func TestCallbackValidatesTokenAndStoresFeishuMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createFeishuEndpoint(t, store)
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	webhook := Callback{
		Config:      cfg,
		Coordinator: imcoord.New(store, nil),
		Now:         func() time.Time { return time.Unix(1700000020, 0) },
	}
	body := []byte(`{"schema":"2.0","header":{"event_id":"evt-1","event_type":"im.message.receive_v1","token":"verify-token","tenant_key":"tenant-1"},"event":{"sender":{"sender_id":{"open_id":"ou_alice","union_id":"on_alice","user_id":"u_alice"},"sender_type":"user"},"message":{"message_id":"om_msg_1","root_id":"","chat_id":"oc_ops","chat_type":"group","message_type":"text","content":"{\"text\":\"@_user_1 deploy please\"}","mentions":[{"key":"@_user_1","name":"Nekode"}]}}}`)
	result, err := webhook.Handle(ctx, http.Header{}, body)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Message.Target != "#ops" ||
		result.Message.ThreadID != "feishu-thread" ||
		result.Message.SourceEndpointID != endpoint.ID ||
		result.Message.ExternalMessageID != "om_msg_1" ||
		result.Message.Content != "deploy please" {
		t.Fatalf("stored message = %+v", result.Message)
	}

	_, err = webhook.Handle(ctx, http.Header{}, []byte(`{"schema":"2.0","header":{"event_id":"evt-2","event_type":"im.message.receive_v1","token":"wrong"},"event":{"sender":{"sender_id":{"open_id":"ou_alice"}},"message":{"message_id":"om_msg_2","chat_id":"oc_ops","chat_type":"p2p","message_type":"text","content":"{\"text\":\"hello\"}"}}}`))
	if !errors.Is(err, ErrUnauthorizedCallback) {
		t.Fatalf("Handle(wrong token) error = %v, want unauthorized", err)
	}
}

func TestCallbackRejectsEncryptedPayloadUntilDecryptSupportExists(t *testing.T) {
	_, err := (Callback{Config: Config{VerificationToken: "verify-token"}}).Handle(context.Background(), nil, []byte(`{"encrypt":"ciphertext"}`))
	if !errors.Is(err, ErrEncryptedUnsupported) {
		t.Fatalf("Handle(encrypt) error = %v, want encrypted unsupported", err)
	}
}

func TestCallbackGroupMentionModeCanIgnoreUnmentionedMessages(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createFeishuEndpoint(t, store)
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	webhook := Callback{Config: cfg, Coordinator: imcoord.New(store, nil)}
	body := []byte(`{"schema":"2.0","header":{"event_id":"evt-ignore","event_type":"im.message.receive_v1","token":"verify-token"},"event":{"sender":{"sender_id":{"open_id":"ou_alice"},"sender_type":"user"},"message":{"message_id":"om_ignore","chat_id":"oc_ops","chat_type":"group","message_type":"text","content":"{\"text\":\"deploy please\"}"}}}`)
	result, err := webhook.Handle(ctx, nil, body)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !result.Ignored || result.Reason == "" {
		t.Fatalf("Handle() = %+v, want ignored group message", result)
	}
}

func TestRuntimeSendsPendingFeishuDeliveryAndMarksDelivered(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createFeishuEndpoint(t, store)
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		ThreadID:          "feishu-thread",
		Role:              "user",
		Content:           "hello",
		SenderDisplayName: "Alice",
		SenderKind:        "endpoint",
		SourceEndpointID:  endpoint.ID,
		ExternalMessageID: "om_msg_1",
		MetadataJSON:      `{"im":{"provider":"feishu","conversation":{"external_id":"oc_ops"},"sender":{"external_id":"ou_alice"}}}`,
		RequestID:         endpoint.ID + ":om_msg_1",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}
	rpc := daemonrpc.New(store, "srv-feishu")
	reply, err := rpc.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           inbound.Target,
		Content:          "acknowledged via feishu",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "fs-reply",
		IdempotencyKey:   "fs-reply",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-feishu",
			DisplayName: "Feishu Agent",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	var tokenRequest map[string]any
	var sendRequest map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			if err := json.NewDecoder(r.Body).Decode(&tokenRequest); err != nil {
				t.Fatalf("decode token request: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"tenant-token","expire":7200}`))
		case "/open-apis/im/v1/messages":
			if auth := r.Header.Get("Authorization"); auth != "Bearer tenant-token" {
				t.Fatalf("Authorization = %q, want bearer token", auth)
			}
			if got := r.URL.Query().Get("receive_id_type"); got != "chat_id" {
				t.Fatalf("receive_id_type = %q, want chat_id", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&sendRequest); err != nil {
				t.Fatalf("decode send request: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"message_id":"om_sent_1"}}`))
		default:
			t.Fatalf("unexpected Feishu API path = %q", r.URL.Path)
		}
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
		t.Fatalf("SendPending() = %+v, want one delivered Feishu send", results)
	}
	if tokenRequest["app_id"] != "cli_a" || tokenRequest["app_secret"] != "secret-a" {
		t.Fatalf("token request = %#v", tokenRequest)
	}
	if sendRequest["receive_id"] != "oc_ops" || sendRequest["msg_type"] != "text" ||
		!strings.Contains(sendRequest["content"].(string), "acknowledged via feishu") {
		t.Fatalf("send request = %#v", sendRequest)
	}
	if reply.GetMessage().GetSourceEndpointId() != "" {
		t.Fatalf("reply source endpoint = %q, want web-visible reply", reply.GetMessage().GetSourceEndpointId())
	}
}

func createFeishuEndpoint(t *testing.T, store *storage.Store) storage.InteractionEndpoint {
	t.Helper()
	endpoint, err := store.CreateInteractionEndpoint(context.Background(), storage.InteractionEndpoint{
		ID:              "iep-feishu-live",
		Kind:            "im",
		Provider:        "feishu",
		DisplayName:     "Feishu Live",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "verification_token",
		ConfigJSON:      `{"app_id":"cli_a","app_secret":"secret-a","verification_token":"verify-token","default_target":"#ops","default_thread_id":"feishu-thread","group_mode":"mention"}`,
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	return endpoint
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("imfeishu_test")+"?mode=memory&cache=shared&_fk=1")
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

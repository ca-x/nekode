package weixin

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
	"github.com/ca-x/nekode/internal/imbinding"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/storage"
)

func TestWebhookVerifiesOfficialAccountURL(t *testing.T) {
	query := Query{Timestamp: "1700000000", Nonce: "nonce-1", Echo: "hello"}
	query.Signature = Signature("wechat-token", query.Timestamp, query.Nonce)
	echo, err := (Webhook{Config: Config{Token: "wechat-token"}}).VerifyURL(query)
	if err != nil {
		t.Fatalf("VerifyURL() error = %v", err)
	}
	if echo != "hello" {
		t.Fatalf("VerifyURL() = %q, want echo", echo)
	}
	query.Signature = "bad"
	if _, err := (Webhook{Config: Config{Token: "wechat-token"}}).VerifyURL(query); err == nil {
		t.Fatal("VerifyURL(bad signature) error = nil")
	}
}

func TestWebhookStoresOfficialAccountTextMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createWeChatEndpoint(t, store)
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	query := Query{Timestamp: "1700000010", Nonce: "nonce-2"}
	query.Signature = Signature("wechat-token", query.Timestamp, query.Nonce)
	body := []byte(`<xml><ToUserName><![CDATA[gh_app]]></ToUserName><FromUserName><![CDATA[openid-1]]></FromUserName><CreateTime>1700000010</CreateTime><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[hello official account]]></Content><MsgId>123456</MsgId></xml>`)
	result, err := (Webhook{
		Config:      cfg,
		Coordinator: imcoord.New(store, nil),
		Now:         func() time.Time { return time.Unix(1700000010, 0) },
	}).Handle(ctx, query, body)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Message.Target != "#ops" ||
		result.Message.ThreadID != "wechat-thread" ||
		result.Message.SourceEndpointID != endpoint.ID ||
		result.Message.ExternalMessageID != "123456" ||
		!strings.Contains(result.Message.Content, "hello official account") {
		t.Fatalf("stored message = %+v", result.Message)
	}

	badQuery := query
	badQuery.Signature = "bad"
	if _, err := (Webhook{Config: cfg, Coordinator: imcoord.New(store, nil)}).Handle(ctx, badQuery, body); err == nil {
		t.Fatal("Handle(bad signature) error = nil")
	}
}

func TestRuntimeSendsCustomerServiceMessageAndMarksDelivered(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createWeChatEndpoint(t, store)
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		ThreadID:          "wechat-thread",
		Role:              "user",
		Content:           "hello",
		SenderDisplayName: "openid-1",
		SenderKind:        "endpoint",
		SourceEndpointID:  endpoint.ID,
		ExternalMessageID: "123456",
		MetadataJSON:      `{"im":{"provider":"weixin","conversation":{"external_id":"openid-1"},"sender":{"external_id":"openid-1"}}}`,
		RequestID:         endpoint.ID + ":123456",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}
	rpc := daemonrpc.New(store, "srv-wechat")
	_, err = rpc.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           inbound.Target,
		Content:          "acknowledged via wechat",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "wechat-reply",
		IdempotencyKey:   "wechat-reply",
		Sender:           &daemonv1.Actor{ActorKind: daemonv1.ActorKind_ACTOR_KIND_AGENT, AgentId: "agent-wechat", DisplayName: "WeChat Agent"},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	var tokenRequested bool
	var sendBody map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			tokenRequested = true
			if r.URL.Query().Get("appid") != "wx-app" || r.URL.Query().Get("secret") != "wx-secret" {
				t.Fatalf("token query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"access_token":"access-1","expires_in":7200}`))
		case "/cgi-bin/message/custom/send":
			if r.URL.Query().Get("access_token") != "access-1" {
				t.Fatalf("send access_token = %q", r.URL.Query().Get("access_token"))
			}
			if err := json.NewDecoder(r.Body).Decode(&sendBody); err != nil {
				t.Fatalf("decode send body: %v", err)
			}
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		default:
			t.Fatalf("unexpected WeChat API path %q", r.URL.Path)
		}
	}))
	t.Cleanup(api.Close)

	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	cfg.APIBaseURL = api.URL
	results, err := (Runtime{Config: cfg, Store: store, HTTPClient: api.Client(), TokenCache: &TokenCache{}}).SendPending(ctx, 10)
	if err != nil {
		t.Fatalf("SendPending() error = %v", err)
	}
	if !tokenRequested || len(results) != 1 || results[0].Delivery.Status != "delivered" {
		t.Fatalf("SendPending() = %+v tokenRequested=%v", results, tokenRequested)
	}
	if sendBody["touser"] != "openid-1" || sendBody["msgtype"] != "text" {
		t.Fatalf("send body = %#v", sendBody)
	}
	text, _ := sendBody["text"].(map[string]any)
	if !strings.Contains(text["content"].(string), "acknowledged via wechat") {
		t.Fatalf("send text = %#v", text)
	}
}

func TestILinkBindingSessionFetchesQRAndPersistsBoundConfig(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			_, _ = w.Write([]byte(`{"qrcode":"qr-ticket-1","qrcode_img_content":"base64-png"}`))
		case "/ilink/bot/get_qrcode_status":
			if r.URL.Query().Get("qrcode") != "qr-ticket-1" {
				t.Fatalf("qrcode query = %q", r.URL.Query().Get("qrcode"))
			}
			_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"wx-token","ilink_bot_id":"bot-1","ilink_user_id":"user-1","baseurl":"https://wx-bound.example"}`))
		default:
			t.Fatalf("unexpected iLink API path %q", r.URL.Path)
		}
	}))
	t.Cleanup(api.Close)

	endpoint := storage.InteractionEndpoint{
		ID:         "iep-weixin-ilink",
		Kind:       "im",
		Provider:   "weixin",
		ConfigJSON: `{"mode":"ilink","base_url":"` + api.URL + `"}`,
	}
	bindings := imbinding.NewStore(time.Minute)
	session, err := bindings.Create(endpoint, imbinding.MethodQRCode)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	started, err := StartILinkBindingSession(endpoint, bindings, session)
	if err != nil {
		t.Fatalf("StartILinkBindingSession() error = %v", err)
	}
	if started.QRPayload != "qr-ticket-1" || started.QRImageURL != "data:image/png;base64,base64-png" || started.Status != imbinding.StatusPending {
		t.Fatalf("started session = %+v", started)
	}
	result, err := PollILinkBindingSession(endpoint, bindings, started)
	if err != nil {
		t.Fatalf("PollILinkBindingSession() error = %v", err)
	}
	if !result.Bound || result.Session.Status != imbinding.StatusBound {
		t.Fatalf("binding result = %+v, want bound", result)
	}
	cfg, err := ConfigFromEndpoint(storage.InteractionEndpoint{ID: endpoint.ID, ConfigJSON: result.ConfigJSON})
	if err != nil {
		t.Fatalf("ConfigFromEndpoint(bound config) error = %v", err)
	}
	if cfg.BotToken != "wx-token" || cfg.BotID != "bot-1" || cfg.UserID != "user-1" || cfg.BaseURL != "https://wx-bound.example" {
		t.Fatalf("bound config = %+v from %s", cfg, result.ConfigJSON)
	}
}

func TestRuntimeSendsILinkMessageOnlyAfterBinding(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint, err := store.CreateInteractionEndpoint(ctx, storage.InteractionEndpoint{
		ID:              "iep-weixin-ilink",
		Kind:            "im",
		Provider:        "weixin",
		DisplayName:     "Weixin iLink",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "ilink_bot_token",
		ConfigJSON:      `{"mode":"ilink","bot_token":"wx-token","user_id":"wx-user-1","default_target":"#ops"}`,
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		Role:              "user",
		Content:           "hello",
		SenderDisplayName: "wx-user-1",
		SenderKind:        "endpoint",
		SourceEndpointID:  endpoint.ID,
		ExternalMessageID: "wx-msg-1",
		MetadataJSON:      `{"im":{"provider":"weixin","conversation":{"external_id":"wx-user-1"},"sender":{"external_id":"wx-user-1"}}}`,
		RequestID:         endpoint.ID + ":wx-msg-1",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}
	_, err = daemonrpc.New(store, "srv-weixin-ilink").SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           inbound.Target,
		Content:          "acknowledged via ilink",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "ilink-reply",
		IdempotencyKey:   "ilink-reply",
		Sender:           &daemonv1.Actor{ActorKind: daemonv1.ActorKind_ACTOR_KIND_AGENT, AgentId: "agent-weixin", DisplayName: "Weixin Agent"},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	var sendBody map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Fatalf("unexpected iLink API path %q", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer wx-token" {
			t.Fatalf("Authorization = %q, want Bearer token", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&sendBody); err != nil {
			t.Fatalf("decode send body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	t.Cleanup(api.Close)

	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	cfg.BaseURL = api.URL
	results, err := (Runtime{Config: cfg, Store: store}).SendPending(ctx, 10)
	if err != nil {
		t.Fatalf("SendPending() error = %v", err)
	}
	if len(results) != 1 || results[0].Delivery.Status != "delivered" {
		t.Fatalf("SendPending() = %+v, want delivered", results)
	}
	msg, _ := sendBody["msg"].(map[string]any)
	if msg["to_user_id"] != "wx-user-1" || msg["message_type"] != float64(ILinkMessageTypeBot) || msg["message_state"] != float64(ILinkMessageStateFinish) {
		t.Fatalf("ilink send msg = %#v", msg)
	}
}

func createWeChatEndpoint(t *testing.T, store *storage.Store) storage.InteractionEndpoint {
	t.Helper()
	endpoint, err := store.CreateInteractionEndpoint(context.Background(), storage.InteractionEndpoint{
		ID:              "iep-wechat-live",
		Kind:            "im",
		Provider:        "weixin",
		DisplayName:     "WeChat Official Account",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "webhook_signature",
		ConfigJSON:      `{"mode":"official_account","app_id":"wx-app","app_secret":"wx-secret","token":"wechat-token","default_target":"#ops","default_thread_id":"wechat-thread"}`,
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	return endpoint
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("imwechat_test")+"?mode=memory&cache=shared&_fk=1")
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

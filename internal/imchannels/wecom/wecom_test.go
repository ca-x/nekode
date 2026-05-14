package wecom

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/daemonrpc"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/storage"
)

const testAESKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestVerifyURLDecryptsEcho(t *testing.T) {
	encrypted := encryptForTest(t, testAESKey, "ww-test", "verify-ok")
	query := Query{Timestamp: "1700000000", Nonce: "nonce-1", Echo: encrypted}
	query.MsgSignature = Signature("cb-token", query.Timestamp, query.Nonce, encrypted)
	echo, err := (Webhook{Config: Config{CorpID: "ww-test", CallbackToken: "cb-token", CallbackAESKey: testAESKey}}).VerifyURL(query)
	if err != nil {
		t.Fatalf("VerifyURL() error = %v", err)
	}
	if echo != "verify-ok" {
		t.Fatalf("VerifyURL() = %q, want echo", echo)
	}
	query.MsgSignature = "bad"
	if _, err := (Webhook{Config: Config{CorpID: "ww-test", CallbackToken: "cb-token", CallbackAESKey: testAESKey}}).VerifyURL(query); err == nil {
		t.Fatal("VerifyURL(bad signature) error = nil")
	}
}

func TestWebhookStoresEncryptedTextMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createWeComEndpoint(t, store)
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	plain := `<xml><ToUserName><![CDATA[ww-test]]></ToUserName><FromUserName><![CDATA[user-1]]></FromUserName><CreateTime>1700000010</CreateTime><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[<@bot> hello wecom]]></Content><MsgId>123456</MsgId><AgentID>1000002</AgentID></xml>`
	encrypted := encryptForTest(t, testAESKey, "ww-test", plain)
	query := Query{Timestamp: "1700000010", Nonce: "nonce-2", Echo: encrypted}
	query.MsgSignature = Signature("cb-token", query.Timestamp, query.Nonce, encrypted)
	body := []byte(`<xml><ToUserName><![CDATA[ww-test]]></ToUserName><AgentID><![CDATA[1000002]]></AgentID><Encrypt><![CDATA[` + encrypted + `]]></Encrypt></xml>`)
	result, err := (Webhook{
		Config:      cfg,
		Coordinator: imcoord.New(store, nil),
		Now:         func() time.Time { return time.Unix(1700000010, 0) },
	}).Handle(ctx, query, body)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Message.Target != "#ops" ||
		result.Message.ThreadID != "wecom-thread" ||
		result.Message.SourceEndpointID != endpoint.ID ||
		result.Message.ExternalMessageID != "123456" ||
		result.Message.Content != "hello wecom" {
		t.Fatalf("stored message = %+v", result.Message)
	}
}

func TestRuntimeSendsAppMessageAndMarksDelivered(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createWeComEndpoint(t, store)
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		ThreadID:          "wecom-thread",
		Role:              "user",
		Content:           "hello",
		SenderDisplayName: "user-1",
		SenderKind:        "endpoint",
		SourceEndpointID:  endpoint.ID,
		ExternalMessageID: "123456",
		MetadataJSON:      `{"im":{"provider":"wecom","conversation":{"external_id":"user-1"},"sender":{"external_id":"user-1"}}}`,
		RequestID:         endpoint.ID + ":123456",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}
	rpc := daemonrpc.New(store, "srv-wecom")
	if _, err := rpc.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           inbound.Target,
		Content:          "**acknowledged** via wecom",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "wecom-reply",
		IdempotencyKey:   "wecom-reply",
		Sender:           &daemonv1.Actor{ActorKind: daemonv1.ActorKind_ACTOR_KIND_AGENT, AgentId: "agent-wecom", DisplayName: "WeCom Agent"},
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	var tokenRequested bool
	var sendBody map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			tokenRequested = true
			if r.URL.Query().Get("corpid") != "ww-test" || r.URL.Query().Get("corpsecret") != "corp-secret" {
				t.Fatalf("token query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"access-1","expires_in":7200}`))
		case "/cgi-bin/message/send":
			if r.URL.Query().Get("access_token") != "access-1" {
				t.Fatalf("send access_token = %q", r.URL.Query().Get("access_token"))
			}
			if err := json.NewDecoder(r.Body).Decode(&sendBody); err != nil {
				t.Fatalf("decode send body: %v", err)
			}
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","msgid":"msg-1"}`))
		default:
			t.Fatalf("unexpected WeCom API path %q", r.URL.Path)
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
	if sendBody["touser"] != "user-1" || sendBody["msgtype"] != "text" || sendBody["agentid"] != float64(1000002) {
		t.Fatalf("send body = %#v", sendBody)
	}
	text, _ := sendBody["text"].(map[string]any)
	if text["content"] != "acknowledged via wecom" {
		t.Fatalf("send text = %#v", text)
	}
}

func TestRuntimeRecordsRetryingFailure(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createWeComEndpoint(t, store)
	msg, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		Role:              "agent",
		Content:           "retry me",
		SenderAgentID:     "agent-1",
		SenderDisplayName: "agent",
		SenderKind:        "agent",
		MetadataJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	delivery, err := store.CreateOutboundDelivery(ctx, storage.OutboundDelivery{
		Target:       msg.Target,
		MessageID:    msg.ID,
		EndpointID:   endpoint.ID,
		EndpointKind: endpoint.Kind,
		Status:       "pending",
		RequestID:    "wecom-retry",
	})
	if err != nil {
		t.Fatalf("CreateOutboundDelivery() error = %v", err)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"access-1","expires_in":7200}`))
		case "/cgi-bin/message/send":
			http.Error(w, "temporary failure", http.StatusBadGateway)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(api.Close)
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	cfg.APIBaseURL = api.URL
	cfg.DefaultUserID = "user-1"
	result, err := (Runtime{Config: cfg, Store: store, HTTPClient: api.Client(), TokenCache: &TokenCache{}, MaxAttempts: 2}).SendDelivery(ctx, delivery)
	if err == nil {
		t.Fatal("SendDelivery() error = nil, want transient send failure")
	}
	if result.Delivery.Status != "retrying" || result.Delivery.AttemptCount != 1 || result.Delivery.NextRetryTimeUnix == 0 {
		t.Fatalf("delivery after failure = %+v, want retrying", result.Delivery)
	}
}

func createWeComEndpoint(t *testing.T, store *storage.Store) storage.InteractionEndpoint {
	t.Helper()
	endpoint, err := store.CreateInteractionEndpoint(context.Background(), storage.InteractionEndpoint{
		ID:              "iep-wecom-live",
		Kind:            "im",
		Provider:        "wecom",
		DisplayName:     "WeChat Work",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "webhook_signature",
		ConfigJSON:      `{"mode":"callback_app","corp_id":"ww-test","corp_secret":"corp-secret","agent_id":"1000002","callback_token":"cb-token","callback_aes_key":"` + testAESKey + `","default_target":"#ops","default_thread_id":"wecom-thread"}`,
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	return endpoint
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("imwecom_test")+"?mode=memory&cache=shared&_fk=1")
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

func encryptForTest(t *testing.T, encodingAESKey, receiveID, plain string) string {
	t.Helper()
	key, err := decodeAESKey(encodingAESKey)
	if err != nil {
		t.Fatalf("decodeAESKey() error = %v", err)
	}
	buf := bytes.NewBufferString("0123456789abcdef")
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(plain)))
	buf.Write(lenBuf[:])
	buf.WriteString(plain)
	buf.WriteString(receiveID)
	padded := pkcs7Pad(buf.Bytes(), aes.BlockSize)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(out, padded)
	return base64.StdEncoding.EncodeToString(out)
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

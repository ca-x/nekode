package qq

import (
	"context"
	"testing"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/daemonrpc"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/storage"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/openapi/options"
)

func TestRuntimeStoresQQGroupMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createQQEndpoint(t, store)
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	msg, err := (Runtime{
		Config:      cfg,
		Store:       store,
		Coordinator: imcoord.New(store, nil),
		Now:         func() time.Time { return time.Unix(1700000040, 0) },
	}).HandleGroupMessage(ctx, &dto.Message{
		ID:      "qq-msg-1",
		GroupID: "g1",
		Content: "hello qq",
		Author:  &dto.User{ID: "u1", Username: "Alice"},
	})
	if err != nil {
		t.Fatalf("HandleGroupMessage() error = %v", err)
	}
	if msg.Target != "#ops" ||
		msg.ThreadID != "qq-thread" ||
		msg.SourceEndpointID != endpoint.ID ||
		msg.ExternalMessageID != "qq-msg-1" ||
		msg.SenderDisplayName != "Alice" {
		t.Fatalf("stored message = %+v", msg)
	}
}

func TestRuntimeSendsPendingQQDeliveryAndMarksDelivered(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	endpoint := createQQEndpoint(t, store)
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		ThreadID:          "qq-thread",
		Role:              "user",
		Content:           "hello",
		SenderDisplayName: "Alice",
		SenderKind:        "endpoint",
		SourceEndpointID:  endpoint.ID,
		ExternalMessageID: "qq-msg-1",
		MetadataJSON:      `{"im":{"provider":"qq","conversation":{"external_id":"qq:group:g1"},"sender":{"external_id":"u1"}}}`,
		RequestID:         endpoint.ID + ":qq-msg-1",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}
	rpc := daemonrpc.New(store, "srv-qq")
	reply, err := rpc.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           inbound.Target,
		Content:          "acknowledged via qq",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "qq-reply",
		IdempotencyKey:   "qq-reply",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-qq",
			DisplayName: "QQ Agent",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	api := &fakeQQAPI{}
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		t.Fatalf("ConfigFromEndpoint() error = %v", err)
	}
	results, err := (Runtime{Config: cfg, Store: store, API: api, SequenceStart: 321}).SendPending(ctx, 10)
	if err != nil {
		t.Fatalf("SendPending() error = %v", err)
	}
	if len(results) != 1 || results[0].Delivery.Status != "delivered" || len(results[0].Messages) != 1 {
		t.Fatalf("SendPending() = %+v, want one delivered QQ send", results)
	}
	if api.groupID != "g1" || api.c2cUserID != "" || api.message.Content != "acknowledged via qq" || api.message.MsgSeq != 321 {
		t.Fatalf("qq send api groupID=%q c2c=%q message=%+v", api.groupID, api.c2cUserID, api.message)
	}
	if reply.GetMessage().GetSourceEndpointId() != "" {
		t.Fatalf("reply source endpoint = %q, want web-visible reply", reply.GetMessage().GetSourceEndpointId())
	}
}

type fakeQQAPI struct {
	groupID   string
	c2cUserID string
	message   dto.MessageToCreate
}

func (f *fakeQQAPI) WS(context.Context, map[string]string, string) (*dto.WebsocketAP, error) {
	return &dto.WebsocketAP{}, nil
}

func (f *fakeQQAPI) PostGroupMessage(_ context.Context, groupID string, msg dto.APIMessage, _ ...options.Option) (*dto.Message, error) {
	f.groupID = groupID
	f.message = msg.(dto.MessageToCreate)
	return &dto.Message{ID: "qq-sent-1", GroupID: groupID}, nil
}

func (f *fakeQQAPI) PostC2CMessage(_ context.Context, userID string, msg dto.APIMessage, _ ...options.Option) (*dto.Message, error) {
	f.c2cUserID = userID
	f.message = msg.(dto.MessageToCreate)
	return &dto.Message{ID: "qq-sent-1", ChannelID: userID}, nil
}

func createQQEndpoint(t *testing.T, store *storage.Store) storage.InteractionEndpoint {
	t.Helper()
	endpoint, err := store.CreateInteractionEndpoint(context.Background(), storage.InteractionEndpoint{
		ID:              "iep-qq-live",
		Kind:            "im",
		Provider:        "qq",
		DisplayName:     "QQ Live",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "botgo",
		ConfigJSON:      `{"app_id":"qq-app","app_secret":"qq-secret","default_target":"#ops","default_thread_id":"qq-thread","group_mode":"mention","default_target_id":"qq:group:g1"}`,
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	return endpoint
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("imqq_test")+"?mode=memory&cache=shared&_fk=1")
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

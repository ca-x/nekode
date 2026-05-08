package imgate

import (
	"context"
	"strings"
	"testing"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/daemonrpc"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/imterminal"
	"github.com/ca-x/nekode/internal/storage"
)

func TestTerminalLocalChannelLiveSmoke(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	rpc := daemonrpc.New(store, "srv-terminal-live")
	coord := imcoord.New(store, nil)

	endpoint, err := store.CreateInteractionEndpoint(ctx, storage.InteractionEndpoint{
		ID:              "iep-terminal-live",
		Kind:            "im",
		Provider:        "terminal",
		DisplayName:     "Local Terminal",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "none",
		ConfigJSON:      `{"runtime":"local","group_mode":"always"}`,
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}

	channel := imterminal.Channel{
		Config: imterminal.Config{
			EndpointID:   endpoint.ID,
			SessionID:    "local-session-1",
			OperatorID:   "operator-1",
			OperatorName: "Local Operator",
			Target:       "#ops",
			ThreadID:     "terminal-thread-1",
		},
		Now: func() time.Time { return time.Unix(1700000000, 0) },
	}
	inbound, err := channel.NormalizeInbound(imterminal.Input{
		MessageID: "term-live-1",
		Text:      "please check local smoke",
	})
	if err != nil {
		t.Fatalf("NormalizeInbound() error = %v", err)
	}
	draft, err := inbound.Draft()
	if err != nil {
		t.Fatalf("Draft() error = %v", err)
	}
	created, err := coord.Handle(ctx, draft)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if created.Message.SourceEndpointID != endpoint.ID ||
		created.Message.ExternalMessageID != "term-live-1" ||
		created.Message.Target != "#ops" ||
		created.Message.ThreadID != "terminal-thread-1" {
		t.Fatalf("created terminal message = %+v, want endpoint source on bound #ops thread", created.Message)
	}

	reply, err := rpc.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           created.Message.Target,
		Content:          "terminal smoke acknowledged",
		ReplyToMessageId: created.Message.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "terminal-smoke-reply",
		IdempotencyKey:   "terminal-smoke-reply",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-terminal-smoke",
			DisplayName: "Terminal Smoke Agent",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if !reply.GetAccepted() || reply.GetMessage().GetSourceEndpointId() != "" {
		t.Fatalf("reply = %+v, want Web-visible reply without copied terminal source", reply.GetMessage())
	}

	listed, err := rpc.ListOutboundDeliveries(ctx, &daemonv1.ListOutboundDeliveriesRequest{
		Target:    created.Message.Target,
		MessageId: reply.GetMessage().GetMessageId(),
		Statuses:  []daemonv1.OutboundDeliveryStatus{daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING},
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListOutboundDeliveries() error = %v", err)
	}
	if len(listed.GetDeliveries()) != 1 {
		t.Fatalf("deliveries = %+v, want one pending terminal source delivery", listed.GetDeliveries())
	}
	delivery := listed.GetDeliveries()[0]
	if delivery.GetEndpointId() != endpoint.ID ||
		delivery.GetEndpointKind() != "im" ||
		delivery.GetExternalMessageId() != "term-live-1" ||
		delivery.GetStatus() != daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING {
		t.Fatalf("delivery = %+v, want pending terminal delivery to source message", delivery)
	}

	replyMessage, err := store.GetMessage(ctx, created.Message.Target, reply.GetMessage().GetMessageId())
	if err != nil {
		t.Fatalf("GetMessage(reply) error = %v", err)
	}
	deliveryRecord, err := store.GetOutboundDelivery(ctx, delivery.GetDeliveryId())
	if err != nil {
		t.Fatalf("GetOutboundDelivery() error = %v", err)
	}
	rendered := imterminal.RenderOutbound(replyMessage, deliveryRecord)
	if !strings.Contains(rendered, "Terminal Smoke Agent: terminal smoke acknowledged") ||
		!strings.Contains(rendered, "[pending delivery "+delivery.GetDeliveryId()+" -> term-live-1]") {
		t.Fatalf("rendered terminal output = %q, want reply content and pending delivery status", rendered)
	}

	delivered, err := rpc.RecordOutboundDeliveryStatus(ctx, delivery.GetDeliveryId(), daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_DELIVERED, "", 0, 0)
	if err != nil {
		t.Fatalf("RecordOutboundDeliveryStatus() error = %v", err)
	}
	rendered = imterminal.RenderOutbound(replyMessage, delivered)
	if !strings.Contains(rendered, "[delivered delivery "+delivery.GetDeliveryId()+" -> term-live-1]") {
		t.Fatalf("rendered delivered terminal output = %q, want delivered status", rendered)
	}
}

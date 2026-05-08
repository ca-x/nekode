package imterminal

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ca-x/nekode/internal/storage"
)

func TestChannelNormalizesTerminalInput(t *testing.T) {
	channel := Channel{
		Config: Config{
			EndpointID:   "iep-terminal",
			SessionID:    "local-1",
			OperatorID:   "op-1",
			OperatorName: "Operator",
			Target:       "#ops",
			ThreadID:     "thread-1",
		},
		Now: func() time.Time { return time.Unix(100, 0) },
	}
	msg, err := channel.NormalizeInbound(Input{Text: " /new investigate "})
	if err != nil {
		t.Fatalf("NormalizeInbound() error = %v", err)
	}
	if msg.Provider != "terminal" || msg.EndpointID != "iep-terminal" || !strings.HasPrefix(msg.ExternalMessageID, "term-") {
		t.Fatalf("message source = %+v", msg)
	}
	if msg.Conversation.ExternalID != "local-1" || msg.Conversation.TargetHint != "#ops" || msg.Conversation.ThreadID != "thread-1" {
		t.Fatalf("conversation = %+v", msg.Conversation)
	}
	if msg.Sender.ExternalID != "op-1" || msg.Sender.DisplayName != "Operator" || msg.Sender.Kind != "human" {
		t.Fatalf("sender = %+v", msg.Sender)
	}
	if got := msg.Text(); got != "/new investigate" {
		t.Fatalf("Text() = %q", got)
	}
	draft, err := msg.Draft()
	if err != nil {
		t.Fatalf("Draft() error = %v", err)
	}
	if draft.Target != "#ops" || draft.ThreadID != "thread-1" || draft.SourceEndpointID != "iep-terminal" {
		t.Fatalf("draft = %+v", draft)
	}
}

func TestChannelRawEventRequiresEndpointAndText(t *testing.T) {
	if _, err := (Channel{}).RawEvent(Input{Text: "hello"}); !errors.Is(err, ErrInvalidTerminalInput) {
		t.Fatalf("RawEvent() error = %v, want ErrInvalidTerminalInput", err)
	}
	if _, err := (Channel{Config: Config{EndpointID: "iep"}}).RawEvent(Input{}); !errors.Is(err, ErrInvalidTerminalInput) {
		t.Fatalf("RawEvent(empty text) error = %v, want ErrInvalidTerminalInput", err)
	}
}

func TestRenderOutboundIncludesDeliveryStatus(t *testing.T) {
	got := RenderOutbound(storage.Message{
		Content:           "done",
		SenderAgentID:     "agent-1",
		SenderDisplayName: "Agent One",
		SenderKind:        "agent",
	}, storage.OutboundDelivery{
		ID:                "od-1",
		Status:            "delivered",
		ExternalMessageID: "term-out-1",
	})
	want := "Agent One: done\n[delivered delivery od-1 -> term-out-1]"
	if got != want {
		t.Fatalf("RenderOutbound() = %q, want %q", got, want)
	}
}

func TestChannelRawEventPassesNormalizer(t *testing.T) {
	event, err := (Channel{
		Config: Config{EndpointID: "iep-terminal"},
		Now:    func() time.Time { return time.Unix(101, 0) },
	}).RawEvent(Input{SessionID: "s1", OperatorID: "op", Text: "hello"})
	if err != nil {
		t.Fatalf("RawEvent() error = %v", err)
	}
	if event.Provider != "terminal" || event.EndpointKind != "im" || event.ReceivedUnix != 101 {
		t.Fatalf("raw event = %+v", event)
	}
	msg, err := (Channel{}).NormalizeInbound(Input{})
	if err == nil || !errors.Is(err, ErrInvalidTerminalInput) {
		t.Fatalf("NormalizeInbound(empty) = %+v, %v; want invalid", msg, err)
	}
}

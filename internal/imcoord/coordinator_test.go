package imcoord

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
)

type fakeStore struct {
	mu       sync.Mutex
	messages []storage.Message
	block    chan struct{}
	started  chan struct{}
}

func (s *fakeStore) CreateMessage(ctx context.Context, msg storage.Message) (storage.Message, error) {
	if s.started != nil {
		select {
		case <-s.started:
		default:
			close(s.started)
		}
	}
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return storage.Message{}, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	msg.ID = storage.NewID("msg")
	s.messages = append(s.messages, msg)
	return msg, nil
}

func TestCoordinatorCreatesExistingStorageMessage(t *testing.T) {
	store := &fakeStore{}
	coord := New(store, func(_ context.Context, target string, ids []string) ([]storage.Attachment, error) {
		if target != "#ops" || len(ids) != 1 || ids[0] != "att-1" {
			t.Fatalf("attachment loader target=%q ids=%v", target, ids)
		}
		return []storage.Attachment{{ID: "att-1", Filename: "shot.png"}}, nil
	})

	result, err := coord.Handle(context.Background(), Draft{
		Target:            "#ops",
		Content:           "hello",
		SourceEndpointID:  "iep-feishu",
		ExternalMessageID: "m-1",
		MetadataJSON:      `{"im":{"provider":"feishu"}}`,
		AttachmentIDs:     []string{"att-1"},
		Sender:            iminbound.Sender{ExternalID: "ou-1", DisplayName: "Alice"},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Message.ID == "" {
		t.Fatalf("message id is empty")
	}
	msg := result.Message
	if msg.Target != "#ops" || msg.SenderKind != "endpoint" || msg.SourceEndpointID != "iep-feishu" || msg.ExternalMessageID != "m-1" {
		t.Fatalf("message = %+v, want endpoint-backed Nekode message", msg)
	}
	if msg.RequestID != "iep-feishu:m-1" {
		t.Fatalf("request id = %q", msg.RequestID)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].ID != "att-1" {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
}

func TestCoordinatorCreatesMessageFromInboundDraft(t *testing.T) {
	store := &fakeStore{}
	coord := New(store, nil)
	providers := []string{"telegram", "qq", "feishu", "wechat", "terminal", "serverchan"}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			inbound := iminbound.Message{
				EndpointID:        "iep-" + provider,
				EndpointKind:      "im",
				Provider:          provider,
				ExternalMessageID: "external-" + provider,
				Conversation: iminbound.Conversation{
					ExternalID: "group-1",
					IsGroup:    true,
					TargetHint: "#ops",
					ThreadID:   "thread-1",
				},
				Sender:  iminbound.Sender{ExternalID: "user-1", DisplayName: "Alice"},
				Content: []iminbound.ContentBlock{{Type: iminbound.ContentTypeText, Text: "hello from " + provider}},
			}
			draft, err := inbound.Draft()
			if err != nil {
				t.Fatalf("Draft() error = %v", err)
			}
			result, err := coord.Handle(context.Background(), draft)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if result.SessionKey != "#ops:thread-1" {
				t.Fatalf("session key = %q", result.SessionKey)
			}
			if result.Message.Target != "#ops" || result.Message.ThreadID != "thread-1" ||
				result.Message.SourceEndpointID != "iep-"+provider ||
				result.Message.ExternalMessageID != "external-"+provider {
				t.Fatalf("message = %+v", result.Message)
			}
		})
	}
}

func TestCoordinatorSerializesSameSession(t *testing.T) {
	store := &fakeStore{block: make(chan struct{}), started: make(chan struct{})}
	coord := New(store, nil)
	ctx := context.Background()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = coord.Handle(ctx, draftWithID("m-1"))
	}()
	<-store.started

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		_, _ = coord.Handle(ctx, draftWithID("m-2"))
	}()

	store.mu.Lock()
	if got := len(store.messages); got != 0 {
		t.Fatalf("messages before unblock = %d, want 0", got)
	}
	store.mu.Unlock()

	close(store.block)
	<-firstDone
	<-secondDone

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.messages) != 2 || store.messages[0].ExternalMessageID != "m-1" || store.messages[1].ExternalMessageID != "m-2" {
		t.Fatalf("messages = %+v, want FIFO by session", store.messages)
	}
}

func TestCoordinatorAbortActiveSession(t *testing.T) {
	store := &fakeStore{block: make(chan struct{}), started: make(chan struct{})}
	coord := New(store, nil)
	firstDone := make(chan error, 1)
	go func() {
		_, err := coord.Handle(context.Background(), draftWithID("m-1"))
		firstDone <- err
	}()
	<-store.started

	result, err := coord.Handle(context.Background(), Draft{
		Target:            "#ops",
		Content:           "/abort",
		SourceEndpointID:  "iep-feishu",
		ExternalMessageID: "m-abort",
		Sender:            iminbound.Sender{ExternalID: "ou-1"},
	})
	if err != nil {
		t.Fatalf("abort Handle() error = %v", err)
	}
	if !result.HandledCommand || result.Response != "Aborted." {
		t.Fatalf("abort result = %+v", result)
	}
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first err = %v, want context.Canceled", err)
	}
}

func TestCoordinatorHandlesKnownCommandsWithoutCreatingMessages(t *testing.T) {
	store := &fakeStore{}
	coord := New(store, nil)
	tests := []struct {
		content string
		command string
		args    string
	}{
		{content: "/new", command: "/new"},
		{content: "/agent @helper", command: "/agent", args: "@helper"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			draft := draftWithID("cmd-" + tt.command[1:])
			draft.Content = tt.content
			result, err := coord.Handle(context.Background(), draft)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if !result.HandledCommand || result.Command != tt.command || result.CommandArgs != tt.args || result.Response == "" {
				t.Fatalf("command result = %+v", result)
			}
		})
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.messages) != 0 {
		t.Fatalf("messages = %+v, want none for commands", store.messages)
	}
}

func TestCoordinatorRejectsInvalidMetadataJSON(t *testing.T) {
	coord := New(&fakeStore{}, nil)
	draft := draftWithID("invalid-json")
	draft.MetadataJSON = `{"im":`
	if _, err := coord.Handle(context.Background(), draft); !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("Handle() error = %v, want ErrInvalidDraft", err)
	}
}

func draftWithID(id string) Draft {
	return Draft{
		Target:            "#ops",
		Content:           "hello " + id,
		SourceEndpointID:  "iep-feishu",
		ExternalMessageID: id,
		Sender:            iminbound.Sender{ExternalID: "ou-1", DisplayName: "Alice"},
	}
}

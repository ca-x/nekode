package imcoord

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
)

type fakeStore struct {
	mu            sync.Mutex
	messages      []storage.Message
	endpoints     map[string]storage.InteractionEndpoint
	authRequests  []storage.IMChatAuthRequest
	subscriptions map[string]storage.IMChatSubscription
	block         chan struct{}
	started       chan struct{}
	err           error
}

func (s *fakeStore) CreateMessage(ctx context.Context, msg storage.Message) (storage.Message, error) {
	if s.err != nil {
		return storage.Message{}, s.err
	}
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

func (s *fakeStore) GetInteractionEndpoint(_ context.Context, id string) (storage.InteractionEndpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	endpoint, ok := s.endpoints[id]
	if !ok {
		return storage.InteractionEndpoint{}, storage.ErrNotFound
	}
	return endpoint, nil
}

func (s *fakeStore) CreateIMChatAuthRequest(_ context.Context, req storage.IMChatAuthRequest) (storage.IMChatAuthRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req.ID = storage.NewID("imreq")
	req.TokenHash = ""
	s.authRequests = append(s.authRequests, req)
	return req, nil
}

func (s *fakeStore) GetIMChatSubscription(_ context.Context, endpointID, conversationID, externalThreadID string) (storage.IMChatSubscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[endpointID+":"+conversationID+":"+externalThreadID]
	if !ok {
		return storage.IMChatSubscription{}, storage.ErrNotFound
	}
	return sub, nil
}

func (s *fakeStore) UpdateIMChatSubscription(_ context.Context, id string, patch storage.IMChatSubscriptionPatch) (storage.IMChatSubscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, sub := range s.subscriptions {
		if sub.ID != id {
			continue
		}
		if patch.Subscribed != nil {
			sub.Subscribed = *patch.Subscribed
		}
		if patch.Verbose != nil {
			sub.Verbose = *patch.Verbose
		}
		s.subscriptions[key] = sub
		return sub, nil
	}
	return storage.IMChatSubscription{}, storage.ErrNotFound
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

func TestCoordinatorDedupesBeforeStorage(t *testing.T) {
	store := &fakeStore{}
	coord := New(store, nil)
	if _, err := coord.Handle(context.Background(), draftWithID("m-dedupe")); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if _, err := coord.Handle(context.Background(), draftWithID("m-dedupe")); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("duplicate Handle() error = %v, want storage.ErrConflict", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.messages) != 1 {
		t.Fatalf("messages = %+v, want one stored message", store.messages)
	}
}

func TestCoordinatorRejectsStaleDraftWhenStartTimeConfigured(t *testing.T) {
	coord := New(&fakeStore{}, nil, WithStartTime(time.Unix(100, 0)))
	draft := draftWithID("m-stale")
	draft.ReceivedUnix = 90
	if _, err := coord.Handle(context.Background(), draft); !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("stale Handle() error = %v, want ErrStaleDraft", err)
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

func TestCoordinatorRequiresIMChatSubscriptionWhenEndpointRequiresIt(t *testing.T) {
	store := &fakeStore{
		endpoints: map[string]storage.InteractionEndpoint{
			"iep-wecom": {
				ID:         "iep-wecom",
				Kind:       "im",
				Provider:   "wecom",
				ConfigJSON: `{"require_subscription":true,"default_target":"#ops","default_thread_id":"im-wecom-ops"}`,
			},
		},
		subscriptions: map[string]storage.IMChatSubscription{},
	}
	coord := New(store, nil, WithAuthTokenGenerator(func() string { return "nk_test_token" }))

	draft := Draft{
		Target:            "#ops",
		ThreadID:          "im-wecom-ops",
		Content:           "hello without auth",
		SourceEndpointID:  "iep-wecom",
		ExternalMessageID: "m-unauthorized",
		MetadataJSON:      `{"im":{"conversation":{"external_id":"chat-1","external_thread_id":"topic-1","display_name":"Ops Room"}}}`,
		Sender:            iminbound.Sender{ExternalID: "user-1", DisplayName: "Alice"},
	}
	result, err := coord.Handle(context.Background(), draft)
	if !errors.Is(err, ErrUnauthorizedChat) {
		t.Fatalf("unauthorized Handle() error = %v, want ErrUnauthorizedChat", err)
	}
	if !result.HandledCommand || result.Response == "" {
		t.Fatalf("unauthorized result = %+v, want command-style response", result)
	}
	store.mu.Lock()
	if len(store.messages) != 0 {
		t.Fatalf("messages = %+v, want unauthorized chat not persisted", store.messages)
	}
	store.mu.Unlock()

	subscribe := draft
	subscribe.Content = "/subscribe"
	subscribe.ExternalMessageID = "m-subscribe"
	result, err = coord.Handle(context.Background(), subscribe)
	if err != nil {
		t.Fatalf("subscribe Handle() error = %v", err)
	}
	if !result.HandledCommand || result.Command != "/subscribe" || !strings.Contains(result.Response, "nk_test_token") {
		t.Fatalf("subscribe result = %+v, want bind token response", result)
	}
	store.mu.Lock()
	if len(store.authRequests) != 1 ||
		store.authRequests[0].Provider != "wecom" ||
		store.authRequests[0].ConversationID != "chat-1" ||
		store.authRequests[0].ExternalThreadID != "topic-1" {
		t.Fatalf("authRequests = %+v, want one pending request for chat/topic", store.authRequests)
	}
	store.subscriptions["iep-wecom:chat-1:topic-1"] = storage.IMChatSubscription{
		ID:               "imsub-1",
		EndpointID:       "iep-wecom",
		Provider:         "wecom",
		ConversationID:   "chat-1",
		ExternalThreadID: "topic-1",
		Target:           "#ops",
		ThreadID:         "im-wecom-ops",
		Subscribed:       true,
		Verbose:          false,
	}
	store.mu.Unlock()

	authorized := draft
	authorized.ExternalMessageID = "m-authorized"
	authorized.Content = "hello after auth"
	result, err = coord.Handle(context.Background(), authorized)
	if err != nil {
		t.Fatalf("authorized Handle() error = %v", err)
	}
	if result.Message.ID == "" || result.Message.Content != "hello after auth" {
		t.Fatalf("authorized result = %+v, want stored inbound message", result)
	}

	verbose := draft
	verbose.ExternalMessageID = "m-verbose"
	verbose.Content = "/verbose"
	result, err = coord.Handle(context.Background(), verbose)
	if err != nil {
		t.Fatalf("verbose Handle() error = %v", err)
	}
	if !result.HandledCommand || result.Command != "/verbose" || !strings.Contains(strings.ToLower(result.Response), "on") {
		t.Fatalf("verbose result = %+v, want verbose toggle response", result)
	}
	store.mu.Lock()
	if !store.subscriptions["iep-wecom:chat-1:topic-1"].Verbose {
		t.Fatalf("subscription = %+v, want verbose enabled", store.subscriptions["iep-wecom:chat-1:topic-1"])
	}
	store.mu.Unlock()
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

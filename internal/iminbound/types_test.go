package iminbound

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMessageNormalizeValidateAndDraft(t *testing.T) {
	msg := Message{
		EndpointID:        " endpoint-feishu ",
		EndpointKind:      " im ",
		Provider:          " feishu ",
		ExternalMessageID: " msg-1 ",
		Conversation: Conversation{
			ExternalID:    " oc-1 ",
			IsGroup:       true,
			TargetHint:    " #ops ",
			ThreadID:      " thread-1 ",
			RootMessageID: " root-1 ",
		},
		Sender: Sender{
			ExternalID:   " ou-1 ",
			CandidateIDs: []string{"ou-1", "on-1", " "},
			DisplayName:  " Alice ",
		},
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: " hello "},
			{Type: ContentTypeImage, AttachmentID: " att-1 ", Filename: "shot.png"},
		},
		AttachmentIDs: []string{"att-1", "att-1", " "},
		Metadata: map[string]any{
			"tenant": "t-1",
		},
	}

	normalized := msg.Normalize()
	if err := normalized.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if normalized.EndpointID != "endpoint-feishu" || normalized.Provider != "feishu" {
		t.Fatalf("normalized endpoint = %+v", normalized)
	}
	if got := normalized.Sender.CandidateIDs; len(got) != 2 || got[0] != "ou-1" || got[1] != "on-1" {
		t.Fatalf("candidate ids = %#v, want deduped preferred order", got)
	}
	if got := normalized.AttachmentIDs; len(got) != 1 || got[0] != "att-1" {
		t.Fatalf("attachment ids = %#v, want deduped att-1", got)
	}
	if got := normalized.DedupeKey(); got != "endpoint-feishu:msg-1" {
		t.Fatalf("DedupeKey() = %q", got)
	}
	if got := normalized.Text(); !strings.Contains(got, "hello") || !strings.Contains(got, "[image: shot.png]") {
		t.Fatalf("Text() = %q, want text and media placeholder", got)
	}

	draft, err := normalized.Draft()
	if err != nil {
		t.Fatalf("Draft() error = %v", err)
	}
	if draft.Target != "#ops" || draft.ThreadID != "thread-1" || draft.Role != "user" {
		t.Fatalf("draft routing = %+v", draft)
	}
	if draft.SourceEndpointID != "endpoint-feishu" || draft.ExternalMessageID != "msg-1" {
		t.Fatalf("draft endpoint refs = %+v", draft)
	}
	if len(draft.AttachmentIDs) != 1 || draft.AttachmentIDs[0] != "att-1" {
		t.Fatalf("draft attachments = %#v", draft.AttachmentIDs)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(draft.MetadataJSON), &metadata); err != nil {
		t.Fatalf("metadata JSON invalid: %v", err)
	}
	if metadata["tenant"] != "t-1" {
		t.Fatalf("metadata tenant = %#v", metadata["tenant"])
	}
	if _, ok := metadata["im"].(map[string]any); !ok {
		t.Fatalf("metadata missing im envelope: %#v", metadata)
	}
}

func TestMessageTextFallsBackToAttachmentPlaceholder(t *testing.T) {
	msg := Message{
		EndpointID:        "ep-1",
		ExternalMessageID: "m-1",
		Conversation:      Conversation{ExternalID: "chat-1"},
		Sender:            Sender{ExternalID: "user-1"},
		AttachmentIDs:     []string{"att-1", "att-2"},
	}
	if got := msg.Text(); got != "[2 attachments]" {
		t.Fatalf("Text() = %q, want attachment placeholder", got)
	}
	draft, err := msg.Draft()
	if err != nil {
		t.Fatalf("Draft() error = %v", err)
	}
	if draft.Content != "[2 attachments]" {
		t.Fatalf("draft.Content = %q, want attachment placeholder", draft.Content)
	}
}

func TestMessageValidateRejectsMissingRequiredFields(t *testing.T) {
	err := Message{}.Validate()
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("Validate() error = %v, want ErrInvalidMessage", err)
	}
	for _, want := range []string{
		"endpoint_id is required",
		"external_message_id is required",
		"conversation.external_id is required",
		"sender.external_id is required",
		"content or attachment_ids is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Validate() error = %q, want %q", err, want)
		}
	}
}

func TestContentBlockValidation(t *testing.T) {
	tests := []struct {
		name    string
		block   ContentBlock
		wantErr bool
	}{
		{name: "text", block: ContentBlock{Type: ContentTypeText, Text: "hello"}},
		{name: "text empty", block: ContentBlock{Type: ContentTypeText}, wantErr: true},
		{name: "image attachment", block: ContentBlock{Type: ContentTypeImage, AttachmentID: "att"}},
		{name: "image external url", block: ContentBlock{Type: ContentTypeImage, ExternalURL: "https://example.test/a.png"}},
		{name: "image empty", block: ContentBlock{Type: ContentTypeImage}, wantErr: true},
		{name: "unknown with metadata", block: ContentBlock{Type: ContentTypeUnknown, Metadata: map[string]any{"raw": true}}},
		{name: "unsupported", block: ContentBlock{Type: ContentType("poll"), Text: "vote"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.block.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() nil error, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestAdapterFunc(t *testing.T) {
	adapter := AdapterFunc(func(_ context.Context, event RawEvent) (Message, error) {
		return Message{
			EndpointID:        event.EndpointID,
			EndpointKind:      event.EndpointKind,
			Provider:          event.Provider,
			ExternalMessageID: event.ExternalMessageID,
			Conversation:      Conversation{ExternalID: "chat-1"},
			Sender:            Sender{ExternalID: "user-1"},
			Content:           []ContentBlock{{Type: ContentTypeText, Text: string(event.Body)}},
		}, nil
	})

	msg, err := adapter.NormalizeInbound(context.Background(), RawEvent{
		EndpointID:        "ep-1",
		EndpointKind:      "im",
		Provider:          "telegram",
		ExternalMessageID: "tg-1",
		Body:              []byte("hello"),
	})
	if err != nil {
		t.Fatalf("NormalizeInbound() error = %v", err)
	}
	if msg.Provider != "telegram" || msg.Text() != "hello" {
		t.Fatalf("NormalizeInbound() = %+v", msg)
	}
}

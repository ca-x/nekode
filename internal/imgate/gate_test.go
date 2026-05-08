package imgate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/immedia"
	"github.com/ca-x/nekode/internal/storage"
)

func TestProviderFixturesCoverFiveStellaProviders(t *testing.T) {
	want := map[string]bool{
		imadapter.ProviderTelegram: false,
		imadapter.ProviderQQ:       false,
		imadapter.ProviderFeishu:   false,
		imadapter.ProviderWeixin:   false,
		imadapter.ProviderTerminal: false,
	}
	for _, fixture := range ProviderFixtures() {
		if _, ok := want[fixture.Provider]; !ok {
			t.Fatalf("unexpected provider fixture %q", fixture.Provider)
		}
		if _, ok := imadapter.GetProvider(fixture.Provider); !ok {
			t.Fatalf("fixture provider %q is not registered", fixture.Provider)
		}
		if want[fixture.Provider] {
			t.Fatalf("duplicate provider fixture %q", fixture.Provider)
		}
		want[fixture.Provider] = true
		msg := fixture.Message()
		if msg.DedupeKey() != fixture.EndpointID+":"+fixture.ExternalMessageID {
			t.Fatalf("%s dedupe key = %q", fixture.Provider, msg.DedupeKey())
		}
	}
	for provider, seen := range want {
		if !seen {
			t.Fatalf("missing provider fixture %q", provider)
		}
	}
}

func TestInboundCoordinatorStorageGate(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store := newTestStore(t)
	coord := imcoord.New(store, func(_ context.Context, target string, ids []string) ([]storage.Attachment, error) {
		attachments := make([]storage.Attachment, 0, len(ids))
		for _, id := range ids {
			attachment, err := immedia.ReadMetadata(dataDir, id)
			if err != nil {
				return nil, err
			}
			if attachment.Target != "" && attachment.Target != target {
				t.Fatalf("attachment target = %q, want %q", attachment.Target, target)
			}
			attachments = append(attachments, attachment)
		}
		return attachments, nil
	})

	for _, fixture := range ProviderFixtures() {
		t.Run(fixture.Provider, func(t *testing.T) {
			endpoint, err := store.CreateInteractionEndpoint(ctx, storage.InteractionEndpoint{
				ID:              fixture.EndpointID,
				Kind:            "im",
				Provider:        fixture.Provider,
				DisplayName:     fixture.EndpointName,
				TargetPrefix:    "#",
				InboundEnabled:  true,
				OutboundEnabled: true,
				AuthMode:        "signature",
				ConfigJSON:      `{"group_mode":"mention"}`,
			})
			if err != nil {
				t.Fatalf("CreateInteractionEndpoint() error = %v", err)
			}

			attachmentIDs := storeFixtureMedia(t, dataDir, fixture)
			inbound := fixture.Message(attachmentIDs...)
			draft, err := inbound.Draft()
			if err != nil {
				t.Fatalf("Draft() error = %v", err)
			}
			result, err := coord.Handle(ctx, draft)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			message := result.Message
			if message.Target != fixture.Target || message.ThreadID != fixture.ThreadID {
				t.Fatalf("message target/thread = %q/%q, want %q/%q", message.Target, message.ThreadID, fixture.Target, fixture.ThreadID)
			}
			if message.SourceEndpointID != endpoint.ID || message.ExternalMessageID != fixture.ExternalMessageID {
				t.Fatalf("message source = %q/%q, want %q/%q", message.SourceEndpointID, message.ExternalMessageID, endpoint.ID, fixture.ExternalMessageID)
			}
			if message.SenderKind != "endpoint" || message.SenderDisplayName != fixture.SenderName {
				t.Fatalf("message sender = %q/%q", message.SenderKind, message.SenderDisplayName)
			}
			if strings.HasPrefix(message.Target, "dm:agent/") {
				t.Fatalf("IM-origin message target %q must not enter Web-native agent DM", message.Target)
			}
			if result.SessionKey != imcoord.SessionKey(draft) {
				t.Fatalf("session key = %q, want %q", result.SessionKey, imcoord.SessionKey(draft))
			}

			assertWebBadgeMetadata(t, message.MetadataJSON, fixture)
			if fixture.AttachmentFilename != "" {
				if len(message.Attachments) != 1 || message.Attachments[0].Filename != fixture.AttachmentFilename {
					t.Fatalf("message attachments = %+v", message.Attachments)
				}
			}

			messages, err := store.ListMessages(ctx, message.Target, message.ThreadID, 20)
			if err != nil {
				t.Fatalf("ListMessages() error = %v", err)
			}
			if !containsMessage(messages, message.ID) {
				t.Fatalf("stored messages = %+v, missing %q", messages, message.ID)
			}
		})
	}
}

func storeFixtureMedia(t *testing.T, dataDir string, fixture Fixture) []string {
	t.Helper()
	if fixture.AttachmentFilename == "" {
		return nil
	}
	stored, err := immedia.Store(dataDir, immedia.StoreInput{
		Target:   fixture.Target,
		OwnerID:  fixture.SenderID,
		Filename: fixture.AttachmentFilename,
		MimeType: fixture.AttachmentMimeType,
		Content:  strings.NewReader(fixture.AttachmentContent),
	})
	if err != nil {
		t.Fatalf("store fixture media: %v", err)
	}
	return []string{stored.Attachment.ID}
}

func assertWebBadgeMetadata(t *testing.T, metadataJSON string, fixture Fixture) {
	t.Helper()
	var metadata struct {
		IM struct {
			EndpointID        string `json:"endpoint_id"`
			EndpointKind      string `json:"endpoint_kind"`
			Provider          string `json:"provider"`
			ExternalMessageID string `json:"external_message_id"`
			Conversation      struct {
				ExternalID  string `json:"external_id"`
				DisplayName string `json:"display_name"`
				IsGroup     bool   `json:"is_group"`
				TargetHint  string `json:"target_hint"`
				ThreadID    string `json:"thread_id"`
			} `json:"conversation"`
			Sender struct {
				ExternalID  string `json:"external_id"`
				DisplayName string `json:"display_name"`
				Username    string `json:"username"`
				Kind        string `json:"kind"`
			} `json:"sender"`
		} `json:"im"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		t.Fatalf("metadata JSON decode failed: %v", err)
	}
	if metadata.IM.Provider != fixture.Provider ||
		metadata.IM.EndpointID != fixture.EndpointID ||
		metadata.IM.EndpointKind != "im" ||
		metadata.IM.ExternalMessageID != fixture.ExternalMessageID {
		t.Fatalf("metadata.im source = %+v, want fixture source", metadata.IM)
	}
	if metadata.IM.Conversation.ExternalID != fixture.ConversationID ||
		metadata.IM.Conversation.DisplayName != fixture.ConversationName ||
		metadata.IM.Conversation.IsGroup != fixture.IsGroup ||
		metadata.IM.Conversation.TargetHint != fixture.Target ||
		metadata.IM.Conversation.ThreadID != fixture.ThreadID {
		t.Fatalf("metadata.im.conversation = %+v, want fixture conversation", metadata.IM.Conversation)
	}
	if metadata.IM.Sender.ExternalID != fixture.SenderID ||
		metadata.IM.Sender.DisplayName != fixture.SenderName ||
		metadata.IM.Sender.Username != fixture.SenderUsername ||
		metadata.IM.Sender.Kind != fixture.SenderKind {
		t.Fatalf("metadata.im.sender = %+v, want fixture sender", metadata.IM.Sender)
	}
}

func containsMessage(messages []storage.Message, id string) bool {
	for _, message := range messages {
		if message.ID == id {
			return true
		}
	}
	return false
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("imgate_test")+"?mode=memory&cache=shared&_fk=1")
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

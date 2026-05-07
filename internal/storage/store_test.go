package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStoreMigrateAndCoreModels(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	user, err := store.CreateUser(ctx, User{
		Username:     "czyt",
		DisplayName:  "czyt",
		PasswordHash: "hash",
		Role:         "admin",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := store.CreateUser(ctx, User{Username: "czyt", DisplayName: "dup", PasswordHash: "hash"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate CreateUser() error = %v, want %v", err, ErrConflict)
	}

	session, err := store.CreateSession(ctx, Session{
		TokenHash:   "token-hash",
		UserID:      user.ID,
		ExpiresUnix: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	loadedSession, err := store.GetSessionByTokenHash(ctx, "token-hash")
	if err != nil {
		t.Fatalf("GetSessionByTokenHash() error = %v", err)
	}
	if loadedSession.ID != session.ID {
		t.Fatalf("session id = %q, want %q", loadedSession.ID, session.ID)
	}

	endpoint, err := store.CreateInteractionEndpoint(ctx, InteractionEndpoint{
		Kind:            "web",
		Provider:        "browser",
		DisplayName:     "Web Console",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "cookie",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	endpoints, err := store.ListInteractionEndpoints(ctx, 10)
	if err != nil {
		t.Fatalf("ListInteractionEndpoints() error = %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].ID != endpoint.ID {
		t.Fatalf("endpoints = %+v, want created endpoint", endpoints)
	}

	message, err := store.CreateMessage(ctx, Message{
		Target:           "#general",
		Role:             "user",
		Content:          "hello",
		SenderUserID:     user.ID,
		SenderKind:       "human",
		SourceEndpointID: endpoint.ID,
		MetadataJSON:     "{}",
	})
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	messages, err := store.ListMessages(ctx, "#general", 10)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0].ID != message.ID {
		t.Fatalf("messages = %+v, want created message", messages)
	}

	task, err := store.CreateTask(ctx, Task{
		Summary:         "ship backend",
		Target:          "#general",
		CreatedByUserID: user.ID,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	nextState := "in_progress"
	updated, err := store.UpdateTask(ctx, task.ID, TaskPatch{State: &nextState})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if updated.State != "in_progress" {
		t.Fatalf("updated.State = %q, want in_progress", updated.State)
	}
}

func TestStoreRejectsInvalidTaskState(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	_, err := store.CreateTask(ctx, Task{Summary: "bad", State: "reviewing", Target: "#general"})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("CreateTask() error = %v, want %v", err, ErrInvalidState)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), "file:"+NewID("test")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

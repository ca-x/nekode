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
	messages, err := store.ListMessages(ctx, "#general", "", 10)
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
	blockedState := "blocked"
	updated, err = store.UpdateTask(ctx, task.ID, TaskPatch{State: &blockedState})
	if err != nil {
		t.Fatalf("UpdateTask(blocked) error = %v", err)
	}
	if updated.State != "blocked" {
		t.Fatalf("updated.State = %q, want blocked", updated.State)
	}
	blockedTasks, err := store.ListTasks(ctx, "blocked", "#general", 10)
	if err != nil {
		t.Fatalf("ListTasks(blocked) error = %v", err)
	}
	if len(blockedTasks) != 1 || blockedTasks[0].ID != task.ID {
		t.Fatalf("blocked tasks = %+v, want task %s", blockedTasks, task.ID)
	}
	cancelledState := "cancelled"
	updated, err = store.UpdateTask(ctx, task.ID, TaskPatch{State: &cancelledState})
	if err != nil {
		t.Fatalf("UpdateTask(cancelled alias) error = %v", err)
	}
	if updated.State != "canceled" {
		t.Fatalf("updated.State = %q, want canceled", updated.State)
	}
	canceledTasks, err := store.ListTasks(ctx, "canceled", "#general", 10)
	if err != nil {
		t.Fatalf("ListTasks(canceled) error = %v", err)
	}
	if len(canceledTasks) != 1 || canceledTasks[0].ID != task.ID {
		t.Fatalf("canceled tasks = %+v, want task %s", canceledTasks, task.ID)
	}
	claimed, accepted, err := store.ClaimTaskCAS(ctx, task.ID, "agent-1", "lease-1")
	if err != nil {
		t.Fatalf("ClaimTaskCAS() error = %v", err)
	}
	if !accepted || claimed.AssigneeID != "agent-1" || claimed.ClaimLeaseID != "lease-1" {
		t.Fatalf("ClaimTaskCAS() = %+v accepted=%v, want agent-1 lease", claimed, accepted)
	}
	conflict, accepted, err := store.ClaimTaskCAS(ctx, task.ID, "agent-2", "lease-2")
	if err != nil {
		t.Fatalf("ClaimTaskCAS(conflict) error = %v", err)
	}
	if accepted || conflict.AssigneeID != "agent-1" {
		t.Fatalf("ClaimTaskCAS(conflict) = %+v accepted=%v, want existing agent-1", conflict, accepted)
	}

	event1, err := store.AppendCollaborationEvent(ctx, CollaborationEvent{
		ServerID:        "srv-test",
		Target:          "#general",
		AggregateID:     "#general",
		Kind:            "activity",
		Operation:       "created",
		ScopeType:       "target",
		ScopeID:         "#general",
		ActivityID:      "act-1",
		PayloadJSON:     "{}",
		ProtocolVersion: 1,
	})
	if err != nil {
		t.Fatalf("AppendCollaborationEvent() error = %v", err)
	}
	event2, err := store.AppendCollaborationEvent(ctx, CollaborationEvent{
		ServerID:        "srv-test",
		Target:          "#general",
		AggregateID:     "#general",
		Kind:            "activity",
		Operation:       "created",
		ScopeType:       "target",
		ScopeID:         "#general",
		ActivityID:      "act-2",
		PayloadJSON:     "{}",
		ProtocolVersion: 1,
	})
	if err != nil {
		t.Fatalf("AppendCollaborationEvent(second) error = %v", err)
	}
	if event1.Sequence != 1 || event2.Sequence != 2 {
		t.Fatalf("event sequences = %d,%d want 1,2", event1.Sequence, event2.Sequence)
	}
	if event1.Operation != "created" || event1.ScopeType != "target" || event1.ScopeID != "#general" {
		t.Fatalf("event envelope = %+v, want operation/scope persisted", event1)
	}
	events, err := store.ListCollaborationEvents(ctx, "srv-test", "#general", "", 1, 10)
	if err != nil {
		t.Fatalf("ListCollaborationEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].ActivityID != "act-2" {
		t.Fatalf("events = %+v, want second event", events)
	}
	recent, err := store.ListRecentCollaborationEvents(ctx, "srv-test", "#general", "activity", 10)
	if err != nil {
		t.Fatalf("ListRecentCollaborationEvents() error = %v", err)
	}
	if len(recent) != 2 || recent[0].ActivityID != "act-2" || recent[1].ActivityID != "act-1" {
		t.Fatalf("recent events = %+v, want newest first", recent)
	}

	reserved, created, err := store.ReserveIdempotencyRecord(ctx, IdempotencyRecord{
		Scope:          "daemonrpc",
		Method:         "Test",
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("ReserveIdempotencyRecord() error = %v", err)
	}
	if !created || reserved.Status != "pending" {
		t.Fatalf("ReserveIdempotencyRecord() = %+v created=%v, want pending create", reserved, created)
	}
	if err := store.CompleteIdempotencyRecord(ctx, IdempotencyRecord{
		Scope:          "daemonrpc",
		Method:         "Test",
		IdempotencyKey: "idem-1",
		ResponseType:   "test.Response",
		ResponseJSON:   `{"ok":true}`,
	}); err != nil {
		t.Fatalf("CompleteIdempotencyRecord() error = %v", err)
	}
	loaded, err := store.GetIdempotencyRecord(ctx, "daemonrpc", "Test", "", "idem-1")
	if err != nil {
		t.Fatalf("GetIdempotencyRecord() error = %v", err)
	}
	if loaded.Status != "completed" || loaded.ResponseJSON == "" {
		t.Fatalf("idempotency record = %+v, want completed response", loaded)
	}
}

func TestStoreRejectsInvalidTaskState(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	_, err := store.CreateTask(ctx, Task{Summary: "bad", State: "reviewing", Target: "#general"})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("CreateTask() error = %v, want %v", err, ErrInvalidState)
	}
	if _, err := store.ListTasks(ctx, "reviewing", "#general", 10); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("ListTasks() error = %v, want %v", err, ErrInvalidState)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), "file:"+NewID("test")+"?mode=memory&cache=shared&_fk=1")
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

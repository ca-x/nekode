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
	delivery, err := store.CreateOutboundDelivery(ctx, OutboundDelivery{
		Target:            message.Target,
		MessageID:         message.ID,
		EndpointID:        endpoint.ID,
		EndpointKind:      endpoint.Kind,
		ExternalMessageID: "im-msg-1",
		Status:            "failed",
		LastError:         "rate limited",
		RequestID:         "delivery-1",
	})
	if err != nil {
		t.Fatalf("CreateOutboundDelivery() error = %v", err)
	}
	failedDeliveries, err := store.ListOutboundDeliveries(ctx, OutboundDeliveryListOptions{
		Target:   "#general",
		Statuses: []string{"failed"},
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListOutboundDeliveries(failed) error = %v", err)
	}
	if len(failedDeliveries) != 1 || failedDeliveries[0].ID != delivery.ID {
		t.Fatalf("failed deliveries = %+v, want %s", failedDeliveries, delivery.ID)
	}
	retryingDelivery, err := store.ScheduleOutboundDeliveryRetry(ctx, delivery.ID, time.Now().Unix())
	if err != nil {
		t.Fatalf("ScheduleOutboundDeliveryRetry() error = %v", err)
	}
	if retryingDelivery.Status != "retrying" || retryingDelivery.AttemptCount != 1 || retryingDelivery.LastError != "" {
		t.Fatalf("retrying delivery = %+v, want retrying attempt_count=1 without last error", retryingDelivery)
	}
	deliveredDelivery, err := store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "delivered", "", 0, time.Now().Unix())
	if err != nil {
		t.Fatalf("UpdateOutboundDeliveryStatus() error = %v", err)
	}
	if deliveredDelivery.Status != "delivered" || deliveredDelivery.DeliveredTimeUnix == 0 {
		t.Fatalf("delivered delivery = %+v, want delivered timestamp", deliveredDelivery)
	}
	notificationRoute, err := store.CreateNotificationRoute(ctx, NotificationRoute{
		Target:     "#general",
		ThreadID:   "thread-1",
		EndpointID: endpoint.ID,
		EventKind:  "messages",
		Preference: "all",
		ConfigJSON: `{"reason":"ops"}`,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("CreateNotificationRoute() error = %v", err)
	}
	resolvedRoutes, err := store.ResolveNotificationRoutes(ctx, NotificationRouteResolveOptions{
		Target:    "#general",
		ThreadID:  "thread-1",
		EventKind: "message",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ResolveNotificationRoutes() error = %v", err)
	}
	if len(resolvedRoutes) != 1 || resolvedRoutes[0].ID != notificationRoute.ID {
		t.Fatalf("resolved routes = %+v, want %s", resolvedRoutes, notificationRoute.ID)
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

func TestNotificationRouteResolution(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	channelEndpoint, err := store.CreateInteractionEndpoint(ctx, InteractionEndpoint{
		Kind:            "im",
		Provider:        "feishu",
		DisplayName:     "Feishu",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "bearer",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint(feishu) error = %v", err)
	}
	threadEndpoint, err := store.CreateInteractionEndpoint(ctx, InteractionEndpoint{
		Kind:            "im",
		Provider:        "telegram",
		DisplayName:     "Telegram",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "bearer",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint(telegram) error = %v", err)
	}
	mutedEndpoint, err := store.CreateInteractionEndpoint(ctx, InteractionEndpoint{
		Kind:            "im",
		Provider:        "terminal",
		DisplayName:     "Terminal",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "bearer",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint(terminal) error = %v", err)
	}

	if _, err := store.CreateNotificationRoute(ctx, NotificationRoute{
		Target:     "#ops",
		EndpointID: channelEndpoint.ID,
		EventKind:  "all",
		Preference: "all",
		Enabled:    true,
		ConfigJSON: "{}",
	}); err != nil {
		t.Fatalf("CreateNotificationRoute(channel) error = %v", err)
	}
	threadRoute, err := store.CreateNotificationRoute(ctx, NotificationRoute{
		Target:     "#ops",
		ThreadID:   "thread-incident",
		EndpointID: threadEndpoint.ID,
		EventKind:  "message",
		Preference: "all",
		Enabled:    true,
		ConfigJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateNotificationRoute(thread) error = %v", err)
	}
	if _, err := store.CreateNotificationRoute(ctx, NotificationRoute{
		Target:     "#ops",
		EndpointID: mutedEndpoint.ID,
		EventKind:  "message",
		Preference: "muted",
		Enabled:    true,
		ConfigJSON: "{}",
	}); err != nil {
		t.Fatalf("CreateNotificationRoute(muted) error = %v", err)
	}

	resolved, err := store.ResolveNotificationRoutes(ctx, NotificationRouteResolveOptions{
		Target:    "#ops",
		ThreadID:  "thread-incident",
		EventKind: "message",
	})
	if err != nil {
		t.Fatalf("ResolveNotificationRoutes() error = %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved routes = %+v, want thread + channel routes", resolved)
	}
	if resolved[0].ID != threadRoute.ID {
		t.Fatalf("resolved[0] = %+v, want thread-specific route first", resolved[0])
	}
	for _, route := range resolved {
		if route.EndpointID == mutedEndpoint.ID {
			t.Fatalf("resolved routes include muted route: %+v", resolved)
		}
	}
	updated, err := store.UpdateNotificationRoute(ctx, threadRoute.ID, NotificationRoutePatch{
		Preference: stringPtr("mentions"),
	})
	if err != nil {
		t.Fatalf("UpdateNotificationRoute() error = %v", err)
	}
	if updated.Preference != "mentions" {
		t.Fatalf("updated.Preference = %q, want mentions", updated.Preference)
	}
	resolved, err = store.ResolveNotificationRoutes(ctx, NotificationRouteResolveOptions{
		Target:    "#ops",
		ThreadID:  "thread-incident",
		EventKind: "message",
	})
	if err != nil {
		t.Fatalf("ResolveNotificationRoutes(after mention preference) error = %v", err)
	}
	for _, route := range resolved {
		if route.ID == threadRoute.ID {
			t.Fatalf("message resolution included mentions-only route: %+v", resolved)
		}
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

func stringPtr(value string) *string {
	return &value
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

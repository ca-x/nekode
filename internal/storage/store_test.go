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

func TestMessageThreadSaveAndSearchInvariants(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	parent, err := store.CreateMessage(ctx, Message{
		ID:                "msg-parent",
		Target:            "#general",
		Role:              "user",
		Content:           "Root launch topic",
		SenderUserID:      "user-1",
		SenderDisplayName: "Alice",
		SenderKind:        "human",
		Attachments: []Attachment{{
			ID:          "att-1",
			Target:      "#general",
			Filename:    "plan.txt",
			MimeType:    "text/plain",
			SizeBytes:   12,
			StorageRef:  "local/plan.txt",
			DownloadURL: "/api/attachments/att-1",
		}},
		CreatedUnix: 100,
	})
	if err != nil {
		t.Fatalf("CreateMessage(parent) error = %v", err)
	}
	reply1, err := store.CreateMessage(ctx, Message{
		ID:               "msg-reply-1",
		Target:           "#general",
		ThreadID:         parent.ID,
		Role:             "assistant",
		Content:          "first reply",
		ReplyToMessageID: parent.ID,
		SenderAgentID:    "agent-1",
		SenderKind:       "agent",
		CreatedUnix:      101,
	})
	if err != nil {
		t.Fatalf("CreateMessage(reply1) error = %v", err)
	}
	reply2, err := store.CreateMessage(ctx, Message{
		ID:               "msg-reply-2",
		Target:           "#general",
		ThreadID:         parent.ID,
		Role:             "assistant",
		Content:          "second reply",
		ReplyToMessageID: reply1.ID,
		SenderAgentID:    "agent-1",
		SenderKind:       "agent",
		CreatedUnix:      102,
	})
	if err != nil {
		t.Fatalf("CreateMessage(reply2) error = %v", err)
	}
	standalone, err := store.CreateMessage(ctx, Message{
		ID:           "msg-standalone",
		Target:       "#general",
		Role:         "user",
		Content:      "standalone",
		SenderUserID: "user-1",
		SenderKind:   "human",
		CreatedUnix:  103,
	})
	if err != nil {
		t.Fatalf("CreateMessage(standalone) error = %v", err)
	}

	loadedParent, err := store.GetMessage(ctx, "#general", parent.ID)
	if err != nil {
		t.Fatalf("GetMessage(parent) error = %v", err)
	}
	if len(loadedParent.Attachments) != 1 || loadedParent.Attachments[0].ID != "att-1" {
		t.Fatalf("loaded parent attachments = %+v, want att-1 round trip", loadedParent.Attachments)
	}
	if _, err := store.GetMessage(ctx, "#other", parent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetMessage(wrong target) error = %v, want %v", err, ErrNotFound)
	}

	topLevel, err := store.ListMessages(ctx, "#general", "", 10)
	if err != nil {
		t.Fatalf("ListMessages(top-level) error = %v", err)
	}
	assertMessageIDs(t, topLevel, standalone.ID, parent.ID)
	threadMessages, err := store.ListMessages(ctx, "#general", parent.ID, 10)
	if err != nil {
		t.Fatalf("ListMessages(thread) error = %v", err)
	}
	assertMessageIDs(t, threadMessages, reply2.ID, reply1.ID)
	comments, err := store.ListTaskComments(ctx, parent.ID, 10)
	if err != nil {
		t.Fatalf("ListTaskComments() error = %v", err)
	}
	assertMessageIDs(t, comments, reply1.ID, reply2.ID)

	inbox, err := store.ListThreadInbox(ctx, "user-1", "#", 10)
	if err != nil {
		t.Fatalf("ListThreadInbox() error = %v", err)
	}
	if len(inbox) != 1 || inbox[0].ThreadID != parent.ID || inbox[0].MessageCount != 2 || inbox[0].UnreadCount != 2 {
		t.Fatalf("inbox = %+v, want one unread thread with two replies", inbox)
	}
	if inbox[0].FirstMessage.ID != parent.ID || inbox[0].LatestMessage.ID != reply2.ID || inbox[0].Topic != "Root launch topic" {
		t.Fatalf("inbox item = %+v, want parent topic and latest reply", inbox[0])
	}
	if err := store.MarkThreadRead(ctx, "user-1", "#general", parent.ID); err != nil {
		t.Fatalf("MarkThreadRead() error = %v", err)
	}
	inbox, err = store.ListThreadInbox(ctx, "user-1", "#", 10)
	if err != nil {
		t.Fatalf("ListThreadInbox(after read) error = %v", err)
	}
	if len(inbox) != 1 || inbox[0].UnreadCount != 0 || inbox[0].LastReadMessageID != reply2.ID {
		t.Fatalf("inbox after read = %+v, want no unread messages at reply2", inbox)
	}
	reply3, err := store.CreateMessage(ctx, Message{
		ID:               "msg-reply-3",
		Target:           "#general",
		ThreadID:         parent.ID,
		Role:             "assistant",
		Content:          "third reply from agent",
		ReplyToMessageID: reply2.ID,
		SenderAgentID:    "agent-1",
		SenderKind:       "agent",
		CreatedUnix:      104,
	})
	if err != nil {
		t.Fatalf("CreateMessage(reply3) error = %v", err)
	}
	inbox, err = store.ListThreadInbox(ctx, "user-1", "#", 10)
	if err != nil {
		t.Fatalf("ListThreadInbox(after new reply) error = %v", err)
	}
	if len(inbox) != 1 || inbox[0].UnreadCount != 1 || inbox[0].LatestMessage.ID != reply3.ID {
		t.Fatalf("inbox after new reply = %+v, want one unread latest reply3", inbox)
	}

	saved, err := store.SaveMessage(ctx, "#general", reply3.ID, "user-1", "")
	if err != nil {
		t.Fatalf("SaveMessage() error = %v", err)
	}
	duplicate, err := store.SaveMessage(ctx, "#general", reply3.ID, "user-1", "")
	if err != nil {
		t.Fatalf("SaveMessage(duplicate) error = %v", err)
	}
	if duplicate.ID != saved.ID || duplicate.Message.ID != reply3.ID {
		t.Fatalf("duplicate saved = %+v, want existing saved message %s", duplicate, saved.ID)
	}
	savedMessages, err := store.ListSavedMessages(ctx, "#general", "user-1", "", 10)
	if err != nil {
		t.Fatalf("ListSavedMessages() error = %v", err)
	}
	if len(savedMessages) != 1 || savedMessages[0].Message.ID != reply3.ID {
		t.Fatalf("savedMessages = %+v, want reply3", savedMessages)
	}
	unsaved, err := store.UnsaveMessage(ctx, "#general", reply3.ID, "user-1", "")
	if err != nil {
		t.Fatalf("UnsaveMessage() error = %v", err)
	}
	if unsaved.ID != saved.ID || unsaved.Message.ID != reply3.ID {
		t.Fatalf("unsaved = %+v, want removed saved record with message", unsaved)
	}
	if _, err := store.GetSavedMessage(ctx, "#general", reply3.ID, "user-1", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSavedMessage(after unsave) error = %v, want %v", err, ErrNotFound)
	}

	searchResults, err := store.SearchMessages(ctx, MessageSearchOptions{Query: "third", SenderHandle: "@agent-1", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	assertMessageIDs(t, searchResults, reply3.ID)

	attachmentSearch, err := store.SearchMessages(ctx, MessageSearchOptions{Query: "plan.txt", HasAttachment: true, Limit: 10})
	if err != nil {
		t.Fatalf("SearchMessages(attachment filename) error = %v", err)
	}
	assertMessageIDs(t, attachmentSearch, parent.ID)

	attachmentOnlySearch, err := store.SearchMessages(ctx, MessageSearchOptions{HasAttachment: true, Limit: 10})
	if err != nil {
		t.Fatalf("SearchMessages(has attachment) error = %v", err)
	}
	assertMessageIDs(t, attachmentOnlySearch, parent.ID)

	savedParent, err := store.SaveMessage(ctx, "#general", parent.ID, "user-1", "")
	if err != nil {
		t.Fatalf("SaveMessage(parent attachment) error = %v", err)
	}
	if _, err := store.client.SavedMessage.UpdateOneID(savedParent.ID).SetCreatedUnix(200).Save(ctx); err != nil {
		t.Fatalf("backdate saved parent error = %v", err)
	}
	for i := 0; i < 5; i++ {
		filler, err := store.CreateMessage(ctx, Message{
			ID:           "msg-saved-filler-" + string(rune('a'+i)),
			Target:       "#general",
			Role:         "user",
			Content:      "saved filler without attachment",
			SenderUserID: "user-1",
			SenderKind:   "human",
			CreatedUnix:  int64(205 + i),
		})
		if err != nil {
			t.Fatalf("CreateMessage(saved filler %d) error = %v", i, err)
		}
		savedFiller, err := store.SaveMessage(ctx, "#general", filler.ID, "user-1", "")
		if err != nil {
			t.Fatalf("SaveMessage(saved filler %d) error = %v", i, err)
		}
		if _, err := store.client.SavedMessage.UpdateOneID(savedFiller.ID).SetCreatedUnix(int64(205 + i)).Save(ctx); err != nil {
			t.Fatalf("update saved filler %d created_unix error = %v", i, err)
		}
	}
	savedAttachmentMessages, err := store.ListSavedMessagesWithOptions(ctx, SavedMessageListOptions{
		Target:        "#general",
		UserID:        "user-1",
		Query:         "plan.txt",
		HasAttachment: true,
		Limit:         1,
	})
	if err != nil {
		t.Fatalf("ListSavedMessagesWithOptions(attachment query) error = %v", err)
	}
	if len(savedAttachmentMessages) != 1 || savedAttachmentMessages[0].Message.ID != parent.ID {
		t.Fatalf("savedAttachmentMessages = %+v, want parent", savedAttachmentMessages)
	}
}

func TestTaskStateVersionAndClaimInvariants(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	task, err := store.CreateTask(ctx, Task{
		Summary:       "ship release",
		State:         "cancelled",
		Target:        "#general",
		BlockedReason: "waiting",
	})
	if err != nil {
		t.Fatalf("CreateTask(cancelled alias) error = %v", err)
	}
	if task.State != "canceled" || task.Version != 1 {
		t.Fatalf("task = %+v, want canceled state and version 1", task)
	}
	if task.CreatedUnix == 0 || task.UpdatedUnix == 0 {
		t.Fatalf("task timestamps = %+v, want persisted timestamps", task)
	}

	state := "blocked"
	assignee := "agent-1"
	blockedReason := "needs artifact"
	updated, err := store.UpdateTask(ctx, task.ID, TaskPatch{
		State:         &state,
		AssigneeID:    &assignee,
		BlockedReason: &blockedReason,
	})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if updated.State != "blocked" || updated.AssigneeID != "agent-1" || updated.BlockedReason != "needs artifact" || updated.Version != task.Version+1 {
		t.Fatalf("updated task = %+v, want blocked agent-1 version increment", updated)
	}
	claimed, accepted, err := store.ClaimTaskCAS(ctx, task.ID, "agent-1", "lease-1")
	if err != nil {
		t.Fatalf("ClaimTaskCAS(same assignee) error = %v", err)
	}
	if !accepted || claimed.AssigneeID != "agent-1" || claimed.ClaimLeaseID != "lease-1" || claimed.Version != updated.Version+1 {
		t.Fatalf("ClaimTaskCAS(same assignee) = %+v accepted=%v, want lease update and version increment", claimed, accepted)
	}
	conflict, accepted, err := store.ClaimTaskCAS(ctx, task.ID, "agent-2", "lease-2")
	if err != nil {
		t.Fatalf("ClaimTaskCAS(conflict) error = %v", err)
	}
	if accepted || conflict.AssigneeID != "agent-1" || conflict.ClaimLeaseID != "lease-1" || conflict.Version != claimed.Version {
		t.Fatalf("ClaimTaskCAS(conflict) = %+v accepted=%v, want existing claim unchanged", conflict, accepted)
	}

	missingState := "done"
	if _, err := store.UpdateTask(ctx, "missing-task", TaskPatch{State: &missingState}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateTask(missing) error = %v, want %v", err, ErrNotFound)
	}
	if _, _, err := store.ClaimTaskCAS(ctx, "missing-task", "agent-1", "lease"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClaimTaskCAS(missing) error = %v, want %v", err, ErrNotFound)
	}
}

func TestReminderLifecycleEventInvariants(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	reminder, err := store.CreateReminder(ctx, Reminder{
		ID:                    "rem-release",
		Target:                "#general",
		ScheduleKind:          "REMINDER_SCHEDULE_KIND_CRON",
		Schedule:              "0 9 * * *",
		Prompt:                "standup",
		NextRunUnix:           200,
		Title:                 "Daily standup",
		Status:                "ACTIVE",
		MsgRef:                "msg-1",
		RecurrenceRule:        "FREQ=DAILY",
		RecurrenceDescription: "daily",
		RecurrenceTimezone:    "Asia/Shanghai",
		CancelToken:           "cancel-1",
	}, "alien", "actor-1", "created from test")
	if err != nil {
		t.Fatalf("CreateReminder() error = %v", err)
	}
	if reminder.ScheduleKind != "cron" || reminder.Status != "active" || !reminder.Enabled {
		t.Fatalf("reminder = %+v, want normalized active cron reminder", reminder)
	}
	if reminder.CreatedUnix == 0 || reminder.UpdatedUnix == 0 || reminder.NextRunUnix != 200 {
		t.Fatalf("reminder timestamps = %+v, want created/updated and next run preserved", reminder)
	}
	events, err := store.ListReminderEvents(ctx, reminder.ID, 10)
	if err != nil {
		t.Fatalf("ListReminderEvents(created) error = %v", err)
	}
	if len(events) != 1 || events[0].EventType != "created" || events[0].ActorType != "system" || events[0].NextFireTimeUnix != 200 {
		t.Fatalf("created events = %+v, want normalized system created event", events)
	}

	canceled, err := store.CancelReminder(ctx, reminder.ID, "human", "user-1", "no longer needed")
	if err != nil {
		t.Fatalf("CancelReminder() error = %v", err)
	}
	if canceled.Status != "canceled" || canceled.Enabled {
		t.Fatalf("canceled reminder = %+v, want disabled canceled reminder", canceled)
	}
	active, err := store.ListReminders(ctx, "#general", nil, false, 10)
	if err != nil {
		t.Fatalf("ListReminders(active only) error = %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active reminders = %+v, want canceled reminder hidden by default", active)
	}
	all, err := store.ListReminders(ctx, "#general", nil, true, 10)
	if err != nil {
		t.Fatalf("ListReminders(include canceled) error = %v", err)
	}
	if len(all) != 1 || all[0].ID != reminder.ID {
		t.Fatalf("all reminders = %+v, want canceled reminder when included", all)
	}

	snoozed, err := store.SnoozeReminder(ctx, reminder.ID, 300, "in 10 minutes", "agent", "agent-1", "snooze")
	if err != nil {
		t.Fatalf("SnoozeReminder() error = %v", err)
	}
	if snoozed.Status != "active" || !snoozed.Enabled || snoozed.ScheduleKind != "at" || snoozed.NextRunUnix != 300 {
		t.Fatalf("snoozed reminder = %+v, want active one-shot reminder at 300", snoozed)
	}
	title := " Updated title "
	kind := "REMINDER_SCHEDULE_KIND_RRULE"
	schedule := "FREQ=WEEKLY"
	nextRun := int64(400)
	recurrenceRule := " FREQ=WEEKLY;COUNT=3 "
	timezone := " UTC "
	updated, err := store.UpdateReminder(ctx, reminder.ID, ReminderPatch{
		Title:              &title,
		ScheduleKind:       &kind,
		Schedule:           &schedule,
		NextRunUnix:        &nextRun,
		RecurrenceRule:     &recurrenceRule,
		RecurrenceTimezone: &timezone,
	}, "human", "user-1", "update")
	if err != nil {
		t.Fatalf("UpdateReminder() error = %v", err)
	}
	if updated.Title != "Updated title" || updated.ScheduleKind != "rrule" || updated.NextRunUnix != 400 ||
		updated.RecurrenceRule != "FREQ=WEEKLY;COUNT=3" || updated.RecurrenceTimezone != "UTC" || !updated.Enabled || updated.Status != "active" {
		t.Fatalf("updated reminder = %+v, want trimmed active rrule reminder", updated)
	}
	badKind := "calendar"
	if _, err := store.UpdateReminder(ctx, reminder.ID, ReminderPatch{ScheduleKind: &badKind}, "human", "user-1", "bad"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("UpdateReminder(invalid kind) error = %v, want %v", err, ErrInvalidState)
	}
	if _, err := store.CreateReminder(ctx, Reminder{
		Target:       "#general",
		ScheduleKind: "calendar",
		Status:       "active",
		Title:        "bad",
	}, "human", "user-1", "bad"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("CreateReminder(invalid kind) error = %v, want %v", err, ErrInvalidState)
	}
	if _, err := store.ListReminders(ctx, "#general", []string{"unknown"}, false, 10); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("ListReminders(invalid status) error = %v, want %v", err, ErrInvalidState)
	}
	if _, err := store.ListReminderEvents(ctx, "missing-reminder", 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListReminderEvents(missing) error = %v, want %v", err, ErrNotFound)
	}

	events, err = store.ListReminderEvents(ctx, reminder.ID, 10)
	if err != nil {
		t.Fatalf("ListReminderEvents(final) error = %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %+v, want created/canceled/snoozed/updated events", events)
	}
	createdEvent := requireReminderEvent(t, events, "created")
	canceledEvent := requireReminderEvent(t, events, "canceled")
	snoozedEvent := requireReminderEvent(t, events, "snoozed")
	updatedEvent := requireReminderEvent(t, events, "updated")
	if createdEvent.ActorType != "system" || canceledEvent.ActorType != "human" || snoozedEvent.ActorType != "agent" || updatedEvent.ActorType != "human" {
		t.Fatalf("events = %+v, want actor types normalized/preserved by event type", events)
	}
	if createdEvent.NextFireTimeUnix != 200 || canceledEvent.NextFireTimeUnix != 200 || snoozedEvent.NextFireTimeUnix != 300 || updatedEvent.NextFireTimeUnix != 400 {
		t.Fatalf("events = %+v, want next fire timestamps captured per lifecycle operation", events)
	}
}

func TestCollaborationEventAndIdempotencyInvariants(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if _, err := store.AppendCollaborationEvent(ctx, CollaborationEvent{
		Kind:        "message",
		PayloadJSON: "{}",
	}); err == nil {
		t.Fatal("AppendCollaborationEvent(without server) succeeded, want error")
	}
	first, err := store.AppendCollaborationEvent(ctx, CollaborationEvent{
		ServerID:    "srv-a",
		EventID:     "event-1",
		Target:      "#general",
		AggregateID: "thread-1",
		Kind:        "message",
		Operation:   "appended",
		PayloadJSON: "{}",
	})
	if err != nil {
		t.Fatalf("AppendCollaborationEvent(first) error = %v", err)
	}
	second, err := store.AppendCollaborationEvent(ctx, CollaborationEvent{
		ServerID:    "srv-a",
		EventID:     "event-2",
		Target:      "#general",
		AggregateID: "thread-1",
		Kind:        "task",
		Operation:   "created",
		PayloadJSON: "{}",
	})
	if err != nil {
		t.Fatalf("AppendCollaborationEvent(second) error = %v", err)
	}
	otherServer, err := store.AppendCollaborationEvent(ctx, CollaborationEvent{
		ServerID:    "srv-b",
		EventID:     "event-3",
		Target:      "#general",
		Kind:        "message",
		Operation:   "appended",
		PayloadJSON: "{}",
	})
	if err != nil {
		t.Fatalf("AppendCollaborationEvent(other server) error = %v", err)
	}
	if first.Sequence != 1 || second.Sequence != 2 || otherServer.Sequence != 1 {
		t.Fatalf("sequences = first:%d second:%d other:%d, want per-server monotonic sequences", first.Sequence, second.Sequence, otherServer.Sequence)
	}
	if _, err := store.AppendCollaborationEvent(ctx, CollaborationEvent{
		ServerID:    "srv-a",
		EventID:     "event-1",
		Target:      "#general",
		Kind:        "message",
		Operation:   "appended",
		PayloadJSON: "{}",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("AppendCollaborationEvent(duplicate event id) error = %v, want %v", err, ErrConflict)
	}
	events, err := store.ListCollaborationEvents(ctx, "srv-a", "#general", "thread-1", first.Sequence, 10)
	if err != nil {
		t.Fatalf("ListCollaborationEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].ID != second.ID {
		t.Fatalf("events after first sequence = %+v, want only second event", events)
	}

	reserved, created, err := store.ReserveIdempotencyRecord(ctx, IdempotencyRecord{
		Scope:          "http",
		Method:         "CreateMessage",
		ActorID:        "user-1",
		IdempotencyKey: "idem-1",
		RequestHash:    "hash-1",
	})
	if err != nil {
		t.Fatalf("ReserveIdempotencyRecord(first) error = %v", err)
	}
	if !created || reserved.Status != "pending" || reserved.ExpiresUnix <= reserved.CreatedUnix {
		t.Fatalf("reserved = %+v created=%v, want pending create with expiry", reserved, created)
	}
	replayed, created, err := store.ReserveIdempotencyRecord(ctx, IdempotencyRecord{
		Scope:          "http",
		Method:         "CreateMessage",
		ActorID:        "user-1",
		IdempotencyKey: "idem-1",
		RequestHash:    "different-hash",
	})
	if err != nil {
		t.Fatalf("ReserveIdempotencyRecord(replay) error = %v", err)
	}
	if created || replayed.ID != reserved.ID || replayed.RequestHash != "hash-1" {
		t.Fatalf("replayed = %+v created=%v, want existing reserved record", replayed, created)
	}
	if err := store.CompleteIdempotencyRecord(ctx, IdempotencyRecord{
		Scope:          "http",
		Method:         "CreateMessage",
		ActorID:        "user-1",
		IdempotencyKey: "idem-1",
		RequestHash:    "hash-1",
		ResponseType:   "storage.Message",
		ResponseJSON:   `{"id":"msg_1"}`,
		ResourceType:   "message",
		ResourceID:     "msg_1",
	}); err != nil {
		t.Fatalf("CompleteIdempotencyRecord() error = %v", err)
	}
	completed, err := store.GetIdempotencyRecord(ctx, "http", "CreateMessage", "user-1", "idem-1")
	if err != nil {
		t.Fatalf("GetIdempotencyRecord() error = %v", err)
	}
	if completed.Status != "completed" || completed.ResponseJSON == "" || completed.ResourceID != "msg_1" {
		t.Fatalf("completed idempotency record = %+v, want completed resource response", completed)
	}
	if err := store.CompleteIdempotencyRecord(ctx, IdempotencyRecord{
		Scope:          "http",
		Method:         "CreateMessage",
		ActorID:        "user-1",
		IdempotencyKey: "missing",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CompleteIdempotencyRecord(missing) error = %v, want %v", err, ErrNotFound)
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

func TestNotificationRouteResolutionDedupesThreadSpecificRoutes(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	sharedEndpoint, err := store.CreateInteractionEndpoint(ctx, InteractionEndpoint{
		Kind:            "im",
		Provider:        "feishu",
		DisplayName:     "Feishu Shared",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "bearer",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint(shared) error = %v", err)
	}
	disabledEndpoint, err := store.CreateInteractionEndpoint(ctx, InteractionEndpoint{
		Kind:            "im",
		Provider:        "terminal",
		DisplayName:     "Terminal Disabled Route",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "none",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint(disabled) error = %v", err)
	}
	mentionEndpoint, err := store.CreateInteractionEndpoint(ctx, InteractionEndpoint{
		Kind:            "im",
		Provider:        "telegram",
		DisplayName:     "Telegram Mentions",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "bearer",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint(mention) error = %v", err)
	}

	defaultRoute, err := store.CreateNotificationRoute(ctx, NotificationRoute{
		Target:     "#ops",
		EndpointID: sharedEndpoint.ID,
		EventKind:  "all",
		Preference: "all",
		Enabled:    true,
		ConfigJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateNotificationRoute(default) error = %v", err)
	}
	threadRoute, err := store.CreateNotificationRoute(ctx, NotificationRoute{
		Target:     "#ops",
		ThreadID:   "thread-incident",
		EndpointID: sharedEndpoint.ID,
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
		EndpointID: disabledEndpoint.ID,
		EventKind:  "message",
		Preference: "all",
		Enabled:    false,
		ConfigJSON: "{}",
	}); err != nil {
		t.Fatalf("CreateNotificationRoute(disabled) error = %v", err)
	}
	mentionRoute, err := store.CreateNotificationRoute(ctx, NotificationRoute{
		Target:     "#ops",
		EndpointID: mentionEndpoint.ID,
		EventKind:  "mention",
		Preference: "mentions",
		Enabled:    true,
		ConfigJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateNotificationRoute(mention) error = %v", err)
	}

	messageRoutes, err := store.ResolveNotificationRoutes(ctx, NotificationRouteResolveOptions{
		Target:    "#ops",
		ThreadID:  "thread-incident",
		EventKind: "message",
	})
	if err != nil {
		t.Fatalf("ResolveNotificationRoutes(message thread) error = %v", err)
	}
	if len(messageRoutes) != 1 || messageRoutes[0].ID != threadRoute.ID {
		t.Fatalf("messageRoutes = %+v, want only thread-specific route after endpoint dedupe", messageRoutes)
	}

	defaultRoutes, err := store.ResolveNotificationRoutes(ctx, NotificationRouteResolveOptions{
		Target:    "#ops",
		EventKind: "message",
	})
	if err != nil {
		t.Fatalf("ResolveNotificationRoutes(default message) error = %v", err)
	}
	if len(defaultRoutes) != 1 || defaultRoutes[0].ID != defaultRoute.ID {
		t.Fatalf("defaultRoutes = %+v, want default route only", defaultRoutes)
	}

	mentionRoutes, err := store.ResolveNotificationRoutes(ctx, NotificationRouteResolveOptions{
		Target:    "#ops",
		EventKind: "mention",
	})
	if err != nil {
		t.Fatalf("ResolveNotificationRoutes(mention) error = %v", err)
	}
	if len(mentionRoutes) != 2 || mentionRoutes[0].ID != mentionRoute.ID || mentionRoutes[1].ID != defaultRoute.ID {
		t.Fatalf("mentionRoutes = %+v, want mention route before all-events default", mentionRoutes)
	}
}

func TestNotificationRouteValidation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	endpoint, err := store.CreateInteractionEndpoint(ctx, InteractionEndpoint{
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
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	validRoute, err := store.CreateNotificationRoute(ctx, NotificationRoute{
		Target:     "#ops",
		EndpointID: endpoint.ID,
		EventKind:  "message",
		Preference: "all",
		Enabled:    true,
		ConfigJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateNotificationRoute(valid) error = %v", err)
	}

	createCases := []struct {
		name  string
		route NotificationRoute
	}{
		{
			name: "empty target",
			route: NotificationRoute{
				EndpointID: endpoint.ID,
				EventKind:  "message",
				Preference: "all",
				Enabled:    true,
				ConfigJSON: "{}",
			},
		},
		{
			name: "invalid event kind",
			route: NotificationRoute{
				Target:     "#ops",
				EndpointID: endpoint.ID,
				EventKind:  "email",
				Preference: "all",
				Enabled:    true,
				ConfigJSON: "{}",
			},
		},
		{
			name: "invalid preference",
			route: NotificationRoute{
				Target:     "#ops",
				EndpointID: endpoint.ID,
				EventKind:  "message",
				Preference: "sometimes",
				Enabled:    true,
				ConfigJSON: "{}",
			},
		},
		{
			name: "invalid config json",
			route: NotificationRoute{
				Target:     "#ops",
				EndpointID: endpoint.ID,
				EventKind:  "message",
				Preference: "all",
				Enabled:    true,
				ConfigJSON: "{",
			},
		},
	}
	for _, tc := range createCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.CreateNotificationRoute(ctx, tc.route); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("CreateNotificationRoute() error = %v, want %v", err, ErrInvalidState)
			}
		})
	}

	if _, err := store.CreateNotificationRoute(ctx, NotificationRoute{
		Target:     "#ops",
		EndpointID: endpoint.ID,
		EventKind:  "message",
		Preference: "all",
		Enabled:    true,
		ConfigJSON: "{}",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate CreateNotificationRoute() error = %v, want %v", err, ErrConflict)
	}
	if _, err := store.ListNotificationRoutes(ctx, NotificationRouteListOptions{
		Target:    "#ops",
		EventKind: "email",
		Limit:     10,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("ListNotificationRoutes(invalid event) error = %v, want %v", err, ErrInvalidState)
	}
	if _, err := store.ResolveNotificationRoutes(ctx, NotificationRouteResolveOptions{
		Target:    "",
		EventKind: "message",
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("ResolveNotificationRoutes(empty target) error = %v, want %v", err, ErrInvalidState)
	}
	if _, err := store.ResolveNotificationRoutes(ctx, NotificationRouteResolveOptions{
		Target:    "#ops",
		EventKind: "all",
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("ResolveNotificationRoutes(all event) error = %v, want %v", err, ErrInvalidState)
	}
	if _, err := store.UpdateNotificationRoute(ctx, validRoute.ID, NotificationRoutePatch{
		ConfigJSON: stringPtr("{"),
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("UpdateNotificationRoute(invalid config) error = %v, want %v", err, ErrInvalidState)
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

func assertMessageIDs(t *testing.T, messages []Message, want ...string) {
	t.Helper()
	if len(messages) != len(want) {
		t.Fatalf("message ids length = %d, want %d; messages = %+v", len(messages), len(want), messages)
	}
	for i, message := range messages {
		if message.ID != want[i] {
			t.Fatalf("message ids[%d] = %q, want %q; messages = %+v", i, message.ID, want[i], messages)
		}
	}
}

func requireReminderEvent(t *testing.T, events []ReminderEvent, eventType string) ReminderEvent {
	t.Helper()
	for _, event := range events {
		if event.EventType == eventType {
			return event
		}
	}
	t.Fatalf("event type %q not found in %+v", eventType, events)
	return ReminderEvent{}
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

package daemonrpc

import (
	"context"
	"net"
	"testing"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestDaemonControlFlow(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()
	ctx := context.Background()

	registerReq := &daemonv1.RegisterComputerRequest{
		Info: &daemonv1.ComputerInfo{
			DaemonId:   "daemon-1",
			ComputerId: "computer-1",
			Hostname:   "test-host",
			Status:     daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE,
		},
		RequestId:      "register-1",
		IdempotencyKey: "register-1",
	}
	registered, err := client.RegisterComputer(ctx, registerReq)
	if err != nil {
		t.Fatalf("RegisterComputer() error = %v", err)
	}
	if !registered.GetAccepted() || registered.GetLease().GetLeaseId() == "" {
		t.Fatalf("RegisterComputer() = %+v, want accepted lease", registered)
	}
	replayed, err := client.RegisterComputer(ctx, registerReq)
	if err != nil {
		t.Fatalf("RegisterComputer replay error = %v", err)
	}
	if replayed.GetLease().GetLeaseId() != registered.GetLease().GetLeaseId() {
		t.Fatalf("replay lease = %q, want %q", replayed.GetLease().GetLeaseId(), registered.GetLease().GetLeaseId())
	}

	heartbeat, err := client.HeartbeatComputer(ctx, &daemonv1.HeartbeatComputerRequest{
		Info: &daemonv1.ComputerInfo{
			DaemonId:   "daemon-1",
			ComputerId: "computer-1",
			Status:     daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE,
		},
		LeaseId: registered.GetLease().GetLeaseId(),
		AgentStatuses: []*daemonv1.AgentStatusSnapshot{
			{
				AgentId:       "agent-1",
				ComputerId:    "computer-1",
				Presence:      daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE,
				ActivityState: daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_THINKING,
				Health:        daemonv1.AgentHealth_AGENT_HEALTH_OK,
				Severity:      daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_INFO,
			},
		},
	})
	if err != nil {
		t.Fatalf("HeartbeatComputer() error = %v", err)
	}
	if !heartbeat.GetAccepted() {
		t.Fatalf("HeartbeatComputer accepted = false")
	}

	presets, err := client.ListRuntimePresets(ctx, &daemonv1.ListRuntimePresetsRequest{IncludeExperimental: true})
	if err != nil {
		t.Fatalf("ListRuntimePresets() error = %v", err)
	}
	presetKinds := map[string]bool{}
	for _, preset := range presets.GetPresets() {
		presetKinds[preset.GetKind()] = true
	}
	for _, kind := range []string{"codex", "claude", "opencode", "kimi", "gemini", "cursor-agent", "copilot", "openclaw", "hermes", "pi", "kiro-cli"} {
		if !presetKinds[kind] {
			t.Fatalf("ListRuntimePresets missing %q; got=%v", kind, presetKinds)
		}
	}

	statuses, err := client.ListAgentStatuses(ctx, &daemonv1.ListAgentStatusesRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("ListAgentStatuses() error = %v", err)
	}
	if len(statuses.GetStatuses()) != 1 || statuses.GetStatuses()[0].GetPresence() != daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE {
		t.Fatalf("ListAgentStatuses() = %+v, want one online status", statuses.GetStatuses())
	}

	streamCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	stream, err := client.SubscribeServerEvents(streamCtx, &daemonv1.SubscribeServerEventsRequest{
		DaemonId:   "daemon-1",
		ComputerId: "computer-1",
		RequestId:  "stream-1",
	})
	if err != nil {
		t.Fatalf("SubscribeServerEvents() error = %v", err)
	}
	eventResp, err := stream.Recv()
	if err != nil {
		t.Fatalf("SubscribeServerEvents Recv() error = %v", err)
	}
	if eventResp.GetEvent().GetKind() != daemonv1.ServerEventKind_SERVER_EVENT_KIND_PING {
		t.Fatalf("server event kind = %v, want ping", eventResp.GetEvent().GetKind())
	}
	if eventResp.GetEvent().GetOperation() != daemonv1.EventOperation_EVENT_OPERATION_HEARTBEAT {
		t.Fatalf("server event operation = %v, want heartbeat", eventResp.GetEvent().GetOperation())
	}
	if eventResp.GetEvent().GetScope().GetScopeType() != daemonv1.EventScopeType_EVENT_SCOPE_TYPE_COMPUTER ||
		eventResp.GetEvent().GetScope().GetScopeId() != "computer-1" {
		t.Fatalf("server event scope = %+v, want computer-1", eventResp.GetEvent().GetScope())
	}
	ack, err := client.AcknowledgeServerEvents(ctx, &daemonv1.AcknowledgeServerEventsRequest{
		DaemonId:       "daemon-1",
		ComputerId:     "computer-1",
		EventIds:       []string{eventResp.GetEvent().GetEventId()},
		Cursor:         &daemonv1.EventCursor{Sequence: eventResp.GetEvent().GetSequence()},
		RequestId:      "ack-1",
		IdempotencyKey: "ack-1",
	})
	if err != nil {
		t.Fatalf("AcknowledgeServerEvents() error = %v", err)
	}
	if !ack.GetAccepted() {
		t.Fatalf("AcknowledgeServerEvents accepted = false")
	}
}

func TestComputerAndAgentStatusBecomeStaleOfflineAndRecover(t *testing.T) {
	srv := New(newTestStore(t, "daemonrpc_stale"), "srv_test")
	ctx := context.Background()
	registerReq := &daemonv1.RegisterComputerRequest{
		Info: &daemonv1.ComputerInfo{
			DaemonId:   "daemon-1",
			ComputerId: "computer-1",
			Hostname:   "test-host",
			Status:     daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE,
		},
		Inventory: &daemonv1.ComputerInventory{
			Agents: []*daemonv1.AgentProfile{
				{
					AgentId:          "agent-1",
					ComputerId:       "computer-1",
					RuntimeProfileId: "profile-1",
					Status:           daemonv1.AgentPresence_AGENT_PRESENCE_IDLE,
				},
			},
		},
		RequestId:      "register-stale-1",
		IdempotencyKey: "register-stale-1",
	}
	registered, err := srv.RegisterComputer(ctx, registerReq)
	if err != nil {
		t.Fatalf("RegisterComputer() error = %v", err)
	}
	_, err = srv.HeartbeatComputer(ctx, &daemonv1.HeartbeatComputerRequest{
		Info: &daemonv1.ComputerInfo{
			DaemonId:   "daemon-1",
			ComputerId: "computer-1",
			Status:     daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE,
		},
		LeaseId: registered.GetLease().GetLeaseId(),
		AgentStatuses: []*daemonv1.AgentStatusSnapshot{
			{
				AgentId:       "agent-1",
				ComputerId:    "computer-1",
				Presence:      daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE,
				ActivityState: daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_THINKING,
				Health:        daemonv1.AgentHealth_AGENT_HEALTH_OK,
				Severity:      daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_INFO,
				Summary:       "working",
			},
		},
	})
	if err != nil {
		t.Fatalf("HeartbeatComputer() error = %v", err)
	}

	assertComputerAndAgentStatus(t, srv, daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE, daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE, daemonv1.AgentHealth_AGENT_HEALTH_OK)

	setComputerLastHeartbeat(t, srv, "computer-1", unixNow()-computerStaleAfterSeconds-5)
	assertComputerAndAgentStatus(t, srv, daemonv1.ComputerStatus_COMPUTER_STATUS_STALE, daemonv1.AgentPresence_AGENT_PRESENCE_STALE, daemonv1.AgentHealth_AGENT_HEALTH_OK)
	staleInventory := srv.ListComputerInventories(10)
	if len(staleInventory) != 1 || len(staleInventory[0].Inventory.GetAgents()) != 1 ||
		staleInventory[0].Inventory.GetAgents()[0].GetStatus() != daemonv1.AgentPresence_AGENT_PRESENCE_STALE {
		t.Fatalf("stale inventory = %+v, want inventory agent marked stale", staleInventory)
	}

	setComputerLastHeartbeat(t, srv, "computer-1", unixNow()-computerOfflineAfterSeconds-5)
	assertComputerAndAgentStatus(t, srv, daemonv1.ComputerStatus_COMPUTER_STATUS_OFFLINE, daemonv1.AgentPresence_AGENT_PRESENCE_OFFLINE, daemonv1.AgentHealth_AGENT_HEALTH_OFFLINE)

	_, err = srv.HeartbeatComputer(ctx, &daemonv1.HeartbeatComputerRequest{
		Info: &daemonv1.ComputerInfo{
			DaemonId:   "daemon-1",
			ComputerId: "computer-1",
			Status:     daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE,
		},
		LeaseId: registered.GetLease().GetLeaseId(),
		AgentStatuses: []*daemonv1.AgentStatusSnapshot{
			{
				AgentId:       "agent-1",
				ComputerId:    "computer-1",
				Presence:      daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE,
				ActivityState: daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_THINKING,
				Health:        daemonv1.AgentHealth_AGENT_HEALTH_OK,
				Severity:      daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_INFO,
				Summary:       "recovered",
			},
		},
	})
	if err != nil {
		t.Fatalf("HeartbeatComputer(recover) error = %v", err)
	}
	assertComputerAndAgentStatus(t, srv, daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE, daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE, daemonv1.AgentHealth_AGENT_HEALTH_OK)
}

func TestMessageTaskAndActivityFlow(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()
	ctx := context.Background()

	message, err := client.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:         "#general",
		Content:        "hello",
		RequestId:      "msg-1",
		IdempotencyKey: "msg-1",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-1",
			DisplayName: "Agent One",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if !message.GetAccepted() || message.GetMessage().GetSender().GetActorKind() != daemonv1.ActorKind_ACTOR_KIND_AGENT {
		t.Fatalf("SendMessage() = %+v, want accepted agent sender", message)
	}
	messages, err := client.ReadMessages(ctx, &daemonv1.ReadMessagesRequest{Target: "#general"})
	if err != nil {
		t.Fatalf("ReadMessages() error = %v", err)
	}
	if len(messages.GetMessages()) != 1 {
		t.Fatalf("ReadMessages() returned %d messages, want 1", len(messages.GetMessages()))
	}

	task, err := client.CreateCollaborationTask(ctx, &daemonv1.CreateCollaborationTaskRequest{
		Target:         "#general",
		Summary:        "wire daemon bridge",
		AgentId:        "agent-1",
		RequestId:      "task-1",
		IdempotencyKey: "task-1",
	})
	if err != nil {
		t.Fatalf("CreateCollaborationTask() error = %v", err)
	}
	if task.GetTask().GetState() != daemonv1.TaskState_TASK_STATE_TODO {
		t.Fatalf("task state = %v, want todo", task.GetTask().GetState())
	}
	nextState := daemonv1.TaskState_TASK_STATE_IN_PROGRESS
	updated, err := client.UpdateTask(ctx, &daemonv1.UpdateTaskRequest{
		TaskId:         task.GetTask().GetTaskId(),
		State:          &nextState,
		RequestId:      "task-update-1",
		IdempotencyKey: "task-update-1",
	})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if !updated.GetAccepted() || updated.GetTask().GetState() != daemonv1.TaskState_TASK_STATE_IN_PROGRESS {
		t.Fatalf("UpdateTask() = %+v, want in progress", updated)
	}
	listed, err := client.ListCollaborationTasks(ctx, &daemonv1.ListCollaborationTasksRequest{
		Target: "#general",
		States: []daemonv1.TaskState{daemonv1.TaskState_TASK_STATE_IN_PROGRESS},
	})
	if err != nil {
		t.Fatalf("ListCollaborationTasks() error = %v", err)
	}
	if len(listed.GetTasks()) != 1 {
		t.Fatalf("ListCollaborationTasks() returned %d tasks, want 1", len(listed.GetTasks()))
	}
	claim, err := client.ClaimCollaborationTask(ctx, &daemonv1.ClaimCollaborationTaskRequest{
		TaskId:         task.GetTask().GetTaskId(),
		AgentId:        "agent-1",
		RequestId:      "claim-1",
		IdempotencyKey: "claim-1",
	})
	if err != nil {
		t.Fatalf("ClaimCollaborationTask() error = %v", err)
	}
	if !claim.GetAccepted() || claim.GetClaimLease().GetLeaseId() == "" {
		t.Fatalf("ClaimCollaborationTask() = %+v, want accepted lease", claim)
	}
	conflict, err := client.ClaimCollaborationTask(ctx, &daemonv1.ClaimCollaborationTaskRequest{
		TaskId:         task.GetTask().GetTaskId(),
		AgentId:        "agent-2",
		RequestId:      "claim-2",
		IdempotencyKey: "claim-2",
	})
	if err != nil {
		t.Fatalf("ClaimCollaborationTask(conflict) error = %v", err)
	}
	if conflict.GetAccepted() || conflict.GetCurrentAssigneeId() != "agent-1" {
		t.Fatalf("ClaimCollaborationTask(conflict) = %+v, want conflict with agent-1", conflict)
	}
	blockedState := daemonv1.TaskState_TASK_STATE_BLOCKED
	blockedTask, err := client.UpdateTask(ctx, &daemonv1.UpdateTaskRequest{
		TaskId:         task.GetTask().GetTaskId(),
		State:          &blockedState,
		RequestId:      "task-update-blocked-1",
		IdempotencyKey: "task-update-blocked-1",
	})
	if err != nil {
		t.Fatalf("UpdateTask(blocked) error = %v", err)
	}
	if !blockedTask.GetAccepted() ||
		blockedTask.GetTask().GetState() != daemonv1.TaskState_TASK_STATE_BLOCKED ||
		blockedTask.GetTask().GetBoardColumn() != "blocked" {
		t.Fatalf("UpdateTask(blocked) = %+v, want blocked task and board column", blockedTask)
	}
	canceledCreate, err := client.CreateCollaborationTask(ctx, &daemonv1.CreateCollaborationTaskRequest{
		Target:         "#general",
		Summary:        "cancel stale branch",
		AgentId:        "agent-1",
		RequestId:      "task-cancel-1",
		IdempotencyKey: "task-cancel-1",
	})
	if err != nil {
		t.Fatalf("CreateCollaborationTask(canceled) error = %v", err)
	}
	canceledState := daemonv1.TaskState_TASK_STATE_CANCELED
	canceledUpdate, err := client.UpdateTask(ctx, &daemonv1.UpdateTaskRequest{
		TaskId:         canceledCreate.GetTask().GetTaskId(),
		State:          &canceledState,
		RequestId:      "task-update-canceled-1",
		IdempotencyKey: "task-update-canceled-1",
	})
	if err != nil {
		t.Fatalf("UpdateTask(canceled) error = %v", err)
	}
	if !canceledUpdate.GetAccepted() ||
		canceledUpdate.GetTask().GetState() != daemonv1.TaskState_TASK_STATE_CANCELED ||
		canceledUpdate.GetTask().GetBoardColumn() != "canceled" {
		t.Fatalf("UpdateTask(canceled) = %+v, want canceled task and board column", canceledUpdate)
	}
	board, err := client.ListTaskBoard(ctx, &daemonv1.ListTaskBoardRequest{
		Target: "#general",
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("ListTaskBoard() error = %v", err)
	}
	columnIndex := map[string]int{}
	for i, column := range board.GetBoard().GetColumns() {
		columnIndex[column.GetColumn()] = i
	}
	blockedIndex, hasBlocked := columnIndex["blocked"]
	canceledIndex, hasCanceled := columnIndex["canceled"]
	if !hasBlocked || !hasCanceled {
		t.Fatalf("ListTaskBoard columns = %+v, want blocked and canceled", board.GetBoard().GetColumns())
	}
	if blockedIndex >= canceledIndex {
		t.Fatalf("ListTaskBoard column order blocked=%d canceled=%d, want blocked before canceled", blockedIndex, canceledIndex)
	}

	activity, err := client.LogActivity(ctx, &daemonv1.LogActivityRequest{
		Target:         "#general",
		AgentId:        "agent-1",
		Kind:           "test_run",
		Summary:        "tests passed",
		RequestId:      "activity-1",
		IdempotencyKey: "activity-1",
	})
	if err != nil {
		t.Fatalf("LogActivity() error = %v", err)
	}
	events, err := client.ListEventsSince(ctx, &daemonv1.ListEventsSinceRequest{
		Cursor: &daemonv1.EventCursor{Sequence: 0, Target: "#general"},
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("ListEventsSince() error = %v", err)
	}
	if findEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_MESSAGE, daemonv1.EventOperation_EVENT_OPERATION_APPENDED) == nil {
		t.Fatalf("ListEventsSince() = %+v, want appended message event", events.GetEvents())
	}
	createdTaskEvent := findEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_TASK, daemonv1.EventOperation_EVENT_OPERATION_CREATED)
	if createdTaskEvent == nil || createdTaskEvent.GetTask().GetTaskId() == "" ||
		createdTaskEvent.GetScope().GetScopeType() != daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TASK {
		t.Fatalf("ListEventsSince() = %+v, want created task event with task scope", events.GetEvents())
	}
	if findEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_TASK, daemonv1.EventOperation_EVENT_OPERATION_STATE_CHANGED) == nil {
		t.Fatalf("ListEventsSince() = %+v, want task state_changed event", events.GetEvents())
	}
	if findEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_TASK, daemonv1.EventOperation_EVENT_OPERATION_CLAIMED) == nil {
		t.Fatalf("ListEventsSince() = %+v, want task claimed event", events.GetEvents())
	}
	activityEvent := findEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_ACTIVITY, daemonv1.EventOperation_EVENT_OPERATION_CREATED)
	if activityEvent == nil || activityEvent.GetActivity().GetActivityId() != activity.GetActivity().GetActivityId() {
		t.Fatalf("ListEventsSince() = %+v, want logged activity event", events.GetEvents())
	}
	if activityEvent.GetScope().GetScopeType() != daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TARGET ||
		activityEvent.GetScope().GetTarget() != "#general" {
		t.Fatalf("event scope = %+v, want target #general", activityEvent.GetScope())
	}
	if events.GetNextCursor().GetServerId() != "srv_test" {
		t.Fatalf("next cursor server_id = %q, want srv_test", events.GetNextCursor().GetServerId())
	}
}

func TestOutboundDeliveryLifecycleForIMReply(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "daemonrpc_outbound")
	srv := New(store, "srv_test")

	endpoint, err := store.CreateInteractionEndpoint(ctx, storage.InteractionEndpoint{
		Kind:            "im",
		Provider:        "feishu",
		DisplayName:     "Feishu Ops",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "bearer",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		Role:              "user",
		Content:           "incident update",
		SenderDisplayName: "Feishu User",
		SenderKind:        "endpoint",
		SourceEndpointID:  endpoint.ID,
		ExternalMessageID: "feishu-msg-1",
		MetadataJSON:      `{"im":{"provider":"feishu","conversation":{"id":"chat-1","display_name":"Ops"},"sender":{"id":"user-1","display_name":"Feishu User"}}}`,
		RequestID:         "iep-feishu:feishu-msg-1",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}

	reply, err := srv.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           "#ops",
		Content:          "acknowledged",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "reply-1",
		IdempotencyKey:   "reply-1",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-1",
			DisplayName: "Agent One",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage(reply) error = %v", err)
	}
	if !reply.GetAccepted() || reply.GetMessage().GetSourceEndpointId() != "" {
		t.Fatalf("SendMessage(reply) = %+v, want accepted Web-visible agent reply without copied source endpoint", reply)
	}

	listed, err := srv.ListOutboundDeliveries(ctx, &daemonv1.ListOutboundDeliveriesRequest{
		Target:    "#ops",
		MessageId: reply.GetMessage().GetMessageId(),
		Statuses:  []daemonv1.OutboundDeliveryStatus{daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING},
	})
	if err != nil {
		t.Fatalf("ListOutboundDeliveries() error = %v", err)
	}
	if len(listed.GetDeliveries()) != 1 {
		t.Fatalf("ListOutboundDeliveries() returned %d deliveries, want 1", len(listed.GetDeliveries()))
	}
	delivery := listed.GetDeliveries()[0]
	if delivery.GetEndpointId() != endpoint.ID ||
		delivery.GetEndpointKind() != "im" ||
		delivery.GetExternalMessageId() != "feishu-msg-1" ||
		delivery.GetStatus() != daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING {
		t.Fatalf("delivery = %+v, want pending source-only IM delivery", delivery)
	}

	retryReq := &daemonv1.RetryOutboundDeliveryRequest{
		DeliveryId:     delivery.GetDeliveryId(),
		RequestId:      "retry-1",
		IdempotencyKey: "retry-1",
	}
	retry, err := srv.RetryOutboundDelivery(ctx, retryReq)
	if err != nil {
		t.Fatalf("RetryOutboundDelivery() error = %v", err)
	}
	if retry.GetDelivery().GetStatus() != daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_RETRYING ||
		retry.GetDelivery().GetAttemptCount() != 1 ||
		retry.GetDelivery().GetLastError() != "" {
		t.Fatalf("RetryOutboundDelivery() = %+v, want retrying attempt 1 without last error", retry.GetDelivery())
	}
	replayed, err := srv.RetryOutboundDelivery(ctx, retryReq)
	if err != nil {
		t.Fatalf("RetryOutboundDelivery(replay) error = %v", err)
	}
	if replayed.GetDelivery().GetAttemptCount() != retry.GetDelivery().GetAttemptCount() {
		t.Fatalf("retry replay attempt_count = %d, want %d", replayed.GetDelivery().GetAttemptCount(), retry.GetDelivery().GetAttemptCount())
	}
	delivered, err := srv.RecordOutboundDeliveryStatus(ctx, delivery.GetDeliveryId(), daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_DELIVERED, "", 12345, 0)
	if err != nil {
		t.Fatalf("RecordOutboundDeliveryStatus(delivered) error = %v", err)
	}
	if delivered.Status != "delivered" || delivered.DeliveredTimeUnix == 0 || delivered.NextRetryTimeUnix != 0 {
		t.Fatalf("delivered status = %+v, want delivered timestamp without retry time", delivered)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	seenOutboundEvent := false
	seenDeliveredEvent := false
	for _, event := range srv.serverEvents {
		if event.GetKind() == daemonv1.ServerEventKind_SERVER_EVENT_KIND_OUTBOUND_DELIVERY &&
			event.GetOutboundDelivery().GetDeliveryId() == delivery.GetDeliveryId() &&
			event.GetTarget() == "#ops" {
			seenOutboundEvent = true
			if event.GetOperation() == daemonv1.EventOperation_EVENT_OPERATION_STATE_CHANGED &&
				event.GetOutboundDelivery().GetStatus() == daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_DELIVERED {
				seenDeliveredEvent = true
			}
		}
	}
	if !seenOutboundEvent {
		t.Fatalf("serverEvents = %+v, want outbound delivery event for %s", srv.serverEvents, delivery.GetDeliveryId())
	}
	if !seenDeliveredEvent {
		t.Fatalf("serverEvents = %+v, want delivered outbound status event for %s", srv.serverEvents, delivery.GetDeliveryId())
	}
}

func TestOutboundDeliveryUsesNotificationRoutes(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "daemonrpc_notification_routes")
	srv := New(store, "srv_test")

	endpoint, err := store.CreateInteractionEndpoint(ctx, storage.InteractionEndpoint{
		Kind:            "im",
		Provider:        "telegram",
		DisplayName:     "Telegram Alerts",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "bearer",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	if _, err := store.CreateNotificationRoute(ctx, storage.NotificationRoute{
		Target:     "#ops",
		EndpointID: endpoint.ID,
		EventKind:  "message",
		Preference: "all",
		Enabled:    true,
		ConfigJSON: `{"purpose":"alerts"}`,
	}); err != nil {
		t.Fatalf("CreateNotificationRoute() error = %v", err)
	}

	sent, err := srv.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:         "#ops",
		Content:        "deploy complete",
		OutboundPolicy: daemonv1.OutboundPolicy_OUTBOUND_POLICY_ALL_BOUND_ENDPOINTS,
		RequestId:      "notify-1",
		IdempotencyKey: "notify-1",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-release",
			DisplayName: "Release Agent",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	listed, err := srv.ListOutboundDeliveries(ctx, &daemonv1.ListOutboundDeliveriesRequest{
		Target:    "#ops",
		MessageId: sent.GetMessage().GetMessageId(),
		Statuses:  []daemonv1.OutboundDeliveryStatus{daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING},
	})
	if err != nil {
		t.Fatalf("ListOutboundDeliveries() error = %v", err)
	}
	if len(listed.GetDeliveries()) != 1 {
		t.Fatalf("ListOutboundDeliveries() returned %d deliveries, want 1", len(listed.GetDeliveries()))
	}
	delivery := listed.GetDeliveries()[0]
	if delivery.GetEndpointId() != endpoint.ID ||
		delivery.GetEndpointKind() != "im" ||
		delivery.GetExternalMessageId() != "" ||
		delivery.GetStatus() != daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING {
		t.Fatalf("delivery = %+v, want pending routed IM delivery without source message id", delivery)
	}
}

func TestAgentControlAndDirectMessageEmitDaemonEvents(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()
	ctx := context.Background()

	_, err := client.RegisterComputer(ctx, &daemonv1.RegisterComputerRequest{
		Info: &daemonv1.ComputerInfo{
			DaemonId:   "daemon-1",
			ComputerId: "computer-1",
			Hostname:   "test-host",
			Status:     daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE,
		},
		Inventory: &daemonv1.ComputerInventory{
			Agents: []*daemonv1.AgentProfile{
				{
					AgentId:          "agent-1",
					ComputerId:       "computer-1",
					RuntimeProfileId: "profile-agent-1",
					Enabled:          true,
				},
			},
		},
		RequestId:      "register-control-1",
		IdempotencyKey: "register-control-1",
	})
	if err != nil {
		t.Fatalf("RegisterComputer() error = %v", err)
	}

	control, err := client.ControlAgent(ctx, &daemonv1.ControlAgentRequest{
		AgentId:        "agent-1",
		Action:         daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_TERMINATE,
		Reason:         "test terminate",
		RequestId:      "control-1",
		IdempotencyKey: "control-1",
	})
	if err != nil {
		t.Fatalf("ControlAgent() error = %v", err)
	}
	if !control.GetAccepted() ||
		control.GetOperation().GetComputerId() != "computer-1" ||
		control.GetOperation().GetState() != daemonv1.AgentControlState_AGENT_CONTROL_STATE_QUEUED {
		t.Fatalf("ControlAgent() = %+v, want queued operation for computer-1", control)
	}

	direct, err := client.SendAgentDirectMessage(ctx, &daemonv1.SendAgentDirectMessageRequest{
		AgentId:        "agent-1",
		Content:        "hello agent",
		RequestId:      "direct-1",
		IdempotencyKey: "direct-1",
		Sender:         &daemonv1.Actor{ActorKind: daemonv1.ActorKind_ACTOR_KIND_HUMAN, DisplayName: "Tester"},
	})
	if err != nil {
		t.Fatalf("SendAgentDirectMessage() error = %v", err)
	}
	if !direct.GetAccepted() || direct.GetMessage().GetTarget() != "dm:agent-1" {
		t.Fatalf("SendAgentDirectMessage() = %+v, want dm:agent-1", direct)
	}

	streamCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	stream, err := client.SubscribeServerEvents(streamCtx, &daemonv1.SubscribeServerEventsRequest{
		DaemonId:   "daemon-1",
		ComputerId: "computer-1",
		AgentIds:   []string{"agent-1"},
		Kinds: []daemonv1.ServerEventKind{
			daemonv1.ServerEventKind_SERVER_EVENT_KIND_AGENT_CONTROL,
			daemonv1.ServerEventKind_SERVER_EVENT_KIND_MESSAGE,
		},
		RequestId: "stream-control-1",
	})
	if err != nil {
		t.Fatalf("SubscribeServerEvents() error = %v", err)
	}
	seenControl := false
	seenMessage := false
	for !seenControl || !seenMessage {
		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("stream Recv() error = %v", err)
		}
		switch payload := resp.GetEvent().GetPayload().(type) {
		case *daemonv1.ServerEvent_AgentControl:
			seenControl = payload.AgentControl.GetOperationId() == control.GetOperation().GetOperationId()
		case *daemonv1.ServerEvent_Message:
			seenMessage = payload.Message.GetMessageId() == direct.GetMessage().GetMessageId()
		}
	}
}

func assertComputerAndAgentStatus(t *testing.T, srv *Server, computerStatus daemonv1.ComputerStatus, agentPresence daemonv1.AgentPresence, agentHealth daemonv1.AgentHealth) {
	t.Helper()
	inventories := srv.ListComputerInventories(10)
	if len(inventories) != 1 {
		t.Fatalf("ListComputerInventories() returned %d items, want 1", len(inventories))
	}
	if inventories[0].Info.GetStatus() != computerStatus {
		t.Fatalf("computer status = %v, want %v", inventories[0].Info.GetStatus(), computerStatus)
	}
	statuses, err := srv.ListAgentStatuses(context.Background(), &daemonv1.ListAgentStatusesRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("ListAgentStatuses() error = %v", err)
	}
	if len(statuses.GetStatuses()) != 1 {
		t.Fatalf("ListAgentStatuses() returned %d items, want 1", len(statuses.GetStatuses()))
	}
	got := statuses.GetStatuses()[0]
	if got.GetPresence() != agentPresence || got.GetHealth() != agentHealth {
		t.Fatalf("agent status = presence:%v health:%v summary:%q, want presence:%v health:%v", got.GetPresence(), got.GetHealth(), got.GetSummary(), agentPresence, agentHealth)
	}
}

func setComputerLastHeartbeat(t *testing.T, srv *Server, computerID string, lastHeartbeat int64) {
	t.Helper()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	computer := srv.computers[computerID]
	if computer == nil {
		t.Fatalf("computer %q not found", computerID)
	}
	computer.lastHeartbeat = lastHeartbeat
}

func findEvent(events []*daemonv1.CollaborationEvent, kind daemonv1.CollaborationEventKind, operation daemonv1.EventOperation) *daemonv1.CollaborationEvent {
	for _, event := range events {
		if event.GetKind() == kind && event.GetOperation() == operation {
			return event
		}
	}
	return nil
}

func TestDurableIdempotencyAndEventsSurviveServerRestart(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "daemonrpc_restart")
	req := &daemonv1.LogActivityRequest{
		Target:         "#general",
		AgentId:        "agent-1",
		Kind:           "test_run",
		Summary:        "durable replay",
		RequestId:      "activity-restart-1",
		IdempotencyKey: "activity-restart-1",
	}

	first, err := New(store, "srv_test").LogActivity(ctx, req)
	if err != nil {
		t.Fatalf("first LogActivity() error = %v", err)
	}
	replayed, err := New(store, "srv_test").LogActivity(ctx, req)
	if err != nil {
		t.Fatalf("replayed LogActivity() error = %v", err)
	}
	if replayed.GetActivity().GetActivityId() != first.GetActivity().GetActivityId() {
		t.Fatalf("replayed activity id = %q, want %q", replayed.GetActivity().GetActivityId(), first.GetActivity().GetActivityId())
	}

	events, err := New(store, "srv_test").ListEventsSince(ctx, &daemonv1.ListEventsSinceRequest{
		Cursor: &daemonv1.EventCursor{Sequence: 0, Target: "#general"},
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListEventsSince() error = %v", err)
	}
	if len(events.GetEvents()) != 1 || events.GetEvents()[0].GetActivity().GetActivityId() != first.GetActivity().GetActivityId() {
		t.Fatalf("events = %+v, want one durable activity event", events.GetEvents())
	}
}

func newTestClient(t *testing.T) (daemonv1.DaemonControlServiceClient, func()) {
	t.Helper()
	store := newTestStore(t, "daemonrpc_test")
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	daemonv1.RegisterDaemonControlServiceServer(grpcServer, New(store, "srv_test"))
	go func() {
		_ = grpcServer.Serve(listener)
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = listener.Close()
	}
	return daemonv1.NewDaemonControlServiceClient(conn), cleanup
}

func newTestStore(t *testing.T, prefix string) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID(prefix)+"?mode=memory&cache=shared&_fk=1")
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

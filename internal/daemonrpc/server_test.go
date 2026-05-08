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

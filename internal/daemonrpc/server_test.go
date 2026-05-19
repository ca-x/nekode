package daemonrpc

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/gen/go/nekode/daemon/v1/daemonv1connect"
	"github.com/ca-x/nekode/internal/storage"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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

func TestGetLaunchPromptSnapshotBuildsRedactedManifest(t *testing.T) {
	store := newTestStore(t, "daemonrpc_prompt")
	srv := New(store, "srv_prompt")
	ctx := context.Background()
	_, err := srv.RegisterComputer(ctx, &daemonv1.RegisterComputerRequest{
		Info: &daemonv1.ComputerInfo{
			DaemonId:   "daemon-1",
			ComputerId: "computer-1",
			Status:     daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE,
		},
		Inventory: &daemonv1.ComputerInventory{
			Agents: []*daemonv1.AgentProfile{
				{
					AgentId:          "agent-1",
					Name:             "agent-one",
					DisplayName:      "Agent One",
					ComputerId:       "computer-1",
					RuntimeProfileId: "profile-agent-1",
					RuntimeKind:      "codex",
					Provider:         "openai",
					Model:            "gpt-test",
					Capabilities: []*daemonv1.Capability{
						{Name: "code_execution", Enabled: true},
					},
				},
			},
			RuntimeProfiles: []*daemonv1.RuntimeProfile{
				{
					RuntimeProfileId: "profile-agent-1",
					Kind:             "codex",
					Provider:         "openai",
					Model:            "gpt-test",
					AdapterConfigJson: `{
						"selectedOptions":{
							"display_name":"Agent One",
							"system_message":"Prefer concise updates.",
							"api_token":"secret-token-value"
						}
					}`,
					Capabilities: []*daemonv1.Capability{
						{Name: "file_write", Enabled: true},
					},
					Env: []*daemonv1.EnvVar{
						{Name: "OPENAI_API_KEY", Redacted: true},
					},
				},
			},
		},
		RequestId:      "register-prompt-1",
		IdempotencyKey: "register-prompt-1",
	})
	if err != nil {
		t.Fatalf("RegisterComputer() error = %v", err)
	}
	msg, err := store.CreateMessage(ctx, storage.Message{
		ID:                "msg-1",
		Target:            "#LightOsClub",
		ThreadID:          "thread-1",
		Role:              "user",
		Content:           "Please finish prompt hardening.",
		SenderDisplayName: "xczyt",
		SenderKind:        "human",
		MetadataJSON:      `{"api_token":"secret-token-value","safe":"ok"}`,
	})
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	taskModel, err := store.CreateTask(ctx, storage.Task{
		ID:          "task-188",
		Target:      "#LightOsClub",
		Summary:     "Prompt hardening",
		Description: "Build launch prompt snapshot.",
		State:       "in_progress",
		AssigneeID:  "agent-1",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	srv.mu.Lock()
	srv.runs["run-1"] = &daemonv1.Run{
		RunId:            "run-1",
		TaskId:           taskModel.ID,
		Target:           msg.Target,
		AgentId:          "agent-1",
		ComputerId:       "computer-1",
		RuntimeProfileId: "profile-agent-1",
		InputMessageId:   msg.ID,
		Summary:          "current objective",
	}
	srv.mu.Unlock()

	resp, err := srv.GetLaunchPromptSnapshot(ctx, &daemonv1.GetLaunchPromptSnapshotRequest{
		RunId:      "run-1",
		AgentId:    "agent-1",
		ComputerId: "computer-1",
	})
	if err != nil {
		t.Fatalf("GetLaunchPromptSnapshot() error = %v", err)
	}
	snapshot := resp.GetSnapshot()
	if snapshot.GetSnapshotId() == "" || snapshot.GetContentHash() == "" {
		t.Fatalf("snapshot = %+v, want id and content hash", snapshot)
	}
	content := snapshot.GetContent()
	for _, want := range []string{
		"Agent One",
		"Prefer concise updates.",
		"Prompt hardening",
		"Please finish prompt hardening.",
		"reply_target_hint: #LightOsClub:thread-1",
		"use that exact target for replies",
		"code_execution",
		"file_write",
		"Do not mention yourself to ask whether you have started",
		"Do not send empty coordination/status messages",
		"execution_verification:",
		"acceptance criteria as default-failing",
		"server-visible task, message, status, or activity surfaces",
		"Do not stop at analysis or half-finished implementation",
		"report the evidence or the exact remaining gap",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("snapshot content missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "secret-token-value") {
		t.Fatalf("snapshot content leaked secret:\n%s", content)
	}
	if !strings.Contains(content, "<redacted>") || !strings.Contains(snapshot.GetRedactionSummary(), "redacted") {
		t.Fatalf("snapshot redaction summary/content = %q / %q, want redaction", snapshot.GetRedactionSummary(), content)
	}
	if len(snapshot.GetSections()) < 6 {
		t.Fatalf("snapshot sections = %+v, want layered manifest", snapshot.GetSections())
	}
}

func TestReplyTargetHint(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		threadID string
		want     string
	}{
		{name: "channel", target: "#general", want: "#general"},
		{name: "channel thread", target: "#general", threadID: "abc123", want: "#general:abc123"},
		{name: "dm", target: "dm:@alice", want: "dm:@alice"},
		{name: "dm thread", target: "dm:@alice", threadID: "abc123", want: "dm:@alice:abc123"},
		{name: "trim", target: " #ops ", threadID: " thread-1 ", want: "#ops:thread-1"},
		{name: "empty target", threadID: "thread-1", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := replyTargetHint(tt.target, tt.threadID); got != tt.want {
				t.Fatalf("replyTargetHint(%q, %q) = %q, want %q", tt.target, tt.threadID, got, tt.want)
			}
		})
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

func TestTaskClaimCreatesRunAndTerminalStatusUpdatesTask(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := client.RegisterComputer(ctx, &daemonv1.RegisterComputerRequest{
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
					RuntimeKind:      "codex",
					Enabled:          true,
				},
				{
					AgentId:          "agent-2",
					ComputerId:       "computer-1",
					RuntimeProfileId: "profile-agent-2",
					RuntimeKind:      "claude",
					Enabled:          true,
				},
			},
		},
		RequestId:      "register-task-run-1",
		IdempotencyKey: "register-task-run-1",
	}); err != nil {
		t.Fatalf("RegisterComputer() error = %v", err)
	}

	task, err := client.CreateCollaborationTask(ctx, &daemonv1.CreateCollaborationTaskRequest{
		Target:         "#general",
		Summary:        "execute claimed task",
		RequestId:      "task-run-1",
		IdempotencyKey: "task-run-1",
	})
	if err != nil {
		t.Fatalf("CreateCollaborationTask() error = %v", err)
	}
	claim, err := client.ClaimCollaborationTask(ctx, &daemonv1.ClaimCollaborationTaskRequest{
		TaskId:         task.GetTask().GetTaskId(),
		AgentId:        "agent-1",
		RequestId:      "claim-run-1",
		IdempotencyKey: "claim-run-1",
	})
	if err != nil {
		t.Fatalf("ClaimCollaborationTask() error = %v", err)
	}
	runID := claim.GetTask().GetCurrentRunId()
	if !claim.GetAccepted() ||
		claim.GetTask().GetAssigneeId() != "agent-1" ||
		claim.GetTask().GetState() != daemonv1.TaskState_TASK_STATE_IN_PROGRESS ||
		runID == "" {
		t.Fatalf("ClaimCollaborationTask() = %+v, want accepted in-progress task with current run", claim)
	}

	assigned, err := client.FetchAssignedRuns(ctx, &daemonv1.FetchAssignedRunsRequest{
		ComputerId: "computer-1",
		AgentIds:   []string{"agent-1"},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("FetchAssignedRuns() error = %v", err)
	}
	if len(assigned.GetRuns()) != 1 ||
		assigned.GetRuns()[0].GetRunId() != runID ||
		assigned.GetRuns()[0].GetTaskId() != task.GetTask().GetTaskId() ||
		assigned.GetRuns()[0].GetRuntimeProfileId() != "profile-agent-1" ||
		assigned.GetRuns()[0].GetState() != daemonv1.RunState_RUN_STATE_QUEUED {
		t.Fatalf("FetchAssignedRuns() = %+v, want queued task run %q", assigned.GetRuns(), runID)
	}
	conflict, err := client.ClaimCollaborationTask(ctx, &daemonv1.ClaimCollaborationTaskRequest{
		TaskId:         task.GetTask().GetTaskId(),
		AgentId:        "agent-2",
		RequestId:      "claim-run-2",
		IdempotencyKey: "claim-run-2",
	})
	if err != nil {
		t.Fatalf("ClaimCollaborationTask(conflict) error = %v", err)
	}
	if conflict.GetAccepted() || conflict.GetCurrentAssigneeId() != "agent-1" {
		t.Fatalf("ClaimCollaborationTask(conflict) = %+v, want agent-1 conflict", conflict)
	}
	runsAfterConflict, err := client.ListRuns(ctx, &daemonv1.ListRunsRequest{TaskId: task.GetTask().GetTaskId(), Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns(after conflict) error = %v", err)
	}
	if len(runsAfterConflict.GetRuns()) != 1 || runsAfterConflict.GetRuns()[0].GetRunId() != runID {
		t.Fatalf("ListRuns(after conflict) = %+v, want single original run", runsAfterConflict.GetRuns())
	}

	if _, err := client.UpdateRunStatus(ctx, &daemonv1.UpdateRunStatusRequest{
		RunId:          runID,
		AgentId:        "agent-1",
		State:          daemonv1.RunState_RUN_STATE_COMPLETED,
		Summary:        "implementation ready",
		RequestId:      "run-complete-1",
		IdempotencyKey: "run-complete-1",
	}); err != nil {
		t.Fatalf("UpdateRunStatus(completed) error = %v", err)
	}
	completedTask, err := client.GetTask(ctx, &daemonv1.GetTaskRequest{TaskId: task.GetTask().GetTaskId()})
	if err != nil {
		t.Fatalf("GetTask(completed) error = %v", err)
	}
	if completedTask.GetTask().GetState() != daemonv1.TaskState_TASK_STATE_IN_REVIEW ||
		completedTask.GetTask().GetAssigneeId() != "agent-1" {
		t.Fatalf("completed task = %+v, want in_review with assignee", completedTask.GetTask())
	}
	completedRuns, err := client.ListRuns(ctx, &daemonv1.ListRunsRequest{TaskId: task.GetTask().GetTaskId(), Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns(completed) error = %v", err)
	}
	if len(completedRuns.GetRuns()) != 1 ||
		completedRuns.GetRuns()[0].GetState() != daemonv1.RunState_RUN_STATE_COMPLETED {
		t.Fatalf("ListRuns(completed) = %+v, want completed run", completedRuns.GetRuns())
	}
}

func TestTaskClaimAndRunStatusPersistAttemptOutput(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "daemonrpc_attempt")
	srv := New(store, "srv_test")

	task, err := srv.CreateCollaborationTask(ctx, &daemonv1.CreateCollaborationTaskRequest{
		Target:         "#general",
		Summary:        "persist attempt output",
		RequestId:      "task-attempt-1",
		IdempotencyKey: "task-attempt-1",
	})
	if err != nil {
		t.Fatalf("CreateCollaborationTask() error = %v", err)
	}
	claim, err := srv.ClaimCollaborationTask(ctx, &daemonv1.ClaimCollaborationTaskRequest{
		TaskId:         task.GetTask().GetTaskId(),
		AgentId:        "agent-1",
		RequestId:      "claim-attempt-1",
		IdempotencyKey: "claim-attempt-1",
	})
	if err != nil {
		t.Fatalf("ClaimCollaborationTask() error = %v", err)
	}
	runID := claim.GetTask().GetCurrentRunId()
	if runID == "" {
		t.Fatalf("ClaimCollaborationTask() = %+v, want current run", claim)
	}
	attempts, err := store.ListTaskAttempts(ctx, task.GetTask().GetTaskId())
	if err != nil {
		t.Fatalf("ListTaskAttempts(claimed) error = %v", err)
	}
	if len(attempts) != 1 ||
		attempts[0].RunID != runID ||
		attempts[0].AgentID != "agent-1" ||
		attempts[0].Status != storage.TaskAttemptStatusClaimed {
		t.Fatalf("claimed attempts = %+v, want one claimed attempt for run", attempts)
	}

	if _, err := srv.UpdateRunStatus(ctx, &daemonv1.UpdateRunStatusRequest{
		RunId:              runID,
		AgentId:            "agent-1",
		State:              daemonv1.RunState_RUN_STATE_COMPLETED,
		Summary:            "done",
		ResultJson:         `{"result":"ok"}`,
		ResultDigest:       "sha256:result",
		ResultSignature:    "ed25519:signature",
		SignaturePublicKey: "ed25519:public",
		RequestId:          "run-attempt-complete-1",
		IdempotencyKey:     "run-attempt-complete-1",
	}); err != nil {
		t.Fatalf("UpdateRunStatus(completed) error = %v", err)
	}
	attempts, err = store.ListTaskAttempts(ctx, task.GetTask().GetTaskId())
	if err != nil {
		t.Fatalf("ListTaskAttempts(completed) error = %v", err)
	}
	if len(attempts) != 1 ||
		attempts[0].Status != storage.TaskAttemptStatusCompleted ||
		attempts[0].OutputJSON != `{"result":"ok"}` ||
		attempts[0].OutputDigest != "sha256:result" ||
		attempts[0].OutputSignature != "ed25519:signature" ||
		attempts[0].SignaturePublicKey != "ed25519:public" ||
		attempts[0].CompletedUnix == 0 {
		t.Fatalf("completed attempts = %+v, want signed output persisted", attempts)
	}
}

func TestFailedTaskRunBlocksTaskWithRedactedReason(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()
	ctx := context.Background()

	task, err := client.CreateCollaborationTask(ctx, &daemonv1.CreateCollaborationTaskRequest{
		Target:         "#general",
		Summary:        "handle failed runtime",
		RequestId:      "task-run-failed-1",
		IdempotencyKey: "task-run-failed-1",
	})
	if err != nil {
		t.Fatalf("CreateCollaborationTask() error = %v", err)
	}
	claim, err := client.ClaimCollaborationTask(ctx, &daemonv1.ClaimCollaborationTaskRequest{
		TaskId:         task.GetTask().GetTaskId(),
		AgentId:        "agent-1",
		RequestId:      "claim-run-failed-1",
		IdempotencyKey: "claim-run-failed-1",
	})
	if err != nil {
		t.Fatalf("ClaimCollaborationTask() error = %v", err)
	}
	runID := claim.GetTask().GetCurrentRunId()
	if runID == "" {
		t.Fatalf("ClaimCollaborationTask() = %+v, want current run", claim)
	}
	if _, err := client.UpdateRunStatus(ctx, &daemonv1.UpdateRunStatusRequest{
		RunId:          runID,
		AgentId:        "agent-1",
		State:          daemonv1.RunState_RUN_STATE_FAILED,
		Summary:        "runtime failed with token=secret-token-value",
		Error:          "exit 2 authorization: Bearer secret-token-value",
		BlockedReason:  "argv failed password=super-secret",
		RequestId:      "run-failed-1",
		IdempotencyKey: "run-failed-1",
	}); err != nil {
		t.Fatalf("UpdateRunStatus(failed) error = %v", err)
	}
	failedTask, err := client.GetTask(ctx, &daemonv1.GetTaskRequest{TaskId: task.GetTask().GetTaskId()})
	if err != nil {
		t.Fatalf("GetTask(failed) error = %v", err)
	}
	if failedTask.GetTask().GetState() != daemonv1.TaskState_TASK_STATE_BLOCKED {
		t.Fatalf("failed task state = %v, want blocked", failedTask.GetTask().GetState())
	}
	reason := failedTask.GetTask().GetBlockedReason()
	if !strings.Contains(reason, runID) || !strings.Contains(reason, "agent agent-1") {
		t.Fatalf("blocked reason = %q, want run and agent context", reason)
	}
	if strings.Contains(reason, "secret-token-value") || strings.Contains(reason, "super-secret") {
		t.Fatalf("blocked reason leaked secret: %q", reason)
	}
	runs, err := client.ListRuns(ctx, &daemonv1.ListRunsRequest{TaskId: task.GetTask().GetTaskId(), Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns(failed) error = %v", err)
	}
	if len(runs.GetRuns()) != 1 ||
		runs.GetRuns()[0].GetState() != daemonv1.RunState_RUN_STATE_FAILED ||
		strings.Contains(runs.GetRuns()[0].GetError(), "secret-token-value") {
		t.Fatalf("ListRuns(failed) = %+v, want failed run with redacted error", runs.GetRuns())
	}
}

func TestReviewerClaimDoesNotStartExecutionRun(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()
	ctx := context.Background()

	task, err := client.CreateCollaborationTask(ctx, &daemonv1.CreateCollaborationTaskRequest{
		Target:         "#general",
		Summary:        "review only",
		RequestId:      "task-review-claim-1",
		IdempotencyKey: "task-review-claim-1",
	})
	if err != nil {
		t.Fatalf("CreateCollaborationTask() error = %v", err)
	}
	claim, err := client.ClaimCollaborationTask(ctx, &daemonv1.ClaimCollaborationTaskRequest{
		TaskId:         task.GetTask().GetTaskId(),
		AgentId:        "agent-reviewer",
		ClaimMode:      daemonv1.TaskClaimMode_TASK_CLAIM_MODE_REVIEWER,
		RequestId:      "claim-review-only-1",
		IdempotencyKey: "claim-review-only-1",
	})
	if err != nil {
		t.Fatalf("ClaimCollaborationTask(reviewer) error = %v", err)
	}
	if !claim.GetAccepted() ||
		claim.GetTask().GetCurrentRunId() != "" ||
		claim.GetTask().GetState() != daemonv1.TaskState_TASK_STATE_TODO {
		t.Fatalf("ClaimCollaborationTask(reviewer) = %+v, want claim-only semantics without run", claim)
	}
	runs, err := client.ListRuns(ctx, &daemonv1.ListRunsRequest{TaskId: task.GetTask().GetTaskId(), Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns(reviewer) error = %v", err)
	}
	if len(runs.GetRuns()) != 0 {
		t.Fatalf("ListRuns(reviewer) = %+v, want no execution run", runs.GetRuns())
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

func TestOutboundDeliverySuppressesNonVerboseIMStatusReply(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "daemonrpc_outbound_verbose_filter")
	srv := New(store, "srv_test")

	endpoint, err := store.CreateInteractionEndpoint(ctx, storage.InteractionEndpoint{
		Kind:            "im",
		Provider:        "wecom",
		DisplayName:     "WeCom Ops",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "webhook_signature",
		ConfigJSON:      `{"require_subscription":true}`,
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		Role:              "user",
		Content:           "incident update",
		SenderDisplayName: "WeCom User",
		SenderKind:        "endpoint",
		SourceEndpointID:  endpoint.ID,
		ExternalMessageID: "wecom-msg-1",
		MetadataJSON:      `{"im":{"provider":"wecom","conversation":{"external_id":"chat-ops","external_thread_id":"topic-1","display_name":"Ops"},"sender":{"external_id":"user-1","display_name":"WeCom User"}}}`,
		RequestID:         "incoming-wecom-msg-1",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}

	noSubscriptionReply, err := srv.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           "#ops",
		Content:          "reply before authorization",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "reply-before-subscription",
		IdempotencyKey:   "reply-before-subscription",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-1",
			DisplayName: "Agent One",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage(no subscription reply) error = %v", err)
	}
	listed, err := srv.ListOutboundDeliveries(ctx, &daemonv1.ListOutboundDeliveriesRequest{
		Target:    "#ops",
		MessageId: noSubscriptionReply.GetMessage().GetMessageId(),
		Statuses:  []daemonv1.OutboundDeliveryStatus{daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING},
	})
	if err != nil {
		t.Fatalf("ListOutboundDeliveries(no subscription) error = %v", err)
	}
	if len(listed.GetDeliveries()) != 0 {
		t.Fatalf("no-subscription deliveries = %+v, want suppressed while chat is unauthorized", listed.GetDeliveries())
	}

	if _, err := store.UpsertIMChatSubscription(ctx, storage.IMChatSubscription{
		EndpointID:       endpoint.ID,
		Provider:         endpoint.Provider,
		ConversationID:   "chat-ops",
		ExternalThreadID: "topic-1",
		ChatTitle:        "Ops",
		Target:           "#ops",
		ThreadID:         "im-chat-ops",
		Subscribed:       true,
		Verbose:          false,
	}); err != nil {
		t.Fatalf("UpsertIMChatSubscription() error = %v", err)
	}

	statusReply, err := srv.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           "#ops",
		Content:          "working...",
		ReplyToMessageId: inbound.ID,
		Kind:             daemonv1.MessageKind_MESSAGE_KIND_STATUS,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "reply-status-1",
		IdempotencyKey:   "reply-status-1",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-1",
			DisplayName: "Agent One",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage(status reply) error = %v", err)
	}
	listed, err = srv.ListOutboundDeliveries(ctx, &daemonv1.ListOutboundDeliveriesRequest{
		Target:    "#ops",
		MessageId: statusReply.GetMessage().GetMessageId(),
		Statuses:  []daemonv1.OutboundDeliveryStatus{daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING},
	})
	if err != nil {
		t.Fatalf("ListOutboundDeliveries(status) error = %v", err)
	}
	if len(listed.GetDeliveries()) != 0 {
		t.Fatalf("status deliveries = %+v, want suppressed while verbose is off", listed.GetDeliveries())
	}

	verbose := true
	subscription, err := store.GetIMChatSubscription(ctx, endpoint.ID, "chat-ops", "topic-1")
	if err != nil {
		t.Fatalf("GetIMChatSubscription() error = %v", err)
	}
	if _, err := store.UpdateIMChatSubscription(ctx, subscription.ID, storage.IMChatSubscriptionPatch{Verbose: &verbose}); err != nil {
		t.Fatalf("UpdateIMChatSubscription(verbose) error = %v", err)
	}
	verboseReply, err := srv.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           "#ops",
		Content:          "still working...",
		ReplyToMessageId: inbound.ID,
		Kind:             daemonv1.MessageKind_MESSAGE_KIND_STATUS,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		RequestId:        "reply-status-2",
		IdempotencyKey:   "reply-status-2",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-1",
			DisplayName: "Agent One",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage(verbose status reply) error = %v", err)
	}
	listed, err = srv.ListOutboundDeliveries(ctx, &daemonv1.ListOutboundDeliveriesRequest{
		Target:    "#ops",
		MessageId: verboseReply.GetMessage().GetMessageId(),
		Statuses:  []daemonv1.OutboundDeliveryStatus{daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING},
	})
	if err != nil {
		t.Fatalf("ListOutboundDeliveries(verbose status) error = %v", err)
	}
	if len(listed.GetDeliveries()) != 1 {
		t.Fatalf("verbose status deliveries = %+v, want one delivery", listed.GetDeliveries())
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

func TestOutboundDeliveryDedupesSourceEndpointRoutes(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "daemonrpc_outbound_dedupe")
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
	if _, err := store.CreateNotificationRoute(ctx, storage.NotificationRoute{
		Target:     "#ops",
		EndpointID: endpoint.ID,
		EventKind:  "message",
		Preference: "all",
		Enabled:    true,
		ConfigJSON: "{}",
	}); err != nil {
		t.Fatalf("CreateNotificationRoute() error = %v", err)
	}
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		Role:              "user",
		Content:           "from im",
		SenderDisplayName: "Feishu User",
		SenderKind:        "endpoint",
		SourceEndpointID:  endpoint.ID,
		ExternalMessageID: "feishu-msg-1",
		MetadataJSON:      "{}",
		RequestID:         "incoming-feishu-msg-1",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}

	reply, err := srv.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           "#ops",
		Content:          "agent reply",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_ALL_BOUND_ENDPOINTS,
		RequestId:        "reply-dedupe-1",
		IdempotencyKey:   "reply-dedupe-1",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-1",
			DisplayName: "Agent One",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage(reply) error = %v", err)
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
		t.Fatalf("ListOutboundDeliveries() returned %d deliveries, want source route only once", len(listed.GetDeliveries()))
	}
	delivery := listed.GetDeliveries()[0]
	if delivery.GetEndpointId() != endpoint.ID || delivery.GetExternalMessageId() != "feishu-msg-1" {
		t.Fatalf("delivery = %+v, want deduped source delivery with external message id", delivery)
	}
}

func TestOutboundDeliverySelectedEndpointsUsesRoutesWithoutSource(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "daemonrpc_outbound_selected")
	srv := New(store, "srv_test")

	sourceEndpoint, err := store.CreateInteractionEndpoint(ctx, storage.InteractionEndpoint{
		Kind:            "im",
		Provider:        "feishu",
		DisplayName:     "Feishu Source",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "bearer",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint(source) error = %v", err)
	}
	routeEndpoint, err := store.CreateInteractionEndpoint(ctx, storage.InteractionEndpoint{
		Kind:            "im",
		Provider:        "telegram",
		DisplayName:     "Telegram Route",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "bearer",
		ConfigJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint(route) error = %v", err)
	}
	if _, err := store.CreateNotificationRoute(ctx, storage.NotificationRoute{
		Target:     "#ops",
		EndpointID: routeEndpoint.ID,
		EventKind:  "message",
		Preference: "all",
		Enabled:    true,
		ConfigJSON: "{}",
	}); err != nil {
		t.Fatalf("CreateNotificationRoute() error = %v", err)
	}
	inbound, err := store.CreateMessage(ctx, storage.Message{
		Target:            "#ops",
		Role:              "user",
		Content:           "from im",
		SenderDisplayName: "Feishu User",
		SenderKind:        "endpoint",
		SourceEndpointID:  sourceEndpoint.ID,
		ExternalMessageID: "feishu-msg-2",
		MetadataJSON:      "{}",
		RequestID:         "incoming-feishu-msg-2",
	})
	if err != nil {
		t.Fatalf("CreateMessage(inbound) error = %v", err)
	}

	reply, err := srv.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           "#ops",
		Content:          "selected endpoint reply",
		ReplyToMessageId: inbound.ID,
		OutboundPolicy:   daemonv1.OutboundPolicy_OUTBOUND_POLICY_SELECTED_ENDPOINTS,
		RequestId:        "reply-selected-1",
		IdempotencyKey:   "reply-selected-1",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-1",
			DisplayName: "Agent One",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage(reply) error = %v", err)
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
		t.Fatalf("ListOutboundDeliveries() returned %d deliveries, want routed delivery only", len(listed.GetDeliveries()))
	}
	delivery := listed.GetDeliveries()[0]
	if delivery.GetEndpointId() != routeEndpoint.ID || delivery.GetExternalMessageId() != "" {
		t.Fatalf("delivery = %+v, want selected routed delivery without source external id", delivery)
	}
}

func TestOutboundDeliverySkipsDisabledRouteEndpoints(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "daemonrpc_outbound_disabled_route")
	srv := New(store, "srv_test")

	endpoint, err := store.CreateInteractionEndpoint(ctx, storage.InteractionEndpoint{
		Kind:            "im",
		Provider:        "telegram",
		DisplayName:     "Telegram Disabled",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: false,
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
		ConfigJSON: "{}",
	}); err != nil {
		t.Fatalf("CreateNotificationRoute() error = %v", err)
	}

	sent, err := srv.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:         "#ops",
		Content:        "route disabled",
		OutboundPolicy: daemonv1.OutboundPolicy_OUTBOUND_POLICY_ALL_BOUND_ENDPOINTS,
		RequestId:      "disabled-route-1",
		IdempotencyKey: "disabled-route-1",
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     "agent-1",
			DisplayName: "Agent One",
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
	if len(listed.GetDeliveries()) != 0 {
		t.Fatalf("ListOutboundDeliveries() = %+v, want disabled route endpoint skipped", listed.GetDeliveries())
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
	controlActivity, err := client.ListActivity(ctx, &daemonv1.ListActivityRequest{Target: "dm:agent-1", AgentId: "agent-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListActivity(control) error = %v", err)
	}
	if len(controlActivity.GetActivities()) != 1 ||
		controlActivity.GetActivities()[0].GetKind() != "agent_control_requested" ||
		controlActivity.GetActivities()[0].GetStepId() != control.GetOperation().GetOperationId() {
		t.Fatalf("ListActivity(control) = %+v, want persisted control activity", controlActivity.GetActivities())
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

func TestAgentControlActivityPersistsAcrossServerRestart(t *testing.T) {
	store := newTestStore(t, "daemonrpc_control_activity")
	ctx := context.Background()
	first := New(store, "srv_test")
	if _, err := first.RegisterComputer(ctx, &daemonv1.RegisterComputerRequest{
		Info: &daemonv1.ComputerInfo{
			DaemonId:   "daemon-1",
			ComputerId: "computer-1",
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
		RequestId:      "register-control-persist-1",
		IdempotencyKey: "register-control-persist-1",
	}); err != nil {
		t.Fatalf("RegisterComputer() error = %v", err)
	}
	control, err := first.ControlAgent(ctx, &daemonv1.ControlAgentRequest{
		AgentId:        "agent-1",
		Action:         daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART_FULL_RESET,
		Reason:         "operator reset token=secret-token",
		RequestId:      "control-persist-1",
		IdempotencyKey: "control-persist-1",
	})
	if err != nil {
		t.Fatalf("ControlAgent() error = %v", err)
	}
	afterRestart := New(store, "srv_test")
	activity, err := afterRestart.ListActivity(ctx, &daemonv1.ListActivityRequest{AgentId: "agent-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListActivity(after restart) error = %v", err)
	}
	if len(activity.GetActivities()) != 1 ||
		activity.GetActivities()[0].GetKind() != "agent_control_requested" ||
		activity.GetActivities()[0].GetStepId() != control.GetOperation().GetOperationId() ||
		!strings.Contains(activity.GetActivities()[0].GetSummary(), "full reset") ||
		!strings.Contains(activity.GetActivities()[0].GetDetail(), "operator reset token=<redacted>") ||
		strings.Contains(activity.GetActivities()[0].GetDetail(), "secret-token") {
		t.Fatalf("ListActivity(after restart) = %+v, want persisted full-reset control history", activity.GetActivities())
	}
}

func TestDaemonConsumedAgentEventsUpdateServerQueryViews(t *testing.T) {
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
		RequestId:      "register-consume-1",
		IdempotencyKey: "register-consume-1",
	})
	if err != nil {
		t.Fatalf("RegisterComputer() error = %v", err)
	}
	control, err := client.ControlAgent(ctx, &daemonv1.ControlAgentRequest{
		AgentId:        "agent-1",
		Action:         daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART,
		Reason:         "test restart",
		RequestId:      "control-consume-1",
		IdempotencyKey: "control-consume-1",
	})
	if err != nil {
		t.Fatalf("ControlAgent() error = %v", err)
	}
	direct, err := client.SendAgentDirectMessage(ctx, &daemonv1.SendAgentDirectMessageRequest{
		AgentId:        "agent-1",
		Content:        "please report",
		RequestId:      "direct-consume-1",
		IdempotencyKey: "direct-consume-1",
		Sender:         &daemonv1.Actor{ActorKind: daemonv1.ActorKind_ACTOR_KIND_HUMAN, DisplayName: "Tester"},
	})
	if err != nil {
		t.Fatalf("SendAgentDirectMessage() error = %v", err)
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
		RequestId: "stream-consume-1",
	})
	if err != nil {
		t.Fatalf("SubscribeServerEvents() error = %v", err)
	}
	eventIDs := consumeAgentEventIDs(t, stream, control.GetOperation().GetOperationId(), direct.GetMessage().GetMessageId())
	ack, err := client.AcknowledgeServerEvents(ctx, &daemonv1.AcknowledgeServerEventsRequest{
		DaemonId:       "daemon-1",
		ComputerId:     "computer-1",
		EventIds:       eventIDs,
		RequestId:      "ack-consume-1",
		IdempotencyKey: "ack-consume-1",
	})
	if err != nil {
		t.Fatalf("AcknowledgeServerEvents() error = %v", err)
	}
	if !ack.GetAccepted() {
		t.Fatalf("AcknowledgeServerEvents accepted = false")
	}

	runID := "direct-" + direct.GetMessage().GetMessageId()
	if _, err := client.UpdateRunStatus(ctx, &daemonv1.UpdateRunStatusRequest{
		RunId:          runID,
		AgentId:        "agent-1",
		State:          daemonv1.RunState_RUN_STATE_COMPLETED,
		Summary:        "direct message completed",
		RequestId:      "run-consume-1",
		IdempotencyKey: "run-consume-1",
	}); err != nil {
		t.Fatalf("UpdateRunStatus() error = %v", err)
	}
	if _, err := client.LogActivity(ctx, &daemonv1.LogActivityRequest{
		Target:         "dm:agent-1",
		AgentId:        "agent-1",
		Kind:           "direct_message_completed",
		Summary:        "daemon consumed direct message",
		RunId:          runID,
		RequestId:      "activity-consume-1",
		IdempotencyKey: "activity-consume-1",
	}); err != nil {
		t.Fatalf("LogActivity() error = %v", err)
	}
	if _, err := client.UpdateAgentStatus(ctx, &daemonv1.UpdateAgentStatusRequest{
		Status: &daemonv1.AgentStatusSnapshot{
			AgentId:          "agent-1",
			ComputerId:       "computer-1",
			RuntimeProfileId: "profile-agent-1",
			Presence:         daemonv1.AgentPresence_AGENT_PRESENCE_IDLE,
			ActivityState:    daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING,
			Health:           daemonv1.AgentHealth_AGENT_HEALTH_OK,
			Severity:         daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_INFO,
			Summary:          "direct message completed",
			Target:           "dm:agent-1",
			RunId:            runID,
			OperationId:      control.GetOperation().GetOperationId(),
		},
		RequestId:      "status-consume-1",
		IdempotencyKey: "status-consume-1",
	}); err != nil {
		t.Fatalf("UpdateAgentStatus() error = %v", err)
	}

	runs, err := client.ListRuns(ctx, &daemonv1.ListRunsRequest{AgentId: "agent-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs.GetRuns()) != 1 ||
		runs.GetRuns()[0].GetRunId() != runID ||
		runs.GetRuns()[0].GetState() != daemonv1.RunState_RUN_STATE_COMPLETED ||
		runs.GetRuns()[0].GetTarget() != "dm:agent-1" ||
		runs.GetRuns()[0].GetComputerId() != "computer-1" ||
		runs.GetRuns()[0].GetRuntimeProfileId() != "profile-agent-1" {
		t.Fatalf("ListRuns() = %+v, want completed direct-message run %q with route metadata", runs.GetRuns(), runID)
	}
	activity, err := client.ListActivity(ctx, &daemonv1.ListActivityRequest{Target: "dm:agent-1", AgentId: "agent-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListActivity() error = %v", err)
	}
	directActivity := findActivity(activity.GetActivities(), "direct_message_completed")
	if directActivity == nil || directActivity.GetRunId() != runID {
		t.Fatalf("ListActivity() = %+v, want direct message activity for run %q", activity.GetActivities(), runID)
	}
	statuses, err := client.ListAgentStatuses(ctx, &daemonv1.ListAgentStatusesRequest{AgentId: "agent-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListAgentStatuses() error = %v", err)
	}
	if len(statuses.GetStatuses()) != 1 ||
		statuses.GetStatuses()[0].GetActivityState() != daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING ||
		statuses.GetStatuses()[0].GetRunId() != runID ||
		statuses.GetStatuses()[0].GetOperationId() != control.GetOperation().GetOperationId() {
		t.Fatalf("ListAgentStatuses() = %+v, want waiting status for run/control", statuses.GetStatuses())
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

func consumeAgentEventIDs(t *testing.T, stream testServerEventStream, wantOperationID string, wantMessageID string) []string {
	t.Helper()
	seen := map[string]bool{}
	eventIDs := []string{}
	for !seen["control"] || !seen["message"] {
		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("stream Recv() error = %v", err)
		}
		event := resp.GetEvent()
		switch payload := event.GetPayload().(type) {
		case *daemonv1.ServerEvent_AgentControl:
			if payload.AgentControl.GetOperationId() == wantOperationID {
				seen["control"] = true
				eventIDs = append(eventIDs, event.GetEventId())
			}
		case *daemonv1.ServerEvent_Message:
			if payload.Message.GetMessageId() == wantMessageID {
				seen["message"] = true
				eventIDs = append(eventIDs, event.GetEventId())
			}
		}
	}
	return eventIDs
}

func findActivity(activities []*daemonv1.ActivityRecord, kind string) *daemonv1.ActivityRecord {
	for _, activity := range activities {
		if activity.GetKind() == kind {
			return activity
		}
	}
	return nil
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

func newTestClient(t *testing.T) (testDaemonClient, func()) {
	t.Helper()
	store := newTestStore(t, "daemonrpc_test")
	mux := http.NewServeMux()
	path, handler := daemonv1connect.NewDaemonControlServiceHandler(NewConnectHandler(New(store, "srv_test")))
	mux.Handle(path, handler)
	server := httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
	server.EnableHTTP2 = true
	server.Start()
	client := testDaemonClient{client: daemonv1connect.NewDaemonControlServiceClient(testHTTP2Client(), server.URL)}
	cleanup := func() {
		server.Close()
	}
	return client, cleanup
}

type testDaemonClient struct {
	client daemonv1connect.DaemonControlServiceClient
}

type testServerEventStream interface {
	Recv() (*daemonv1.SubscribeServerEventsResponse, error)
}

type testConnectServerEventStream struct {
	stream *connect.ServerStreamForClient[daemonv1.SubscribeServerEventsResponse]
}

func (s testConnectServerEventStream) Recv() (*daemonv1.SubscribeServerEventsResponse, error) {
	if !s.stream.Receive() {
		if err := s.stream.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return s.stream.Msg(), nil
}

func testHTTP2Client() *http.Client {
	return &http.Client{Transport: &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, addr)
		},
	}}
}

func testConnectReq[T any](msg *T) *connect.Request[T] {
	return connect.NewRequest(msg)
}

func testConnectMsg[T any](resp *connect.Response[T], err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c testDaemonClient) RegisterComputer(ctx context.Context, req *daemonv1.RegisterComputerRequest) (*daemonv1.RegisterComputerResponse, error) {
	return testConnectMsg(c.client.RegisterComputer(ctx, testConnectReq(req)))
}

func (c testDaemonClient) HeartbeatComputer(ctx context.Context, req *daemonv1.HeartbeatComputerRequest) (*daemonv1.HeartbeatComputerResponse, error) {
	return testConnectMsg(c.client.HeartbeatComputer(ctx, testConnectReq(req)))
}

func (c testDaemonClient) ListRuntimePresets(ctx context.Context, req *daemonv1.ListRuntimePresetsRequest) (*daemonv1.ListRuntimePresetsResponse, error) {
	return testConnectMsg(c.client.ListRuntimePresets(ctx, testConnectReq(req)))
}

func (c testDaemonClient) ListAgentStatuses(ctx context.Context, req *daemonv1.ListAgentStatusesRequest) (*daemonv1.ListAgentStatusesResponse, error) {
	return testConnectMsg(c.client.ListAgentStatuses(ctx, testConnectReq(req)))
}

func (c testDaemonClient) SubscribeServerEvents(ctx context.Context, req *daemonv1.SubscribeServerEventsRequest) (testServerEventStream, error) {
	stream, err := c.client.SubscribeServerEvents(ctx, testConnectReq(req))
	if err != nil {
		return nil, err
	}
	return testConnectServerEventStream{stream: stream}, nil
}

func (c testDaemonClient) AcknowledgeServerEvents(ctx context.Context, req *daemonv1.AcknowledgeServerEventsRequest) (*daemonv1.AcknowledgeServerEventsResponse, error) {
	return testConnectMsg(c.client.AcknowledgeServerEvents(ctx, testConnectReq(req)))
}

func (c testDaemonClient) SendMessage(ctx context.Context, req *daemonv1.SendMessageRequest) (*daemonv1.SendMessageResponse, error) {
	return testConnectMsg(c.client.SendMessage(ctx, testConnectReq(req)))
}

func (c testDaemonClient) ReadMessages(ctx context.Context, req *daemonv1.ReadMessagesRequest) (*daemonv1.ReadMessagesResponse, error) {
	return testConnectMsg(c.client.ReadMessages(ctx, testConnectReq(req)))
}

func (c testDaemonClient) CreateCollaborationTask(ctx context.Context, req *daemonv1.CreateCollaborationTaskRequest) (*daemonv1.CreateCollaborationTaskResponse, error) {
	return testConnectMsg(c.client.CreateCollaborationTask(ctx, testConnectReq(req)))
}

func (c testDaemonClient) UpdateTask(ctx context.Context, req *daemonv1.UpdateTaskRequest) (*daemonv1.UpdateTaskResponse, error) {
	return testConnectMsg(c.client.UpdateTask(ctx, testConnectReq(req)))
}

func (c testDaemonClient) ListCollaborationTasks(ctx context.Context, req *daemonv1.ListCollaborationTasksRequest) (*daemonv1.ListCollaborationTasksResponse, error) {
	return testConnectMsg(c.client.ListCollaborationTasks(ctx, testConnectReq(req)))
}

func (c testDaemonClient) ClaimCollaborationTask(ctx context.Context, req *daemonv1.ClaimCollaborationTaskRequest) (*daemonv1.ClaimCollaborationTaskResponse, error) {
	return testConnectMsg(c.client.ClaimCollaborationTask(ctx, testConnectReq(req)))
}

func (c testDaemonClient) ListTaskBoard(ctx context.Context, req *daemonv1.ListTaskBoardRequest) (*daemonv1.ListTaskBoardResponse, error) {
	return testConnectMsg(c.client.ListTaskBoard(ctx, testConnectReq(req)))
}

func (c testDaemonClient) LogActivity(ctx context.Context, req *daemonv1.LogActivityRequest) (*daemonv1.LogActivityResponse, error) {
	return testConnectMsg(c.client.LogActivity(ctx, testConnectReq(req)))
}

func (c testDaemonClient) ListEventsSince(ctx context.Context, req *daemonv1.ListEventsSinceRequest) (*daemonv1.ListEventsSinceResponse, error) {
	return testConnectMsg(c.client.ListEventsSince(ctx, testConnectReq(req)))
}

func (c testDaemonClient) FetchAssignedRuns(ctx context.Context, req *daemonv1.FetchAssignedRunsRequest) (*daemonv1.FetchAssignedRunsResponse, error) {
	return testConnectMsg(c.client.FetchAssignedRuns(ctx, testConnectReq(req)))
}

func (c testDaemonClient) ListRuns(ctx context.Context, req *daemonv1.ListRunsRequest) (*daemonv1.ListRunsResponse, error) {
	return testConnectMsg(c.client.ListRuns(ctx, testConnectReq(req)))
}

func (c testDaemonClient) UpdateRunStatus(ctx context.Context, req *daemonv1.UpdateRunStatusRequest) (*daemonv1.UpdateRunStatusResponse, error) {
	return testConnectMsg(c.client.UpdateRunStatus(ctx, testConnectReq(req)))
}

func (c testDaemonClient) GetTask(ctx context.Context, req *daemonv1.GetTaskRequest) (*daemonv1.GetTaskResponse, error) {
	return testConnectMsg(c.client.GetTask(ctx, testConnectReq(req)))
}

func (c testDaemonClient) ControlAgent(ctx context.Context, req *daemonv1.ControlAgentRequest) (*daemonv1.ControlAgentResponse, error) {
	return testConnectMsg(c.client.ControlAgent(ctx, testConnectReq(req)))
}

func (c testDaemonClient) ListActivity(ctx context.Context, req *daemonv1.ListActivityRequest) (*daemonv1.ListActivityResponse, error) {
	return testConnectMsg(c.client.ListActivity(ctx, testConnectReq(req)))
}

func (c testDaemonClient) SendAgentDirectMessage(ctx context.Context, req *daemonv1.SendAgentDirectMessageRequest) (*daemonv1.SendAgentDirectMessageResponse, error) {
	return testConnectMsg(c.client.SendAgentDirectMessage(ctx, testConnectReq(req)))
}

func (c testDaemonClient) UpdateAgentStatus(ctx context.Context, req *daemonv1.UpdateAgentStatusRequest) (*daemonv1.UpdateAgentStatusResponse, error) {
	return testConnectMsg(c.client.UpdateAgentStatus(ctx, testConnectReq(req)))
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

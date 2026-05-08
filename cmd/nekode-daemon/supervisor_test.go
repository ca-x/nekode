package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"google.golang.org/grpc"
)

func TestRunSupervisorCompletesQueuedRun(t *testing.T) {
	client := newFakeSupervisorClient(&daemonv1.Run{
		RunId:            "run-1",
		TaskId:           "task-1",
		Target:           "#LightOsClub",
		AgentId:          "agent-1",
		ComputerId:       "computer-1",
		RuntimeProfileId: "profile-agent-1",
		State:            daemonv1.RunState_RUN_STATE_QUEUED,
		Summary:          "say hello",
	})
	runner := &fakeRuntimeRunner{result: runtimeCommandResult{Output: "hello\n"}}
	supervisor := newRunSupervisor(runSupervisorConfig{
		Config: daemonConfig{
			ComputerID:        "computer-1",
			AgentID:           "agent-1",
			RuntimeKind:       "codex",
			Target:            "#LightOsClub",
			HeartbeatInterval: time.Second,
			RunTimeout:        time.Second,
			MaxConcurrentRuns: 1,
			ExecutorCommand:   "echo",
			ExecutorArgs:      []string{"ok"},
		},
		Client: client,
		Runner: runner,
	})

	if err := supervisor.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce() error = %v", err)
	}
	if got, want := client.runStates(), []daemonv1.RunState{daemonv1.RunState_RUN_STATE_RUNNING, daemonv1.RunState_RUN_STATE_COMPLETED}; !sameRunStates(got, want) {
		t.Fatalf("run states = %v, want %v", got, want)
	}
	if len(client.steps) < 4 {
		t.Fatalf("steps = %d, want start/command running/command completed/result", len(client.steps))
	}
	if client.steps[0].GetKind() != daemonv1.RunStepKind_RUN_STEP_KIND_START ||
		client.steps[0].GetStatus() != daemonv1.RunStepStatus_RUN_STEP_STATUS_COMPLETED {
		t.Fatalf("first step = %+v, want completed start", client.steps[0])
	}
	if client.steps[len(client.steps)-1].GetKind() != daemonv1.RunStepKind_RUN_STEP_KIND_RESULT ||
		client.steps[len(client.steps)-1].GetStatus() != daemonv1.RunStepStatus_RUN_STEP_STATUS_COMPLETED {
		t.Fatalf("last step = %+v, want completed result", client.steps[len(client.steps)-1])
	}
	if len(client.releasedPermits) != 1 || client.releasedPermits[0] == "" {
		t.Fatalf("released permits = %+v, want one released permit", client.releasedPermits)
	}
	if len(runner.commands) != 1 || runner.commands[0].Command != "echo" || strings.Join(runner.commands[0].Args, " ") != "ok" {
		t.Fatalf("runner commands = %+v, want echo ok", runner.commands)
	}
	if !containsString(runner.commands[0].Env, "NEKODE_RUN_ID=run-1") {
		t.Fatalf("runner env = %+v, want run id", runner.commands[0].Env)
	}
	if !client.hasActivity("run_completed") {
		t.Fatalf("activities = %+v, want run_completed", client.activities)
	}
	if !client.hasAgentStatus(daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING) {
		t.Fatalf("agent statuses = %+v, want waiting status", client.agentStatuses)
	}
}

func TestRunSupervisorInjectsLaunchPromptSnapshot(t *testing.T) {
	client := newFakeSupervisorClient(&daemonv1.Run{
		RunId:            "run-prompt",
		TaskId:           "task-1",
		Target:           "#LightOsClub",
		AgentId:          "agent-1",
		ComputerId:       "computer-1",
		RuntimeProfileId: "profile-agent-1",
		State:            daemonv1.RunState_RUN_STATE_QUEUED,
		Summary:          "say hello",
	})
	client.promptSnapshot = &daemonv1.LaunchPromptSnapshot{
		SnapshotId:       "prompt-run-prompt",
		RunId:            "run-prompt",
		AgentId:          "agent-1",
		RuntimeProfileId: "profile-agent-1",
		TemplateVersion:  "nekode.launch-prompt.v1",
		ContentHash:      "abc123",
		Content:          "Nekode launch prompt\nruntime/profile system_message: stay concise",
		Sections: []*daemonv1.LaunchPromptSnapshotSection{
			{Name: "agent_identity"},
			{Name: "runtime_profile", Redacted: true},
		},
		RedactionSummary: "redacted: runtime_profile.adapter_config",
	}
	runner := &fakeRuntimeRunner{result: runtimeCommandResult{Output: "hello\n"}}
	supervisor := newRunSupervisor(runSupervisorConfig{
		Config: daemonConfig{
			ComputerID:        "computer-1",
			AgentID:           "agent-1",
			RuntimeKind:       "codex",
			Target:            "#LightOsClub",
			HeartbeatInterval: time.Second,
			RunTimeout:        time.Second,
			MaxConcurrentRuns: 1,
		},
		Client: client,
		Runner: runner,
	})

	if err := supervisor.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce() error = %v", err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("runner commands = %+v, want one runtime command", runner.commands)
	}
	args := runner.commands[0].Args
	if !containsString(args, "--system-message") || !containsString(args, client.promptSnapshot.GetContent()) {
		t.Fatalf("runner args = %+v, want launch prompt injected as system-message", args)
	}
	if !containsString(runner.commands[0].Env, "NEKODE_LAUNCH_PROMPT_SNAPSHOT_ID=prompt-run-prompt") ||
		!containsString(runner.commands[0].Env, "NEKODE_LAUNCH_PROMPT_HASH=abc123") {
		t.Fatalf("runner env = %+v, want launch prompt audit env", runner.commands[0].Env)
	}
	if !client.hasActivity("prompt_snapshot_loaded") {
		t.Fatalf("activities = %+v, want prompt_snapshot_loaded", client.activities)
	}
	for _, activity := range client.activities {
		if activity.GetKind() == "command_run" && strings.Contains(activity.GetDetail(), client.promptSnapshot.GetContent()) {
			t.Fatalf("command_run detail leaked full launch prompt: %q", activity.GetDetail())
		}
	}
}

func TestRunSupervisorMarksRunFailed(t *testing.T) {
	client := newFakeSupervisorClient(&daemonv1.Run{
		RunId:      "run-fail",
		Target:     "#LightOsClub",
		AgentId:    "agent-1",
		ComputerId: "computer-1",
		State:      daemonv1.RunState_RUN_STATE_QUEUED,
	})
	runner := &fakeRuntimeRunner{result: runtimeCommandResult{Output: "stderr text", ExitCode: 2, Err: errors.New("exit status 2")}}
	supervisor := newRunSupervisor(runSupervisorConfig{
		Config: daemonConfig{
			ComputerID:        "computer-1",
			AgentID:           "agent-1",
			Target:            "#LightOsClub",
			HeartbeatInterval: time.Second,
			RunTimeout:        time.Second,
			MaxConcurrentRuns: 1,
			ExecutorCommand:   "false",
		},
		Client: client,
		Runner: runner,
	})

	if err := supervisor.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce() error = %v", err)
	}
	states := client.runStates()
	if len(states) == 0 || states[len(states)-1] != daemonv1.RunState_RUN_STATE_FAILED {
		t.Fatalf("run states = %v, want final failed", states)
	}
	if !client.hasStepStatus(daemonv1.RunStepStatus_RUN_STEP_STATUS_FAILED) {
		t.Fatalf("steps = %+v, want failed step", client.steps)
	}
	if !client.hasActivity("run_failed") {
		t.Fatalf("activities = %+v, want run_failed", client.activities)
	}
	if !client.hasAgentStatus(daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_BLOCKED) {
		t.Fatalf("agent statuses = %+v, want blocked status", client.agentStatuses)
	}
}

func TestRunSupervisorHandlesTerminateControl(t *testing.T) {
	client := newFakeSupervisorClient()
	canceled := make(chan struct{}, 1)
	supervisor := newRunSupervisor(runSupervisorConfig{
		Config: daemonConfig{
			ComputerID:        "computer-1",
			AgentID:           "agent-1",
			Target:            "#LightOsClub",
			HeartbeatInterval: time.Second,
			RunTimeout:        time.Second,
			MaxConcurrentRuns: 1,
		},
		Client: client,
		Runner: &fakeRuntimeRunner{},
	})
	supervisor.running["run-1"] = &supervisedProcess{
		RunID:   "run-1",
		AgentID: "agent-1",
		cancel:  func() { canceled <- struct{}{} },
	}

	err := supervisor.handleServerEvent(context.Background(), &daemonv1.ServerEvent{
		Payload: &daemonv1.ServerEvent_AgentControl{AgentControl: &daemonv1.AgentControlOperation{
			OperationId:      "control-1",
			AgentId:          "agent-1",
			ComputerId:       "computer-1",
			RuntimeProfileId: "profile-agent-1",
			Action:           daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_TERMINATE,
			State:            daemonv1.AgentControlState_AGENT_CONTROL_STATE_QUEUED,
		}},
	})
	if err != nil {
		t.Fatalf("handleServerEvent(control) error = %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatalf("terminate control did not cancel running process")
	}
	if !client.hasActivity("agent_control_terminate") {
		t.Fatalf("activities = %+v, want terminate activity", client.activities)
	}
	if !client.hasAgentStatus(daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING) {
		t.Fatalf("agent statuses = %+v, want waiting control status", client.agentStatuses)
	}
}

func TestRunSupervisorHandlesRestartResetControls(t *testing.T) {
	tests := []struct {
		name   string
		action daemonv1.AgentControlAction
	}{
		{"restart", daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART},
		{"restart_reset_session", daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART_RESET_SESSION},
		{"restart_full_reset", daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART_FULL_RESET},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeSupervisorClient()
			canceled := make(chan struct{}, 1)
			supervisor := newRunSupervisor(runSupervisorConfig{
				Config: daemonConfig{
					ComputerID:        "computer-1",
					AgentID:           "agent-1",
					Target:            "#LightOsClub",
					HeartbeatInterval: time.Second,
					RunTimeout:        time.Second,
					MaxConcurrentRuns: 1,
				},
				Client: client,
				Runner: &fakeRuntimeRunner{},
			})
			supervisor.running["run-1"] = &supervisedProcess{
				RunID:   "run-1",
				AgentID: "agent-1",
				cancel:  func() { canceled <- struct{}{} },
			}

			err := supervisor.handleServerEvent(context.Background(), &daemonv1.ServerEvent{
				Payload: &daemonv1.ServerEvent_AgentControl{AgentControl: &daemonv1.AgentControlOperation{
					OperationId:      "control-" + tt.name,
					AgentId:          "agent-1",
					ComputerId:       "computer-1",
					RuntimeProfileId: "profile-agent-1",
					Action:           tt.action,
					State:            daemonv1.AgentControlState_AGENT_CONTROL_STATE_QUEUED,
				}},
			})
			if err != nil {
				t.Fatalf("handleServerEvent(%s) error = %v", tt.name, err)
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatalf("%s control did not cancel running process", tt.name)
			}
			if !client.hasActivity("agent_control_restart") {
				t.Fatalf("activities = %+v, want restart activity", client.activities)
			}
			if got, want := client.agentActivityStates(), []daemonv1.AgentActivityState{
				daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_RESTARTING,
				daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING,
			}; !sameAgentActivityStates(got, want) {
				t.Fatalf("agent activity states = %v, want %v", got, want)
			}
			if client.agentStatuses[0].GetOperationId() != "control-"+tt.name ||
				client.agentStatuses[1].GetOperationId() != "control-"+tt.name {
				t.Fatalf("agent statuses = %+v, want operation id propagated", client.agentStatuses)
			}
		})
	}
}

func TestRunSupervisorReportsUnsupportedControl(t *testing.T) {
	client := newFakeSupervisorClient()
	supervisor := newRunSupervisor(runSupervisorConfig{
		Config: daemonConfig{
			ComputerID:        "computer-1",
			AgentID:           "agent-1",
			Target:            "#LightOsClub",
			HeartbeatInterval: time.Second,
			RunTimeout:        time.Second,
			MaxConcurrentRuns: 1,
		},
		Client: client,
		Runner: &fakeRuntimeRunner{},
	})

	err := supervisor.handleServerEvent(context.Background(), &daemonv1.ServerEvent{
		Payload: &daemonv1.ServerEvent_AgentControl{AgentControl: &daemonv1.AgentControlOperation{
			OperationId:      "control-unsupported",
			AgentId:          "agent-1",
			ComputerId:       "computer-1",
			RuntimeProfileId: "profile-agent-1",
			Action:           daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_UNSPECIFIED,
			State:            daemonv1.AgentControlState_AGENT_CONTROL_STATE_QUEUED,
		}},
	})
	if err != nil {
		t.Fatalf("handleServerEvent(unsupported) error = %v", err)
	}
	if !client.hasActivity("agent_control_unsupported") {
		t.Fatalf("activities = %+v, want unsupported control activity", client.activities)
	}
	if len(client.agentStatuses) != 1 {
		t.Fatalf("agent statuses = %+v, want one unsupported control status", client.agentStatuses)
	}
	got := client.agentStatuses[0]
	if got.GetActivityState() != daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_BLOCKED ||
		got.GetHealth() != daemonv1.AgentHealth_AGENT_HEALTH_RUNTIME_ERROR ||
		got.GetOperationId() != "control-unsupported" {
		t.Fatalf("agent status = %+v, want blocked runtime error with operation id", got)
	}
}

func TestRunSupervisorHandlesDirectMessage(t *testing.T) {
	client := newFakeSupervisorClient()
	runner := &fakeRuntimeRunner{result: runtimeCommandResult{Output: "direct ok"}}
	supervisor := newRunSupervisor(runSupervisorConfig{
		Config: daemonConfig{
			ComputerID:        "computer-1",
			AgentID:           "agent-1",
			Target:            "#LightOsClub",
			HeartbeatInterval: time.Second,
			RunTimeout:        time.Second,
			MaxConcurrentRuns: 1,
			ExecutorCommand:   "echo",
			ExecutorArgs:      []string{"direct"},
		},
		Client: client,
		Runner: runner,
	})

	err := supervisor.handleServerEvent(context.Background(), &daemonv1.ServerEvent{
		Payload: &daemonv1.ServerEvent_Message{Message: &daemonv1.CollaborationMessage{
			MessageId: "msg-1",
			Target:    "dm:agent-1",
			Content:   "please work",
		}},
	})
	if err != nil {
		t.Fatalf("handleServerEvent(message) error = %v", err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("runner commands = %+v, want one direct-message run", runner.commands)
	}
	states := client.runStates()
	if len(states) == 0 || states[len(states)-1] != daemonv1.RunState_RUN_STATE_COMPLETED {
		t.Fatalf("run states = %v, want completed", states)
	}
	if len(client.messages) != 1 || client.messages[0].GetReplyToMessageId() != "msg-1" {
		t.Fatalf("messages = %+v, want reply to msg-1", client.messages)
	}
}

type fakeRuntimeRunner struct {
	commands []runtimeCommand
	result   runtimeCommandResult
	onRun    func()
}

func (r *fakeRuntimeRunner) Run(_ context.Context, cmd runtimeCommand) runtimeCommandResult {
	r.commands = append(r.commands, cmd)
	if r.onRun != nil {
		r.onRun()
	}
	return r.result
}

type fakeSupervisorClient struct {
	runs            []*daemonv1.Run
	promptSnapshot  *daemonv1.LaunchPromptSnapshot
	updates         []*daemonv1.UpdateRunStatusRequest
	steps           []*daemonv1.RunStep
	activities      []*daemonv1.LogActivityRequest
	agentStatuses   []*daemonv1.AgentStatusSnapshot
	messages        []*daemonv1.SendMessageRequest
	releasedPermits []string
}

func newFakeSupervisorClient(runs ...*daemonv1.Run) *fakeSupervisorClient {
	return &fakeSupervisorClient{runs: runs}
}

func (c *fakeSupervisorClient) AcquireStartPermit(context.Context, *daemonv1.AcquireStartPermitRequest, ...grpc.CallOption) (*daemonv1.AcquireStartPermitResponse, error) {
	return &daemonv1.AcquireStartPermitResponse{
		Granted:     true,
		PermitLease: &daemonv1.Lease{LeaseId: "lease-1"},
	}, nil
}

func (c *fakeSupervisorClient) ReleaseStartPermit(_ context.Context, req *daemonv1.ReleaseStartPermitRequest, _ ...grpc.CallOption) (*daemonv1.ReleaseStartPermitResponse, error) {
	c.releasedPermits = append(c.releasedPermits, req.GetLeaseId())
	return &daemonv1.ReleaseStartPermitResponse{Accepted: true}, nil
}

func (c *fakeSupervisorClient) FetchAssignedRuns(_ context.Context, req *daemonv1.FetchAssignedRunsRequest, _ ...grpc.CallOption) (*daemonv1.FetchAssignedRunsResponse, error) {
	out := []*daemonv1.Run{}
	for _, run := range c.runs {
		if req.GetComputerId() != "" && run.GetComputerId() != req.GetComputerId() {
			continue
		}
		if len(req.GetAgentIds()) > 0 && !containsString(req.GetAgentIds(), run.GetAgentId()) {
			continue
		}
		out = append(out, run)
	}
	return &daemonv1.FetchAssignedRunsResponse{Runs: out}, nil
}

func (c *fakeSupervisorClient) GetLaunchPromptSnapshot(_ context.Context, req *daemonv1.GetLaunchPromptSnapshotRequest, _ ...grpc.CallOption) (*daemonv1.GetLaunchPromptSnapshotResponse, error) {
	if c.promptSnapshot == nil {
		return &daemonv1.GetLaunchPromptSnapshotResponse{}, nil
	}
	snapshot := *c.promptSnapshot
	snapshot.RunId = req.GetRunId()
	snapshot.AgentId = req.GetAgentId()
	return &daemonv1.GetLaunchPromptSnapshotResponse{Snapshot: &snapshot}, nil
}

func (c *fakeSupervisorClient) UpdateRunStatus(_ context.Context, req *daemonv1.UpdateRunStatusRequest, _ ...grpc.CallOption) (*daemonv1.UpdateRunStatusResponse, error) {
	c.updates = append(c.updates, req)
	run := &daemonv1.Run{RunId: req.GetRunId(), AgentId: req.GetAgentId(), State: req.GetState(), Summary: req.GetSummary(), Error: req.GetError()}
	return &daemonv1.UpdateRunStatusResponse{Accepted: true, Run: run}, nil
}

func (c *fakeSupervisorClient) AppendRunStep(_ context.Context, req *daemonv1.AppendRunStepRequest, _ ...grpc.CallOption) (*daemonv1.AppendRunStepResponse, error) {
	step := req.GetStep()
	if step.StepId == "" {
		step.StepId = "step-" + time.Now().Format("150405.000000000")
	}
	c.steps = append(c.steps, step)
	return &daemonv1.AppendRunStepResponse{Accepted: true, Step: step}, nil
}

func (c *fakeSupervisorClient) UpdateAgentStatus(_ context.Context, req *daemonv1.UpdateAgentStatusRequest, _ ...grpc.CallOption) (*daemonv1.UpdateAgentStatusResponse, error) {
	c.agentStatuses = append(c.agentStatuses, req.GetStatus())
	return &daemonv1.UpdateAgentStatusResponse{Status: req.GetStatus()}, nil
}

func (c *fakeSupervisorClient) SendMessage(_ context.Context, req *daemonv1.SendMessageRequest, _ ...grpc.CallOption) (*daemonv1.SendMessageResponse, error) {
	c.messages = append(c.messages, req)
	return &daemonv1.SendMessageResponse{Accepted: true, Message: &daemonv1.CollaborationMessage{MessageId: "reply-1", Target: req.GetTarget(), Content: req.GetContent()}}, nil
}

func (c *fakeSupervisorClient) LogActivity(_ context.Context, req *daemonv1.LogActivityRequest, _ ...grpc.CallOption) (*daemonv1.LogActivityResponse, error) {
	c.activities = append(c.activities, req)
	return &daemonv1.LogActivityResponse{Activity: &daemonv1.ActivityRecord{ActivityId: "activity-" + req.GetKind(), Kind: req.GetKind()}}, nil
}

func (c *fakeSupervisorClient) runStates() []daemonv1.RunState {
	out := make([]daemonv1.RunState, 0, len(c.updates))
	for _, update := range c.updates {
		out = append(out, update.GetState())
	}
	return out
}

func (c *fakeSupervisorClient) hasActivity(kind string) bool {
	for _, activity := range c.activities {
		if activity.GetKind() == kind {
			return true
		}
	}
	return false
}

func (c *fakeSupervisorClient) hasStepStatus(status daemonv1.RunStepStatus) bool {
	for _, step := range c.steps {
		if step.GetStatus() == status {
			return true
		}
	}
	return false
}

func (c *fakeSupervisorClient) hasAgentStatus(state daemonv1.AgentActivityState) bool {
	for _, status := range c.agentStatuses {
		if status.GetActivityState() == state {
			return true
		}
	}
	return false
}

func (c *fakeSupervisorClient) agentActivityStates() []daemonv1.AgentActivityState {
	out := make([]daemonv1.AgentActivityState, 0, len(c.agentStatuses))
	for _, status := range c.agentStatuses {
		out = append(out, status.GetActivityState())
	}
	return out
}

func sameRunStates(got []daemonv1.RunState, want []daemonv1.RunState) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sameAgentActivityStates(got []daemonv1.AgentActivityState, want []daemonv1.AgentActivityState) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

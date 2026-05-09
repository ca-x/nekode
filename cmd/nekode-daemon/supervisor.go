package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/runtimeadapter"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxRunDetailBytes = 4096

type runSupervisorClient interface {
	AcquireStartPermit(context.Context, *daemonv1.AcquireStartPermitRequest, ...grpc.CallOption) (*daemonv1.AcquireStartPermitResponse, error)
	ReleaseStartPermit(context.Context, *daemonv1.ReleaseStartPermitRequest, ...grpc.CallOption) (*daemonv1.ReleaseStartPermitResponse, error)
	FetchAssignedRuns(context.Context, *daemonv1.FetchAssignedRunsRequest, ...grpc.CallOption) (*daemonv1.FetchAssignedRunsResponse, error)
	GetLaunchPromptSnapshot(context.Context, *daemonv1.GetLaunchPromptSnapshotRequest, ...grpc.CallOption) (*daemonv1.GetLaunchPromptSnapshotResponse, error)
	UpdateRunStatus(context.Context, *daemonv1.UpdateRunStatusRequest, ...grpc.CallOption) (*daemonv1.UpdateRunStatusResponse, error)
	AppendRunStep(context.Context, *daemonv1.AppendRunStepRequest, ...grpc.CallOption) (*daemonv1.AppendRunStepResponse, error)
	UpdateAgentStatus(context.Context, *daemonv1.UpdateAgentStatusRequest, ...grpc.CallOption) (*daemonv1.UpdateAgentStatusResponse, error)
	SendMessage(context.Context, *daemonv1.SendMessageRequest, ...grpc.CallOption) (*daemonv1.SendMessageResponse, error)
	LogActivity(context.Context, *daemonv1.LogActivityRequest, ...grpc.CallOption) (*daemonv1.LogActivityResponse, error)
	ReportAgentRun(context.Context, ...grpc.CallOption) (daemonv1.DaemonControlService_ReportAgentRunClient, error)
}

type runSupervisorConfig struct {
	Config         daemonConfig
	Client         runSupervisorClient
	WithToken      func(context.Context) context.Context
	RequestContext func() *daemonv1.RequestContext
	Runner         runtimeCommandRunner
}

type runSupervisor struct {
	cfg            daemonConfig
	client         runSupervisorClient
	withToken      func(context.Context) context.Context
	requestContext func() *daemonv1.RequestContext
	runner         runtimeCommandRunner

	mu      sync.Mutex
	running map[string]*supervisedProcess
}

type supervisedProcess struct {
	RunID           string
	AgentID         string
	Command         string
	Args            []string
	StartedTimeUnix int64
	cancel          context.CancelFunc
}

type runtimeCommand struct {
	Command string
	Args    []string
	Env     []string
	Dir     string
	Stdin   string
}

type runtimeCommandResult struct {
	Output   string
	ExitCode int
	Err      error
}

type runtimeCommandRunner interface {
	Run(context.Context, runtimeCommand) runtimeCommandResult
}

type commandRunner struct{}

func newRunSupervisor(cfg runSupervisorConfig) *runSupervisor {
	if cfg.WithToken == nil {
		cfg.WithToken = func(ctx context.Context) context.Context { return ctx }
	}
	if cfg.RequestContext == nil {
		cfg.RequestContext = func() *daemonv1.RequestContext { return nil }
	}
	if cfg.Runner == nil {
		cfg.Runner = commandRunner{}
	}
	return &runSupervisor{
		cfg:            cfg.Config,
		client:         cfg.Client,
		withToken:      cfg.WithToken,
		requestContext: cfg.RequestContext,
		runner:         cfg.Runner,
		running:        make(map[string]*supervisedProcess),
	}
}

func (s *runSupervisor) pollOnce(ctx context.Context) error {
	if s.client == nil {
		return nil
	}
	req := &daemonv1.FetchAssignedRunsRequest{
		ComputerId: s.cfg.ComputerID,
		Limit:      uint32(s.cfg.MaxConcurrentRuns),
		RequestId:  newRequestID("fetch-runs"),
	}
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	resp, err := s.client.FetchAssignedRuns(callCtx, req)
	if err != nil {
		return fmt.Errorf("fetch assigned runs: %w", err)
	}
	for _, run := range resp.GetRuns() {
		if !shouldStartRun(run) {
			continue
		}
		if !s.reserveRun(run.GetRunId()) {
			continue
		}
		s.executeRun(ctx, run)
		s.releaseRun(run.GetRunId())
	}
	return nil
}

func shouldStartRun(run *daemonv1.Run) bool {
	if run == nil || strings.TrimSpace(run.GetRunId()) == "" {
		return false
	}
	switch run.GetState() {
	case daemonv1.RunState_RUN_STATE_UNSPECIFIED,
		daemonv1.RunState_RUN_STATE_QUEUED:
		return true
	default:
		return false
	}
}

func (s *runSupervisor) reserveRun(runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.running[runID]; ok {
		return false
	}
	s.running[runID] = &supervisedProcess{RunID: runID, StartedTimeUnix: time.Now().Unix()}
	return true
}

func (s *runSupervisor) setProcessCommand(runID string, cmd runtimeCommand, cancel context.CancelFunc, agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	process := s.running[runID]
	if process == nil {
		return
	}
	process.AgentID = agentID
	process.Command = cmd.Command
	process.Args = append([]string(nil), cmd.Args...)
	process.cancel = cancel
}

func (s *runSupervisor) releaseRun(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, runID)
}

func (s *runSupervisor) handleServerEvent(ctx context.Context, event *daemonv1.ServerEvent) error {
	switch payload := event.GetPayload().(type) {
	case *daemonv1.ServerEvent_AgentControl:
		return s.executeControl(ctx, payload.AgentControl)
	case *daemonv1.ServerEvent_Message:
		return s.executeDirectMessage(ctx, payload.Message)
	case *daemonv1.ServerEvent_Run:
		run := payload.Run
		if !shouldStartRun(run) {
			return nil
		}
		if !s.reserveRun(run.GetRunId()) {
			return nil
		}
		defer s.releaseRun(run.GetRunId())
		s.executeRun(ctx, run)
		return nil
	default:
		return nil
	}
}

func (s *runSupervisor) executeControl(ctx context.Context, op *daemonv1.AgentControlOperation) error {
	if op == nil || op.GetOperationId() == "" {
		return nil
	}
	agentID := firstNonEmpty(op.GetAgentId(), s.cfg.AgentID)
	switch op.GetAction() {
	case daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_TERMINATE:
		canceled := s.cancelAgentRuns(agentID)
		s.logActivity(ctx, s.cfg.Target, agentID, "agent_control_terminate", "terminate requested", fmt.Sprintf("operation=%s canceled_runs=%d", op.GetOperationId(), canceled), "", "")
		return s.reportControlStatus(ctx, op, daemonv1.AgentControlState_AGENT_CONTROL_STATE_COMPLETED, daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING, "terminate completed")
	case daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART,
		daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART_RESET_SESSION,
		daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART_FULL_RESET:
		canceled := s.cancelAgentRuns(agentID)
		s.logActivity(ctx, s.cfg.Target, agentID, "agent_control_restart", "restart requested", fmt.Sprintf("operation=%s canceled_runs=%d action=%s", op.GetOperationId(), canceled, op.GetAction().String()), "", "")
		if err := s.reportControlStatus(ctx, op, daemonv1.AgentControlState_AGENT_CONTROL_STATE_RUNNING, daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_RESTARTING, "restart running"); err != nil {
			return err
		}
		return s.reportControlStatus(ctx, op, daemonv1.AgentControlState_AGENT_CONTROL_STATE_COMPLETED, daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING, "restart completed")
	default:
		s.logActivity(ctx, s.cfg.Target, agentID, "agent_control_unsupported", "unsupported control requested", op.GetAction().String(), "", "")
		return s.reportControlStatus(ctx, op, daemonv1.AgentControlState_AGENT_CONTROL_STATE_UNSUPPORTED, daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_BLOCKED, "control unsupported")
	}
}

func (s *runSupervisor) cancelAgentRuns(agentID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, process := range s.running {
		if agentID != "" && process.AgentID != "" && process.AgentID != agentID {
			continue
		}
		if process.cancel != nil {
			process.cancel()
			count++
		}
	}
	return count
}

func (s *runSupervisor) reportControlStatus(ctx context.Context, op *daemonv1.AgentControlOperation, controlState daemonv1.AgentControlState, activityState daemonv1.AgentActivityState, summary string) error {
	now := time.Now().Unix()
	agentID := firstNonEmpty(op.GetAgentId(), s.cfg.AgentID)
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	_, err := s.client.UpdateAgentStatus(callCtx, &daemonv1.UpdateAgentStatusRequest{
		Status: &daemonv1.AgentStatusSnapshot{
			AgentId:          agentID,
			ComputerId:       s.cfg.ComputerID,
			RuntimeProfileId: firstNonEmpty(op.GetRuntimeProfileId(), "profile-"+agentID),
			Presence:         presenceForActivity(activityState),
			ActivityState:    activityState,
			Health:           healthForControlState(controlState),
			Severity:         severityForHealth(healthForControlState(controlState)),
			Summary:          summary,
			Detail:           op.GetReason(),
			Target:           s.cfg.Target,
			OperationId:      op.GetOperationId(),
			StartedTimeUnix:  firstNonZero(op.GetCreatedTimeUnix(), now),
			UpdatedTimeUnix:  now,
			ExpiresTimeUnix:  now + int64((2*s.cfg.HeartbeatInterval)/time.Second),
		},
		RequestId:      newRequestID("agent-control-status"),
		IdempotencyKey: newRequestID("agent-control-status"),
		Context:        s.requestContext(),
	})
	return err
}

func healthForControlState(state daemonv1.AgentControlState) daemonv1.AgentHealth {
	switch state {
	case daemonv1.AgentControlState_AGENT_CONTROL_STATE_FAILED,
		daemonv1.AgentControlState_AGENT_CONTROL_STATE_UNSUPPORTED:
		return daemonv1.AgentHealth_AGENT_HEALTH_RUNTIME_ERROR
	default:
		return daemonv1.AgentHealth_AGENT_HEALTH_OK
	}
}

func (s *runSupervisor) executeDirectMessage(ctx context.Context, message *daemonv1.CollaborationMessage) error {
	if message == nil || message.GetMessageId() == "" {
		return nil
	}
	agentID := firstNonEmpty(message.GetAggregateId(), s.cfg.AgentID)
	run := &daemonv1.Run{
		RunId:            "direct-" + message.GetMessageId(),
		Target:           message.GetTarget(),
		AgentId:          agentID,
		ComputerId:       s.cfg.ComputerID,
		RuntimeProfileId: "profile-" + agentID,
		State:            daemonv1.RunState_RUN_STATE_QUEUED,
		InputMessageId:   message.GetMessageId(),
		Summary:          message.GetContent(),
	}
	if !s.reserveRun(run.GetRunId()) {
		return nil
	}
	defer s.releaseRun(run.GetRunId())
	s.executeRun(ctx, run)
	return s.sendDirectMessageReceipt(ctx, message, agentID)
}

func (s *runSupervisor) sendDirectMessageReceipt(ctx context.Context, message *daemonv1.CollaborationMessage, agentID string) error {
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	_, err := s.client.SendMessage(callCtx, &daemonv1.SendMessageRequest{
		Target:           message.GetTarget(),
		Role:             "assistant",
		Content:          "Processed direct message " + message.GetMessageId(),
		ReplyToMessageId: message.GetMessageId(),
		RequestId:        newRequestID("direct-receipt"),
		IdempotencyKey:   newRequestID("direct-receipt"),
		Context:          s.requestContext(),
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_AGENT,
			AgentId:     agentID,
			DisplayName: agentID,
		},
	})
	return err
}

func (s *runSupervisor) executeRun(ctx context.Context, run *daemonv1.Run) {
	agentID := firstNonEmpty(run.GetAgentId(), s.cfg.AgentID)
	target := firstNonEmpty(run.GetTarget(), s.cfg.Target)
	// Open an agent_runs archive stream for the whole run. Failures are
	// non-fatal — the supervisor's own RunStep/Activity tracking is the
	// authoritative record; the archive is a convenience for the UI.
	//
	// The deferred close catches early returns that otherwise forget to
	// close; each early-return below calls closeRunRecorder explicitly
	// with the real failure state so the archive reflects setup errors
	// rather than quietly storing a "succeeded, exit 0" row.
	recorder := newRunRecorder(ctx, s.client, s.withToken, s.cfg.ComputerID, agentID, run.GetRunId(), "run started")
	closeRunRecorder := func(summary string, exitCode int32, errMsg string) {
		if recorder == nil {
			return
		}
		recorder.closeWithEnd(summary, exitCode, errMsg)
		recorder = nil
	}
	defer func() {
		// Safety net: an unhandled early return writes a protocol-violation
		// record so the row is obvious in the UI if a new branch forgets to
		// close explicitly.
		closeRunRecorder("run recorder closed without explicit end", 1, "no explicit closeRunRecorder call")
	}()
	permitLeaseID := ""
	permit, err := s.acquirePermit(ctx, run, agentID)
	if err != nil {
		closeRunRecorder("start permit failed", 1, err.Error())
		s.failRun(ctx, run, agentID, "", "start permit failed", err)
		return
	}
	if permit != nil {
		permitLeaseID = permit.GetLeaseId()
		defer s.releasePermit(ctx, permitLeaseID, agentID)
	}
	if err := s.reportRunAgentStatus(ctx, run, agentID, daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_RUNNING_COMMAND, daemonv1.AgentHealth_AGENT_HEALTH_OK, "run started"); err != nil {
		slog.Warn("daemon supervisor status update failed", "run_id", run.GetRunId(), "error", err)
	}
	if err := s.updateRun(ctx, run, agentID, daemonv1.RunState_RUN_STATE_RUNNING, "runtime command started", "", permitLeaseID); err != nil {
		closeRunRecorder("run status update failed", 1, err.Error())
		s.failRun(ctx, run, agentID, permitLeaseID, "run status update failed", err)
		return
	}
	startStep := s.appendStep(ctx, run, permitLeaseID, daemonv1.RunStepKind_RUN_STEP_KIND_START, daemonv1.RunStepStatus_RUN_STEP_STATUS_COMPLETED, "daemon supervisor started run", "")
	snapshot, err := s.loadLaunchPromptSnapshot(ctx, run, agentID)
	if err != nil {
		closeRunRecorder("load launch prompt snapshot failed", 1, err.Error())
		s.failRun(ctx, run, agentID, permitLeaseID, "load launch prompt snapshot failed", err)
		return
	}
	if snapshot != nil {
		s.logPromptSnapshotLoaded(ctx, target, agentID, snapshot, run.GetRunId())
	}
	cmd, err := s.runtimeCommand(run, agentID, snapshot)
	if err != nil {
		closeRunRecorder("build runtime command failed", 1, err.Error())
		s.failRun(ctx, run, agentID, permitLeaseID, "build runtime command failed", err)
		return
	}
	commandSummary := runtimeCommandSummary(cmd)
	commandStep := s.appendStep(ctx, run, permitLeaseID, daemonv1.RunStepKind_RUN_STEP_KIND_COMMAND, daemonv1.RunStepStatus_RUN_STEP_STATUS_RUNNING, "runtime command running", commandSummary)
	if startStep != nil {
		s.logActivity(ctx, target, agentID, "run_started", "daemon supervisor started run", "", run.GetRunId(), startStep.GetStepId())
	}
	s.logActivity(ctx, target, agentID, "command_run", "runtime command running", commandSummary, run.GetRunId(), commandStepID(commandStep))
	recorder.recordToolEvent(daemonv1.AgentRunPhase_AGENT_RUN_PHASE_TOOL_CALL, "runtime command running", map[string]any{
		"command": cmd.Command,
		"args":    cmd.Args,
	})

	runCtx, cancel := context.WithTimeout(ctx, s.cfg.RunTimeout)
	defer cancel()
	s.setProcessCommand(run.GetRunId(), cmd, cancel, agentID)
	result := s.runner.Run(runCtx, cmd)
	detail := truncateDetail(result.Output)
	if result.Err != nil {
		errDetail := result.Err.Error()
		if detail != "" {
			errDetail += "\n" + detail
		}
		errDetail = "diagnostic_category=" + runtimeFailureCategory(result, errDetail) + "\n" + errDetail
		if commandStep != nil {
			s.appendStep(ctx, run, permitLeaseID, daemonv1.RunStepKind_RUN_STEP_KIND_COMMAND, daemonv1.RunStepStatus_RUN_STEP_STATUS_FAILED, "runtime command failed", errDetail)
		}
		recorder.recordToolEvent(daemonv1.AgentRunPhase_AGENT_RUN_PHASE_ERROR, "runtime command failed", map[string]any{
			"exit_code": result.ExitCode,
			"detail":    errDetail,
		})
		closeRunRecorder("runtime command failed", int32(result.ExitCode), result.Err.Error())
		s.failRun(ctx, run, agentID, permitLeaseID, "runtime command failed", result.Err)
		s.logActivity(ctx, target, agentID, "run_failed", "runtime command failed", errDetail, run.GetRunId(), commandStepID(commandStep))
		return
	}
	if commandStep != nil {
		s.appendStep(ctx, run, permitLeaseID, daemonv1.RunStepKind_RUN_STEP_KIND_COMMAND, daemonv1.RunStepStatus_RUN_STEP_STATUS_COMPLETED, "runtime command completed", detail)
	}
	recorder.recordToolEvent(daemonv1.AgentRunPhase_AGENT_RUN_PHASE_TOOL_RESULT, "runtime command completed", map[string]any{
		"exit_code": result.ExitCode,
		"detail":    detail,
	})
	resultStep := s.appendStep(ctx, run, permitLeaseID, daemonv1.RunStepKind_RUN_STEP_KIND_RESULT, daemonv1.RunStepStatus_RUN_STEP_STATUS_COMPLETED, "run completed", detail)
	if err := s.updateRun(ctx, run, agentID, daemonv1.RunState_RUN_STATE_COMPLETED, "runtime command completed", "", permitLeaseID); err != nil {
		slog.Warn("daemon supervisor completion update failed", "run_id", run.GetRunId(), "error", err)
	}
	if err := s.reportRunAgentStatus(ctx, run, agentID, daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING, daemonv1.AgentHealth_AGENT_HEALTH_OK, "run completed"); err != nil {
		slog.Warn("daemon supervisor status update failed", "run_id", run.GetRunId(), "error", err)
	}
	closeRunRecorder("run completed", int32(result.ExitCode), "")
	s.logActivity(ctx, target, agentID, "run_completed", "runtime command completed", detail, run.GetRunId(), commandStepID(resultStep))
}

func (s *runSupervisor) acquirePermit(ctx context.Context, run *daemonv1.Run, agentID string) (*daemonv1.Lease, error) {
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	resp, err := s.client.AcquireStartPermit(callCtx, &daemonv1.AcquireStartPermitRequest{
		ComputerId:       s.cfg.ComputerID,
		AgentId:          agentID,
		RuntimeProfileId: firstNonEmpty(run.GetRuntimeProfileId(), "profile-"+agentID),
		RequestId:        newRequestID("permit"),
		IdempotencyKey:   newRequestID("permit"),
		Context:          s.requestContext(),
		PermitTtlSeconds: uint32(max(60, int(s.cfg.RunTimeout/time.Second))),
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetGranted() {
		return nil, fmt.Errorf("start permit rejected: %s", resp.GetRejectionReason())
	}
	return resp.GetPermitLease(), nil
}

func (s *runSupervisor) releasePermit(ctx context.Context, leaseID string, agentID string) {
	if strings.TrimSpace(leaseID) == "" {
		return
	}
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	if _, err := s.client.ReleaseStartPermit(callCtx, &daemonv1.ReleaseStartPermitRequest{
		LeaseId:        leaseID,
		ComputerId:     s.cfg.ComputerID,
		AgentId:        agentID,
		RequestId:      newRequestID("permit-release"),
		IdempotencyKey: newRequestID("permit-release"),
		Context:        s.requestContext(),
	}); err != nil {
		slog.Warn("daemon supervisor permit release failed", "lease_id", leaseID, "error", err)
	}
}

func (s *runSupervisor) failRun(ctx context.Context, run *daemonv1.Run, agentID string, leaseID string, summary string, err error) {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	leaseID = firstNonEmpty(leaseID, run.GetLeaseId())
	s.appendStep(ctx, run, leaseID, daemonv1.RunStepKind_RUN_STEP_KIND_RESULT, daemonv1.RunStepStatus_RUN_STEP_STATUS_FAILED, summary, detail)
	if updateErr := s.updateRun(ctx, run, agentID, daemonv1.RunState_RUN_STATE_FAILED, summary, detail, leaseID); updateErr != nil {
		slog.Warn("daemon supervisor failure update failed", "run_id", run.GetRunId(), "error", updateErr)
	}
	if statusErr := s.reportRunAgentStatus(ctx, run, agentID, daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_BLOCKED, daemonv1.AgentHealth_AGENT_HEALTH_RUNTIME_ERROR, summary); statusErr != nil {
		slog.Warn("daemon supervisor failure status update failed", "run_id", run.GetRunId(), "error", statusErr)
	}
}

func (s *runSupervisor) updateRun(ctx context.Context, run *daemonv1.Run, agentID string, state daemonv1.RunState, summary string, detail string, leaseID string) error {
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	_, err := s.client.UpdateRunStatus(callCtx, &daemonv1.UpdateRunStatusRequest{
		RunId:          run.GetRunId(),
		AgentId:        agentID,
		State:          state,
		Summary:        summary,
		Error:          detail,
		RequestId:      newRequestID("run-status"),
		IdempotencyKey: newRequestID("run-status"),
		Context:        s.requestContext(),
		LeaseId:        firstNonEmpty(leaseID, run.GetLeaseId()),
	})
	return err
}

func (s *runSupervisor) appendStep(ctx context.Context, run *daemonv1.Run, leaseID string, kind daemonv1.RunStepKind, status daemonv1.RunStepStatus, summary string, detail string) *daemonv1.RunStep {
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	now := time.Now().Unix()
	resp, err := s.client.AppendRunStep(callCtx, &daemonv1.AppendRunStepRequest{
		Step: &daemonv1.RunStep{
			RunId:             run.GetRunId(),
			Kind:              kind,
			Status:            status,
			Summary:           summary,
			Detail:            truncateDetail(detail),
			StartedTimeUnix:   now,
			CompletedTimeUnix: completedTimeForStep(status, now),
		},
		RequestId:      newRequestID("run-step"),
		IdempotencyKey: newRequestID("run-step"),
		Context:        s.requestContext(),
		LeaseId:        firstNonEmpty(leaseID, run.GetLeaseId()),
	})
	if err != nil {
		slog.Warn("daemon supervisor append step failed", "run_id", run.GetRunId(), "error", err)
		return nil
	}
	return resp.GetStep()
}

func completedTimeForStep(status daemonv1.RunStepStatus, now int64) int64 {
	if status == daemonv1.RunStepStatus_RUN_STEP_STATUS_RUNNING {
		return 0
	}
	return now
}

func (s *runSupervisor) reportRunAgentStatus(ctx context.Context, run *daemonv1.Run, agentID string, state daemonv1.AgentActivityState, health daemonv1.AgentHealth, summary string) error {
	now := time.Now().Unix()
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	_, err := s.client.UpdateAgentStatus(callCtx, &daemonv1.UpdateAgentStatusRequest{
		Status: &daemonv1.AgentStatusSnapshot{
			AgentId:          agentID,
			ComputerId:       s.cfg.ComputerID,
			RuntimeProfileId: firstNonEmpty(run.GetRuntimeProfileId(), "profile-"+agentID),
			Presence:         presenceForActivity(state),
			ActivityState:    state,
			Health:           health,
			Severity:         severityForHealth(health),
			Summary:          summary,
			Target:           firstNonEmpty(run.GetTarget(), s.cfg.Target),
			TaskId:           run.GetTaskId(),
			RunId:            run.GetRunId(),
			StartedTimeUnix:  firstNonZero(run.GetStartedTimeUnix(), now),
			UpdatedTimeUnix:  now,
			ExpiresTimeUnix:  now + int64((2*s.cfg.HeartbeatInterval)/time.Second),
		},
		RequestId:      newRequestID("agent-status"),
		IdempotencyKey: newRequestID("agent-status"),
		Context:        s.requestContext(),
	})
	return err
}

func presenceForActivity(state daemonv1.AgentActivityState) daemonv1.AgentPresence {
	if state == daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING {
		return daemonv1.AgentPresence_AGENT_PRESENCE_IDLE
	}
	if state == daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_BLOCKED {
		return daemonv1.AgentPresence_AGENT_PRESENCE_DEGRADED
	}
	return daemonv1.AgentPresence_AGENT_PRESENCE_BUSY
}

func severityForHealth(health daemonv1.AgentHealth) daemonv1.AgentStatusSeverity {
	if health == daemonv1.AgentHealth_AGENT_HEALTH_OK {
		return daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_INFO
	}
	return daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_ERROR
}

func (s *runSupervisor) logActivity(ctx context.Context, target string, agentID string, kind string, summary string, detail string, runID string, stepID string) {
	if strings.TrimSpace(target) == "" {
		target = "daemon"
	}
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	if _, err := s.client.LogActivity(callCtx, &daemonv1.LogActivityRequest{
		Target:         target,
		AgentId:        agentID,
		Kind:           kind,
		Summary:        summary,
		Detail:         truncateDetail(detail),
		RunId:          runID,
		StepId:         stepID,
		RequestId:      newRequestID("activity"),
		IdempotencyKey: newRequestID("activity"),
		Context:        s.requestContext(),
	}); err != nil {
		slog.Warn("daemon supervisor activity log failed", "run_id", runID, "kind", kind, "error", err)
	}
}

func (s *runSupervisor) loadLaunchPromptSnapshot(ctx context.Context, run *daemonv1.Run, agentID string) (*daemonv1.LaunchPromptSnapshot, error) {
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	resp, err := s.client.GetLaunchPromptSnapshot(callCtx, &daemonv1.GetLaunchPromptSnapshotRequest{
		RunId:            run.GetRunId(),
		AgentId:          agentID,
		ComputerId:       s.cfg.ComputerID,
		RuntimeProfileId: firstNonEmpty(run.GetRuntimeProfileId(), "profile-"+agentID),
		RequestId:        newRequestID("prompt-snapshot"),
		Context:          s.requestContext(),
	})
	if err != nil {
		code := status.Code(err)
		if code == codes.Unimplemented || code == codes.NotFound {
			slog.Warn("daemon supervisor launch prompt snapshot unavailable; using legacy run prompt", "run_id", run.GetRunId(), "error", err)
			return nil, nil
		}
		return nil, err
	}
	if resp.GetSnapshot().GetContent() == "" {
		return nil, nil
	}
	return resp.GetSnapshot(), nil
}

func (s *runSupervisor) logPromptSnapshotLoaded(ctx context.Context, target string, agentID string, snapshot *daemonv1.LaunchPromptSnapshot, runID string) {
	sectionNames := make([]string, 0, len(snapshot.GetSections()))
	for _, section := range snapshot.GetSections() {
		sectionNames = append(sectionNames, section.GetName())
	}
	detail := strings.Join(nonEmptyRuntimeLines(
		"snapshot_id="+snapshot.GetSnapshotId(),
		"content_hash="+snapshot.GetContentHash(),
		"template_version="+snapshot.GetTemplateVersion(),
		"sections="+strings.Join(sectionNames, ","),
		"redaction_summary="+snapshot.GetRedactionSummary(),
	), "\n")
	s.logActivity(ctx, target, agentID, "prompt_snapshot_loaded", "launch prompt snapshot loaded", detail, runID, "")
}

func (s *runSupervisor) runtimeCommand(run *daemonv1.Run, agentID string, snapshot *daemonv1.LaunchPromptSnapshot) (runtimeCommand, error) {
	env := runCommandEnv(s.cfg, run, agentID, snapshot)
	if strings.TrimSpace(s.cfg.ExecutorCommand) != "" {
		return runtimeCommand{
			Command: s.cfg.ExecutorCommand,
			Args:    append([]string(nil), s.cfg.ExecutorArgs...),
			Env:     env,
		}, nil
	}
	kind := firstNonEmpty(s.cfg.RuntimeKind, "codex")
	template := runtimeadapter.DefaultInstanceTemplate(runtimeadapter.RuntimeType{
		Kind:        kind,
		DisplayName: kind,
		Command:     kind,
		Installed:   true,
		Healthy:     true,
	})
	values := map[string]string{
		"display_name": agentID,
	}
	if snapshot.GetContent() != "" {
		values["system_message"] = snapshot.GetContent()
	}
	wrapped, err := runtimeadapter.BuildWrapCommand(template, values)
	if err != nil {
		return runtimeCommand{}, err
	}
	wrapped = runtimeadapter.WithRunPrompt(wrapped, runPrompt(run))
	return runtimeCommand{
		Command: wrapped.Command,
		Args:    append([]string(nil), wrapped.Args...),
		Env:     append(wrapped.Env, env...),
		Dir:     wrapped.Dir,
		Stdin:   wrapped.Stdin,
	}, nil
}

func runPrompt(run *daemonv1.Run) string {
	parts := []string{}
	if run.GetSummary() != "" {
		parts = append(parts, run.GetSummary())
	}
	if run.GetTaskId() != "" {
		parts = append(parts, "task_id="+run.GetTaskId())
	}
	if run.GetInputMessageId() != "" {
		parts = append(parts, "input_message_id="+run.GetInputMessageId())
	}
	return strings.Join(parts, "\n")
}

func runCommandEnv(cfg daemonConfig, run *daemonv1.Run, agentID string, snapshot *daemonv1.LaunchPromptSnapshot) []string {
	env := []string{
		"NEKODE_RUN_ID=" + run.GetRunId(),
		"NEKODE_TASK_ID=" + run.GetTaskId(),
		"NEKODE_AGENT_ID=" + agentID,
		"NEKODE_COMPUTER_ID=" + cfg.ComputerID,
		"NEKODE_TARGET=" + firstNonEmpty(run.GetTarget(), cfg.Target),
		"NEKODE_RUNTIME_PROFILE_ID=" + firstNonEmpty(run.GetRuntimeProfileId(), "profile-"+agentID),
	}
	if snapshot.GetSnapshotId() != "" {
		env = append(env,
			"NEKODE_LAUNCH_PROMPT_SNAPSHOT_ID="+snapshot.GetSnapshotId(),
			"NEKODE_LAUNCH_PROMPT_HASH="+snapshot.GetContentHash(),
			"NEKODE_LAUNCH_PROMPT_TEMPLATE_VERSION="+snapshot.GetTemplateVersion(),
		)
		if snapshot.GetContent() != "" {
			env = append(env, "NEKODE_LAUNCH_PROMPT="+snapshot.GetContent())
		}
	}
	return env
}

func runtimeCommandSummary(cmd runtimeCommand) string {
	parts := append([]string{cmd.Command}, cmd.Args...)
	for i := 0; i < len(parts); i++ {
		if isRedactedRuntimeArg(parts[i]) && i+1 < len(parts) {
			parts[i+1] = "<redacted runtime input>"
			i++
			continue
		}
		if shouldRedactRuntimeValue(parts[i]) {
			parts[i] = "<redacted runtime input>"
		}
	}
	summary := strings.Join(parts, " ")
	if cmd.Dir != "" {
		summary += " cwd=" + cmd.Dir
	}
	if cmd.Stdin != "" {
		summary += " stdin=<redacted runtime input>"
	}
	return summary
}

func isRedactedRuntimeArg(arg string) bool {
	switch arg {
	case "--system-message", "--system-prompt", "--append-system-prompt", "--prompt":
		return true
	default:
		return false
	}
}

func shouldRedactRuntimeValue(value string) bool {
	if strings.Contains(value, "\n") {
		return true
	}
	if strings.Contains(value, "task_id=") || strings.Contains(value, "input_message_id=") {
		return true
	}
	return false
}

func runtimeFailureCategory(result runtimeCommandResult, detail string) string {
	lower := strings.ToLower(detail)
	switch {
	case result.ExitCode == -1 || errors.Is(result.Err, context.DeadlineExceeded):
		return "timeout"
	case strings.Contains(lower, "unknown option"), strings.Contains(lower, "unknown flag"), strings.Contains(lower, "invalid option"), strings.Contains(lower, "unexpected argument"):
		return "argv_contract"
	case strings.Contains(lower, "auth"), strings.Contains(lower, "login"), strings.Contains(lower, "unauthorized"), strings.Contains(lower, "permission denied"), strings.Contains(lower, "api key"):
		return "provider_auth"
	case strings.Contains(lower, "executable file not found"), strings.Contains(lower, "no such file"):
		return "missing_executable"
	default:
		return "runtime_exit"
	}
}

func nonEmptyRuntimeLines(lines ...string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func (commandRunner) Run(ctx context.Context, cmd runtimeCommand) runtimeCommandResult {
	if strings.TrimSpace(cmd.Command) == "" {
		return runtimeCommandResult{Err: fmt.Errorf("runtime command is required"), ExitCode: -1}
	}
	command := exec.CommandContext(ctx, cmd.Command, cmd.Args...)
	command.Env = append(os.Environ(), cmd.Env...)
	if strings.TrimSpace(cmd.Dir) != "" {
		command.Dir = cmd.Dir
	}
	if cmd.Stdin != "" {
		command.Stdin = strings.NewReader(cmd.Stdin)
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	result := runtimeCommandResult{Output: output.String(), Err: err}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	}
	if ctx.Err() != nil {
		result.Err = ctx.Err()
		result.ExitCode = -1
	}
	return result
}

func truncateDetail(value string) string {
	if len(value) <= maxRunDetailBytes {
		return value
	}
	return value[:maxRunDetailBytes] + "\n...truncated..."
}

func commandStepID(step *daemonv1.RunStep) string {
	if step == nil {
		return ""
	}
	return step.GetStepId()
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

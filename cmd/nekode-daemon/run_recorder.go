package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
)

// runRecorder captures a single agent run into the server-side agent_runs
// archive. It opens a fresh client-streaming ReportAgentRun for the run,
// emits a START event, one or more intermediate events, and an END event,
// then closes the stream. Keeping each run in its own stream avoids
// cross-run head-of-line blocking and keeps reconnection logic trivial
// (if a stream breaks mid-run we drop the tail; the supervisor's own
// RunStep/Activity logging still captures the outcome).
type runRecorder struct {
	client     runSupervisorClient
	withToken  func(context.Context) context.Context
	computerID string
	agentID    string
	runID      string
	eventRunID string // identical to the RPC run id; stored for readability
	stream     reportAgentRunClient
}

// newRunRecorder opens a stream and emits the START event. Returning nil
// is a soft failure: the caller continues the run without the archive,
// logging the reason. RunRecorder is not mission-critical to run exec.
func newRunRecorder(ctx context.Context, client runSupervisorClient, withToken func(context.Context) context.Context, computerID, agentID, runID, summary string) *runRecorder {
	if client == nil || runID == "" {
		return nil
	}
	authCtx := ctx
	if withToken != nil {
		authCtx = withToken(ctx)
	}
	stream, err := client.ReportAgentRun(authCtx)
	if err != nil {
		slog.Warn("agent run recorder: open stream failed", "run_id", runID, "error", err)
		return nil
	}
	rec := &runRecorder{
		client:     client,
		withToken:  withToken,
		computerID: computerID,
		agentID:    agentID,
		runID:      runID,
		eventRunID: runID,
		stream:     stream,
	}
	if err := rec.send(daemonv1.AgentRunPhase_AGENT_RUN_PHASE_START, summary, nil, 0, ""); err != nil {
		slog.Warn("agent run recorder: start send failed", "run_id", runID, "error", err)
		_ = stream.Close()
		return nil
	}
	return rec
}

// recordToolEvent emits one intermediate event. Payload is serialised to
// JSON so the server can index it with FTS without coupling the schema.
func (r *runRecorder) recordToolEvent(phase daemonv1.AgentRunPhase, summary string, payload map[string]any) {
	if r == nil {
		return
	}
	encoded := []byte{}
	if payload != nil {
		if data, err := json.Marshal(payload); err == nil {
			encoded = data
		}
	}
	if err := r.send(phase, summary, encoded, 0, ""); err != nil {
		slog.Debug("agent run recorder: tool event drop", "run_id", r.runID, "error", err)
	}
}

// closeWithEnd records the final event and closes the stream. Safe to call
// multiple times; subsequent calls no-op.
func (r *runRecorder) closeWithEnd(summary string, exitCode int32, errMsg string) {
	if r == nil || r.stream == nil {
		return
	}
	if err := r.send(daemonv1.AgentRunPhase_AGENT_RUN_PHASE_END, summary, nil, exitCode, errMsg); err != nil {
		slog.Warn("agent run recorder: end send failed", "run_id", r.runID, "error", err)
	}
	if _, err := r.stream.CloseAndRecv(); err != nil && err != io.EOF {
		slog.Debug("agent run recorder: close error", "run_id", r.runID, "error", err)
	}
	r.stream = nil
}

// send is the internal one-liner that builds and writes an event.
func (r *runRecorder) send(phase daemonv1.AgentRunPhase, summary string, payload []byte, exitCode int32, errMsg string) error {
	return r.stream.Send(&daemonv1.AgentRunEvent{
		RunId:        r.eventRunID,
		AgentId:      r.agentID,
		ComputerId:   r.computerID,
		Phase:        phase,
		AtUnixNano:   time.Now().UnixNano(),
		Summary:      summary,
		PayloadJson:  payload,
		ExitCode:     exitCode,
		ErrorMessage: errMsg,
	})
}

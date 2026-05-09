package daemonrpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListChannelDecisions returns decisions scoped to a channel target,
// optionally filtered by lifecycle status. Empty status_filter means
// all statuses. Cursor-based pagination is not yet supported here —
// the web/api layer tops out at a few hundred decisions per channel
// so a single-page response is fine for now.
func (s *Server) ListChannelDecisions(ctx context.Context, req *daemonv1.ListChannelDecisionsRequest) (*daemonv1.ListChannelDecisionsResponse, error) {
	target := strings.TrimSpace(req.GetTarget())
	if target == "" {
		return nil, status.Error(codes.InvalidArgument, "target is required")
	}
	statuses := make([]string, 0, len(req.GetStatusFilter()))
	for _, raw := range req.GetStatusFilter() {
		statuses = append(statuses, decisionStatusFromProto(raw))
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.store.ListDecisions(ctx, storage.ChannelDecisionListOptions{
		Target:       target,
		StatusFilter: statuses,
		Limit:        limit,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list decisions: %v", err)
	}
	out := make([]*daemonv1.ChannelDecision, 0, len(rows))
	for _, row := range rows {
		out = append(out, decisionToProto(row))
	}
	return &daemonv1.ListChannelDecisionsResponse{Decisions: out}, nil
}

// ProposeChannelDecision creates a new decision in proposed state.
func (s *Server) ProposeChannelDecision(ctx context.Context, req *daemonv1.ProposeChannelDecisionRequest) (*daemonv1.ProposeChannelDecisionResponse, error) {
	title := strings.TrimSpace(req.GetTitle())
	body := strings.TrimSpace(req.GetBody())
	target := strings.TrimSpace(req.GetTarget())
	if target == "" || title == "" || body == "" {
		return nil, status.Error(codes.InvalidArgument, "target, title, and body are required")
	}
	proposerID, proposerKind := actorFromContext(req.GetContext())
	if proposerID == "" {
		return nil, status.Error(codes.Unauthenticated, "proposer identity required")
	}
	created, err := s.store.CreateDecision(ctx, storage.ChannelDecision{
		Target:               target,
		Title:                title,
		Body:                 body,
		Status:               storage.DecisionStatusProposed,
		ProposerID:           proposerID,
		ProposerKind:         proposerKind,
		SupersedesDecisionID: strings.TrimSpace(req.GetSupersedesDecisionId()),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create decision: %v", err)
	}
	return &daemonv1.ProposeChannelDecisionResponse{Decision: decisionToProto(created)}, nil
}

// VoteChannelDecision records or updates a voter's stance. The response
// always includes the fully-refreshed decision (counts + possible
// auto-ratification) so clients don't need a follow-up read.
func (s *Server) VoteChannelDecision(ctx context.Context, req *daemonv1.VoteChannelDecisionRequest) (*daemonv1.VoteChannelDecisionResponse, error) {
	decisionID := strings.TrimSpace(req.GetDecisionId())
	if decisionID == "" {
		return nil, status.Error(codes.InvalidArgument, "decision_id is required")
	}
	voterID, voterKind := actorFromContext(req.GetContext())
	if voterID == "" {
		return nil, status.Error(codes.Unauthenticated, "voter identity required")
	}
	decision := decisionVoteFromProto(req.GetDecision())
	if decision == "" {
		return nil, status.Error(codes.InvalidArgument, "decision vote is required")
	}
	refreshed, persisted, err := s.store.UpsertDecisionVote(ctx, storage.ChannelDecisionVote{
		DecisionID: decisionID,
		VoterID:    voterID,
		VoterKind:  voterKind,
		Decision:   decision,
		Reason:     strings.TrimSpace(req.GetReason()),
	})
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "decision not found")
	}
	if errors.Is(err, storage.ErrInvalid) {
		return nil, status.Error(codes.FailedPrecondition, "decision is not open for voting")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "vote: %v", err)
	}
	return &daemonv1.VoteChannelDecisionResponse{
		Decision: decisionToProto(refreshed),
		Vote:     decisionVoteRecordToProto(persisted),
	}, nil
}

// RatifyChannelDecision is an admin override; when force is false the
// server simply checks the current counts and ratifies if quorum is met.
func (s *Server) RatifyChannelDecision(ctx context.Context, req *daemonv1.RatifyChannelDecisionRequest) (*daemonv1.RatifyChannelDecisionResponse, error) {
	decisionID := strings.TrimSpace(req.GetDecisionId())
	if decisionID == "" {
		return nil, status.Error(codes.InvalidArgument, "decision_id is required")
	}
	current, err := s.store.GetDecision(ctx, decisionID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "decision not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load decision: %v", err)
	}
	if current.Status != storage.DecisionStatusProposed {
		return nil, status.Error(codes.FailedPrecondition, "decision is not proposed")
	}
	canRatify := req.GetForce() || (current.ApproveCount >= 2 && current.RejectCount == 0)
	if !canRatify {
		return nil, status.Error(codes.FailedPrecondition, "quorum not met")
	}
	ratified, err := s.store.ForceRatifyDecision(ctx, decisionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "ratify: %v", err)
	}
	return &daemonv1.RatifyChannelDecisionResponse{Decision: decisionToProto(ratified)}, nil
}

// RetireChannelDecision marks a decision as retired. Only proposed or
// ratified rows can be retired — rejected / already-retired returns
// FailedPrecondition.
func (s *Server) RetireChannelDecision(ctx context.Context, req *daemonv1.RetireChannelDecisionRequest) (*daemonv1.RetireChannelDecisionResponse, error) {
	decisionID := strings.TrimSpace(req.GetDecisionId())
	if decisionID == "" {
		return nil, status.Error(codes.InvalidArgument, "decision_id is required")
	}
	actorID, _ := actorFromContext(req.GetContext())
	if actorID == "" {
		return nil, status.Error(codes.Unauthenticated, "retire actor identity required")
	}
	current, err := s.store.GetDecision(ctx, decisionID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "decision not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load decision: %v", err)
	}
	if current.Status != storage.DecisionStatusProposed && current.Status != storage.DecisionStatusRatified {
		return nil, status.Error(codes.FailedPrecondition, "decision is already closed")
	}
	retired, err := s.store.RetireDecision(ctx, decisionID, actorID, strings.TrimSpace(req.GetReason()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "retire: %v", err)
	}
	return &daemonv1.RetireChannelDecisionResponse{Decision: decisionToProto(retired)}, nil
}

// ListDecisionVotes returns vote history for audit / UI display.
func (s *Server) ListDecisionVotes(ctx context.Context, req *daemonv1.ListDecisionVotesRequest) (*daemonv1.ListDecisionVotesResponse, error) {
	decisionID := strings.TrimSpace(req.GetDecisionId())
	if decisionID == "" {
		return nil, status.Error(codes.InvalidArgument, "decision_id is required")
	}
	rows, err := s.store.ListDecisionVotes(ctx, decisionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list votes: %v", err)
	}
	out := make([]*daemonv1.DecisionVoteRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, decisionVoteRecordToProto(row))
	}
	return &daemonv1.ListDecisionVotesResponse{Votes: out}, nil
}

// ReportAgentRun is the streaming endpoint daemons use to publish agent
// run events. The server persists them in order and returns the counts
// of persisted vs dropped events when the stream closes.
func (s *Server) ReportAgentRun(stream daemonv1.DaemonControlService_ReportAgentRunServer) error {
	var persisted, dropped int64
	for {
		event, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&daemonv1.ReportAgentRunResponse{
				PersistedCount: persisted,
				DroppedCount:   dropped,
			})
		}
		if err != nil {
			return err
		}
		phase := agentRunPhaseFromProto(event.GetPhase())
		if phase == "" {
			dropped++
			continue
		}
		if _, _, err := s.store.AppendAgentRunEvent(stream.Context(),
			storage.AgentRunEvent{
				ID:           "",
				RunID:        event.GetRunId(),
				AtUnixNano:   event.GetAtUnixNano(),
				Phase:        phase,
				Summary:      event.GetSummary(),
				PayloadJSON:  string(event.GetPayloadJson()),
				ExitCode:     event.GetExitCode(),
				ErrorMessage: event.GetErrorMessage(),
			},
			storage.AgentRun{
				AgentID:     event.GetAgentId(),
				ComputerID:  event.GetComputerId(),
				StartedUnix: event.GetAtUnixNano() / 1_000_000_000,
				EndedUnix:   event.GetAtUnixNano() / 1_000_000_000,
			},
		); err != nil {
			// A malformed event (wrong phase ordering) is treated as dropped
			// rather than tearing down the entire stream, so one bad record
			// doesn't cause the daemon to lose its whole backlog.
			if errors.Is(err, storage.ErrInvalid) {
				dropped++
				continue
			}
			return status.Errorf(codes.Internal, "persist event: %v", err)
		}
		persisted++
	}
}

// ListAgentRuns returns runs for an agent / computer / both.
func (s *Server) ListAgentRuns(ctx context.Context, req *daemonv1.ListAgentRunsRequest) (*daemonv1.ListAgentRunsResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.store.ListAgentRuns(ctx, storage.AgentRunListOptions{
		AgentID:    strings.TrimSpace(req.GetAgentId()),
		ComputerID: strings.TrimSpace(req.GetComputerId()),
		Limit:      limit,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list runs: %v", err)
	}
	out := make([]*daemonv1.AgentRunSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentRunSummaryToProto(row))
	}
	return &daemonv1.ListAgentRunsResponse{Runs: out}, nil
}

// GetAgentRun returns a run, optionally with its full event stream.
func (s *Server) GetAgentRun(ctx context.Context, req *daemonv1.GetAgentRunRequest) (*daemonv1.GetAgentRunResponse, error) {
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	run, events, err := s.store.GetAgentRun(ctx, runID, req.GetIncludeEvents())
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "run not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get run: %v", err)
	}
	protoEvents := make([]*daemonv1.AgentRunEvent, 0, len(events))
	for _, ev := range events {
		protoEvents = append(protoEvents, agentRunEventToProto(ev))
	}
	return &daemonv1.GetAgentRunResponse{
		Run:    agentRunSummaryToProto(run),
		Events: protoEvents,
	}, nil
}

// SearchAgentRuns runs a substring search over events; FTS5 swap lands
// later and keeps this RPC shape.
func (s *Server) SearchAgentRuns(ctx context.Context, req *daemonv1.SearchAgentRunsRequest) (*daemonv1.SearchAgentRunsResponse, error) {
	query := strings.TrimSpace(req.GetQuery())
	if query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	hits, err := s.store.SearchAgentRuns(ctx, storage.AgentRunSearchOptions{
		Query:      query,
		AgentID:    strings.TrimSpace(req.GetAgentId()),
		ComputerID: strings.TrimSpace(req.GetComputerId()),
		Limit:      int(req.GetLimit()),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "search runs: %v", err)
	}
	out := make([]*daemonv1.SearchAgentRunsResponse_Hit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, &daemonv1.SearchAgentRunsResponse_Hit{
			Run:            agentRunSummaryToProto(hit.Run),
			MatchingEvent:  agentRunEventToProto(hit.Event),
			Highlight:      hit.Highlight,
		})
	}
	return &daemonv1.SearchAgentRunsResponse{Hits: out}, nil
}

// --- proto <-> storage converters ---------------------------------------

func decisionStatusFromProto(status daemonv1.DecisionStatus) string {
	switch status {
	case daemonv1.DecisionStatus_DECISION_STATUS_PROPOSED:
		return storage.DecisionStatusProposed
	case daemonv1.DecisionStatus_DECISION_STATUS_RATIFIED:
		return storage.DecisionStatusRatified
	case daemonv1.DecisionStatus_DECISION_STATUS_REJECTED:
		return storage.DecisionStatusRejected
	case daemonv1.DecisionStatus_DECISION_STATUS_RETIRED:
		return storage.DecisionStatusRetired
	default:
		return ""
	}
}

func decisionStatusToProto(status string) daemonv1.DecisionStatus {
	switch status {
	case storage.DecisionStatusProposed:
		return daemonv1.DecisionStatus_DECISION_STATUS_PROPOSED
	case storage.DecisionStatusRatified:
		return daemonv1.DecisionStatus_DECISION_STATUS_RATIFIED
	case storage.DecisionStatusRejected:
		return daemonv1.DecisionStatus_DECISION_STATUS_REJECTED
	case storage.DecisionStatusRetired:
		return daemonv1.DecisionStatus_DECISION_STATUS_RETIRED
	default:
		return daemonv1.DecisionStatus_DECISION_STATUS_UNSPECIFIED
	}
}

func decisionVoteFromProto(vote daemonv1.DecisionVote) string {
	switch vote {
	case daemonv1.DecisionVote_DECISION_VOTE_APPROVE:
		return storage.DecisionVoteApprove
	case daemonv1.DecisionVote_DECISION_VOTE_REJECT:
		return storage.DecisionVoteReject
	case daemonv1.DecisionVote_DECISION_VOTE_ABSTAIN:
		return storage.DecisionVoteAbstain
	default:
		return ""
	}
}

func decisionVoteToProto(vote string) daemonv1.DecisionVote {
	switch vote {
	case storage.DecisionVoteApprove:
		return daemonv1.DecisionVote_DECISION_VOTE_APPROVE
	case storage.DecisionVoteReject:
		return daemonv1.DecisionVote_DECISION_VOTE_REJECT
	case storage.DecisionVoteAbstain:
		return daemonv1.DecisionVote_DECISION_VOTE_ABSTAIN
	default:
		return daemonv1.DecisionVote_DECISION_VOTE_UNSPECIFIED
	}
}

func actorKindToProto(kind string) daemonv1.ActorKind {
	switch strings.ToLower(kind) {
	case "agent":
		return daemonv1.ActorKind_ACTOR_KIND_AGENT
	case "human", "user":
		return daemonv1.ActorKind_ACTOR_KIND_HUMAN
	default:
		return daemonv1.ActorKind_ACTOR_KIND_UNSPECIFIED
	}
}

func decisionToProto(row storage.ChannelDecision) *daemonv1.ChannelDecision {
	return &daemonv1.ChannelDecision{
		DecisionId:           row.ID,
		Target:               row.Target,
		Title:                row.Title,
		Body:                 row.Body,
		Status:               decisionStatusToProto(row.Status),
		ProposerId:           row.ProposerID,
		ProposerKind:         actorKindToProto(row.ProposerKind),
		CreatedTimeUnix:      row.CreatedUnix,
		RatifiedTimeUnix:     row.RatifiedUnix,
		RetiredTimeUnix:      row.RetiredUnix,
		RetiredBy:            row.RetiredBy,
		RetireReason:         row.RetireReason,
		SupersedesDecisionId: row.SupersedesDecisionID,
		ApproveCount:         row.ApproveCount,
		RejectCount:          row.RejectCount,
		AbstainCount:         row.AbstainCount,
	}
}

func decisionVoteRecordToProto(row storage.ChannelDecisionVote) *daemonv1.DecisionVoteRecord {
	return &daemonv1.DecisionVoteRecord{
		DecisionId:    row.DecisionID,
		VoterId:       row.VoterID,
		VoterKind:     actorKindToProto(row.VoterKind),
		Decision:      decisionVoteToProto(row.Decision),
		VotedTimeUnix: row.VotedUnix,
		Reason:        row.Reason,
	}
}

func agentRunPhaseFromProto(phase daemonv1.AgentRunPhase) string {
	switch phase {
	case daemonv1.AgentRunPhase_AGENT_RUN_PHASE_START:
		return storage.AgentRunPhaseStart
	case daemonv1.AgentRunPhase_AGENT_RUN_PHASE_TOOL_CALL:
		return storage.AgentRunPhaseToolCall
	case daemonv1.AgentRunPhase_AGENT_RUN_PHASE_TOOL_RESULT:
		return storage.AgentRunPhaseToolResult
	case daemonv1.AgentRunPhase_AGENT_RUN_PHASE_ERROR:
		return storage.AgentRunPhaseError
	case daemonv1.AgentRunPhase_AGENT_RUN_PHASE_OUTPUT:
		return storage.AgentRunPhaseOutput
	case daemonv1.AgentRunPhase_AGENT_RUN_PHASE_END:
		return storage.AgentRunPhaseEnd
	default:
		return ""
	}
}

func agentRunPhaseToProto(phase string) daemonv1.AgentRunPhase {
	switch phase {
	case storage.AgentRunPhaseStart:
		return daemonv1.AgentRunPhase_AGENT_RUN_PHASE_START
	case storage.AgentRunPhaseToolCall:
		return daemonv1.AgentRunPhase_AGENT_RUN_PHASE_TOOL_CALL
	case storage.AgentRunPhaseToolResult:
		return daemonv1.AgentRunPhase_AGENT_RUN_PHASE_TOOL_RESULT
	case storage.AgentRunPhaseError:
		return daemonv1.AgentRunPhase_AGENT_RUN_PHASE_ERROR
	case storage.AgentRunPhaseOutput:
		return daemonv1.AgentRunPhase_AGENT_RUN_PHASE_OUTPUT
	case storage.AgentRunPhaseEnd:
		return daemonv1.AgentRunPhase_AGENT_RUN_PHASE_END
	default:
		return daemonv1.AgentRunPhase_AGENT_RUN_PHASE_UNSPECIFIED
	}
}

func agentRunSummaryToProto(row storage.AgentRun) *daemonv1.AgentRunSummary {
	return &daemonv1.AgentRunSummary{
		RunId:           row.ID,
		AgentId:         row.AgentID,
		ComputerId:      row.ComputerID,
		StartedTimeUnix: row.StartedUnix,
		EndedTimeUnix:   row.EndedUnix,
		ExitCode:        row.ExitCode,
		Summary:         row.Summary,
		ErrorMessage:    row.Error,
		EventCount:      row.EventCount,
	}
}

func agentRunEventToProto(row storage.AgentRunEvent) *daemonv1.AgentRunEvent {
	return &daemonv1.AgentRunEvent{
		RunId:        row.RunID,
		Phase:        agentRunPhaseToProto(row.Phase),
		AtUnixNano:   row.AtUnixNano,
		Summary:      row.Summary,
		PayloadJson:  []byte(row.PayloadJSON),
		ExitCode:     row.ExitCode,
		ErrorMessage: row.ErrorMessage,
	}
}

// actorFromContext pulls (actorId, actorKind) from a RequestContext. When
// absent it returns empty strings so callers can emit Unauthenticated.
// Actor carries both an agent id and a user id; we prefer whichever
// matches the declared ActorKind.
func actorFromContext(reqCtx *daemonv1.RequestContext) (string, string) {
	if reqCtx == nil {
		return "", ""
	}
	actor := reqCtx.GetActor()
	if actor == nil {
		return "", ""
	}
	kind := "human"
	id := strings.TrimSpace(actor.GetUserId())
	if actor.GetActorKind() == daemonv1.ActorKind_ACTOR_KIND_AGENT {
		kind = "agent"
		id = strings.TrimSpace(actor.GetAgentId())
	}
	if id == "" {
		return "", ""
	}
	return id, kind
}

// fmtErr is a narrow helper used only by places where we want to wrap
// errors without importing fmt in the hot-path handlers above.
func fmtErr(pfx string, err error) error { return fmt.Errorf("%s: %w", pfx, err) }

var _ = fmtErr // pacify "unused" lint during incremental development

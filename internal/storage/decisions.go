package storage

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/ent"
	"github.com/ca-x/nekode/internal/ent/channeldecision"
	"github.com/ca-x/nekode/internal/ent/channeldecisionvote"
)

// Decision statuses and vote values live as constants so callers can avoid
// typoing "retierd" into the database.
const (
	DecisionStatusProposed = "proposed"
	DecisionStatusRatified = "ratified"
	DecisionStatusRejected = "rejected"
	DecisionStatusRetired  = "retired"

	DecisionVoteApprove = "approve"
	DecisionVoteReject  = "reject"
	DecisionVoteAbstain = "abstain"
)

// validDecisionStatuses is used by status filters and validation helpers.
var validDecisionStatuses = map[string]struct{}{
	DecisionStatusProposed: {},
	DecisionStatusRatified: {},
	DecisionStatusRejected: {},
	DecisionStatusRetired:  {},
}

var validDecisionVotes = map[string]struct{}{
	DecisionVoteApprove: {},
	DecisionVoteReject:  {},
	DecisionVoteAbstain: {},
}

// MessageKind enumerates the valid message kinds. An empty string on read
// means "note"; normalizeMessageKind converts any input to one of these
// values.
const (
	MessageKindNote     = "note"
	MessageKindDecision = "decision"
	MessageKindBlocker  = "blocker"
	MessageKindStatus   = "status"
)

// normalizeMessageKind snaps any caller-provided kind value to one of the
// known labels. Unknown values fall through to "note" — callers that need
// strict validation should check upstream.
func normalizeMessageKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", MessageKindNote:
		return ""
	case MessageKindDecision:
		return MessageKindDecision
	case MessageKindBlocker:
		return MessageKindBlocker
	case MessageKindStatus:
		return MessageKindStatus
	default:
		return ""
	}
}

// CreateDecision inserts a proposed decision. Callers supply the ID only
// when they need idempotency against a retry (the ID must still be unique).
func (s *Store) CreateDecision(ctx context.Context, decision ChannelDecision) (ChannelDecision, error) {
	if decision.CreatedUnix == 0 {
		decision.CreatedUnix = unixNow()
	}
	if decision.Status == "" {
		decision.Status = DecisionStatusProposed
	}
	if _, ok := validDecisionStatuses[decision.Status]; !ok {
		return ChannelDecision{}, ErrInvalid
	}
	create := s.client.ChannelDecision.Create().
		SetTarget(decision.Target).
		SetTitle(decision.Title).
		SetBody(decision.Body).
		SetStatus(decision.Status).
		SetProposerID(decision.ProposerID).
		SetProposerKind(decision.ProposerKind).
		SetCreatedUnix(decision.CreatedUnix).
		SetRatifiedUnix(decision.RatifiedUnix).
		SetRetiredUnix(decision.RetiredUnix).
		SetRetiredBy(decision.RetiredBy).
		SetRetireReason(decision.RetireReason).
		SetSupersedesDecisionID(decision.SupersedesDecisionID).
		SetApproveCount(decision.ApproveCount).
		SetRejectCount(decision.RejectCount).
		SetAbstainCount(decision.AbstainCount)
	if decision.ID != "" {
		create.SetID(decision.ID)
	}
	row, err := create.Save(ctx)
	if ent.IsConstraintError(err) {
		return ChannelDecision{}, ErrConflict
	}
	if err != nil {
		return ChannelDecision{}, err
	}
	return decisionFromEnt(row), nil
}

// GetDecision returns a single decision by id.
func (s *Store) GetDecision(ctx context.Context, id string) (ChannelDecision, error) {
	row, err := s.client.ChannelDecision.Get(ctx, id)
	if ent.IsNotFound(err) {
		return ChannelDecision{}, ErrNotFound
	}
	if err != nil {
		return ChannelDecision{}, err
	}
	return decisionFromEnt(row), nil
}

// ListDecisions returns decisions for a target filtered by status.
func (s *Store) ListDecisions(ctx context.Context, opts ChannelDecisionListOptions) ([]ChannelDecision, error) {
	query := s.client.ChannelDecision.Query().
		Where(channeldecision.TargetEQ(opts.Target))
	if len(opts.StatusFilter) > 0 {
		query.Where(channeldecision.StatusIn(opts.StatusFilter...))
	}
	if opts.AfterCreated > 0 {
		query.Where(channeldecision.CreatedUnixGT(opts.AfterCreated))
	}
	query.Order(ent.Desc(channeldecision.FieldCreatedUnix))
	if opts.Limit > 0 {
		query.Limit(opts.Limit)
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelDecision, 0, len(rows))
	for _, row := range rows {
		out = append(out, decisionFromEnt(row))
	}
	return out, nil
}

// UpsertDecisionVote writes or updates a voter's stance and recomputes the
// parent decision's aggregate counts. Returns both the persisted vote and
// the refreshed decision (including auto-ratification bookkeeping when the
// ratification rule is satisfied).
func (s *Store) UpsertDecisionVote(ctx context.Context, vote ChannelDecisionVote) (ChannelDecision, ChannelDecisionVote, error) {
	if _, ok := validDecisionVotes[vote.Decision]; !ok {
		return ChannelDecision{}, ChannelDecisionVote{}, ErrInvalid
	}
	if vote.VotedUnix == 0 {
		vote.VotedUnix = unixNow()
	}
	tx, err := s.client.BeginTx(ctx, nil)
	if err != nil {
		return ChannelDecision{}, ChannelDecisionVote{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Only proposed decisions accept vote changes. Post-ratification votes
	// are frozen; an admin must retire and re-propose to change outcome.
	decisionRow, err := tx.ChannelDecision.Get(ctx, vote.DecisionID)
	if ent.IsNotFound(err) {
		return ChannelDecision{}, ChannelDecisionVote{}, ErrNotFound
	}
	if err != nil {
		return ChannelDecision{}, ChannelDecisionVote{}, err
	}
	if decisionRow.Status != DecisionStatusProposed {
		return ChannelDecision{}, ChannelDecisionVote{}, ErrInvalid
	}

	// Upsert the voter's row.
	existing, err := tx.ChannelDecisionVote.Query().
		Where(
			channeldecisionvote.DecisionIDEQ(vote.DecisionID),
			channeldecisionvote.VoterIDEQ(vote.VoterID),
		).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return ChannelDecision{}, ChannelDecisionVote{}, err
	}
	if existing == nil {
		create := tx.ChannelDecisionVote.Create().
			SetDecisionID(vote.DecisionID).
			SetVoterID(vote.VoterID).
			SetVoterKind(vote.VoterKind).
			SetDecision(vote.Decision).
			SetVotedUnix(vote.VotedUnix).
			SetReason(vote.Reason)
		if vote.ID != "" {
			create.SetID(vote.ID)
		}
		_, err = create.Save(ctx)
	} else {
		_, err = tx.ChannelDecisionVote.UpdateOneID(existing.ID).
			SetVoterKind(vote.VoterKind).
			SetDecision(vote.Decision).
			SetVotedUnix(vote.VotedUnix).
			SetReason(vote.Reason).
			Save(ctx)
	}
	if err != nil {
		return ChannelDecision{}, ChannelDecisionVote{}, err
	}

	// Recompute aggregates from the votes table so concurrent upserts can't
	// drift the counters.
	votes, err := tx.ChannelDecisionVote.Query().
		Where(channeldecisionvote.DecisionIDEQ(vote.DecisionID)).
		All(ctx)
	if err != nil {
		return ChannelDecision{}, ChannelDecisionVote{}, err
	}
	var approve, reject, abstain uint32
	for _, v := range votes {
		switch v.Decision {
		case DecisionVoteApprove:
			approve++
		case DecisionVoteReject:
			reject++
		case DecisionVoteAbstain:
			abstain++
		}
	}

	update := tx.ChannelDecision.UpdateOneID(vote.DecisionID).
		SetApproveCount(approve).
		SetRejectCount(reject).
		SetAbstainCount(abstain)
	ratified := decisionRow.Status
	// Quorum rule: ≥ 2 approvals and zero rejections auto-ratifies.
	if approve >= 2 && reject == 0 {
		update.SetStatus(DecisionStatusRatified).SetRatifiedUnix(unixNow())
		ratified = DecisionStatusRatified
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return ChannelDecision{}, ChannelDecisionVote{}, err
	}

	// If we auto-ratified and the decision supersedes another, retire the
	// old one atomically so we don't emit conflicting constraints.
	if ratified == DecisionStatusRatified && updated.SupersedesDecisionID != "" {
		_, err = tx.ChannelDecision.UpdateOneID(updated.SupersedesDecisionID).
			SetStatus(DecisionStatusRetired).
			SetRetiredUnix(unixNow()).
			SetRetiredBy(vote.VoterID).
			SetRetireReason("superseded-by=" + updated.ID).
			Save(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return ChannelDecision{}, ChannelDecisionVote{}, err
		}
		err = nil
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return ChannelDecision{}, ChannelDecisionVote{}, commitErr
	}

	final, err := s.GetDecision(ctx, vote.DecisionID)
	if err != nil {
		return ChannelDecision{}, ChannelDecisionVote{}, err
	}
	voteFinal := vote
	if voteFinal.ID == "" && existing != nil {
		voteFinal.ID = existing.ID
	}
	return final, voteFinal, nil
}

// ForceRatifyDecision marks a proposed decision as ratified regardless of
// quorum state — intended for admin overrides.
func (s *Store) ForceRatifyDecision(ctx context.Context, id string) (ChannelDecision, error) {
	row, err := s.client.ChannelDecision.UpdateOneID(id).
		SetStatus(DecisionStatusRatified).
		SetRatifiedUnix(unixNow()).
		Save(ctx)
	if ent.IsNotFound(err) {
		return ChannelDecision{}, ErrNotFound
	}
	if err != nil {
		return ChannelDecision{}, err
	}
	return decisionFromEnt(row), nil
}

// RetireDecision records a retirement event.
func (s *Store) RetireDecision(ctx context.Context, id, retiredBy, reason string) (ChannelDecision, error) {
	row, err := s.client.ChannelDecision.UpdateOneID(id).
		SetStatus(DecisionStatusRetired).
		SetRetiredUnix(unixNow()).
		SetRetiredBy(retiredBy).
		SetRetireReason(reason).
		Save(ctx)
	if ent.IsNotFound(err) {
		return ChannelDecision{}, ErrNotFound
	}
	if err != nil {
		return ChannelDecision{}, err
	}
	return decisionFromEnt(row), nil
}

// ListDecisionVotes returns all votes on a decision in chronological order.
func (s *Store) ListDecisionVotes(ctx context.Context, decisionID string) ([]ChannelDecisionVote, error) {
	rows, err := s.client.ChannelDecisionVote.Query().
		Where(channeldecisionvote.DecisionIDEQ(decisionID)).
		Order(ent.Asc(channeldecisionvote.FieldVotedUnix)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelDecisionVote, 0, len(rows))
	for _, row := range rows {
		out = append(out, decisionVoteFromEnt(row))
	}
	return out, nil
}

func decisionFromEnt(row *ent.ChannelDecision) ChannelDecision {
	return ChannelDecision{
		ID:                   row.ID,
		Target:               row.Target,
		Title:                row.Title,
		Body:                 row.Body,
		Status:               row.Status,
		ProposerID:           row.ProposerID,
		ProposerKind:         row.ProposerKind,
		CreatedUnix:          row.CreatedUnix,
		RatifiedUnix:         row.RatifiedUnix,
		RetiredUnix:          row.RetiredUnix,
		RetiredBy:            row.RetiredBy,
		RetireReason:         row.RetireReason,
		SupersedesDecisionID: row.SupersedesDecisionID,
		ApproveCount:         row.ApproveCount,
		RejectCount:          row.RejectCount,
		AbstainCount:         row.AbstainCount,
	}
}

func decisionVoteFromEnt(row *ent.ChannelDecisionVote) ChannelDecisionVote {
	return ChannelDecisionVote{
		ID:         row.ID,
		DecisionID: row.DecisionID,
		VoterID:    row.VoterID,
		VoterKind:  row.VoterKind,
		Decision:   row.Decision,
		VotedUnix:  row.VotedUnix,
		Reason:     row.Reason,
	}
}

// ErrInvalid is returned when a caller provides a value that doesn't match
// the enum contract (status, vote, message kind, phase).
var ErrInvalid = errors.New("storage: invalid argument")

// helper for code that doesn't already have unixNow in scope
func unixNowOr(t time.Time) int64 {
	if t.IsZero() {
		return unixNow()
	}
	return t.Unix()
}

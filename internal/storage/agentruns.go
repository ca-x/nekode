package storage

import (
	"context"
	"strings"

	"github.com/ca-x/nekode/internal/ent"
	"github.com/ca-x/nekode/internal/ent/agentrun"
	"github.com/ca-x/nekode/internal/ent/agentrunevent"
)

// Agent run phases. Mirror AgentRunPhase in proto.
const (
	AgentRunPhaseStart      = "start"
	AgentRunPhaseToolCall   = "tool_call"
	AgentRunPhaseToolResult = "tool_result"
	AgentRunPhaseError      = "error"
	AgentRunPhaseOutput     = "output"
	AgentRunPhaseEnd        = "end"
)

var validAgentRunPhases = map[string]struct{}{
	AgentRunPhaseStart:      {},
	AgentRunPhaseToolCall:   {},
	AgentRunPhaseToolResult: {},
	AgentRunPhaseError:      {},
	AgentRunPhaseOutput:     {},
	AgentRunPhaseEnd:        {},
}

// AppendAgentRunEvent persists one lifecycle event. It creates the run row
// on the first START event and finalizes it on END. Other phases require
// the run to already exist.
func (s *Store) AppendAgentRunEvent(ctx context.Context, event AgentRunEvent, runInfo AgentRun) (AgentRun, AgentRunEvent, error) {
	phase := strings.ToLower(strings.TrimSpace(event.Phase))
	if _, ok := validAgentRunPhases[phase]; !ok {
		return AgentRun{}, AgentRunEvent{}, ErrInvalid
	}
	event.Phase = phase

	tx, err := s.client.BeginTx(ctx, nil)
	if err != nil {
		return AgentRun{}, AgentRunEvent{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Find or create the run row.
	run, err := tx.AgentRun.Get(ctx, event.RunID)
	switch {
	case ent.IsNotFound(err):
		if phase != AgentRunPhaseStart {
			// First event for a run must be START; anything else is a
			// protocol violation or a lost start event we refuse to patch
			// over silently.
			return AgentRun{}, AgentRunEvent{}, ErrInvalid
		}
		run, err = tx.AgentRun.Create().
			SetID(event.RunID).
			SetAgentID(runInfo.AgentID).
			SetComputerID(runInfo.ComputerID).
			SetStartedUnix(runInfo.StartedUnix).
			Save(ctx)
		if err != nil {
			return AgentRun{}, AgentRunEvent{}, err
		}
	case err != nil:
		return AgentRun{}, AgentRunEvent{}, err
	}

	// Persist the event.
	eventCreate := tx.AgentRunEvent.Create().
		SetRunID(event.RunID).
		SetAtUnixNano(event.AtUnixNano).
		SetPhase(event.Phase).
		SetSummary(event.Summary).
		SetPayloadJSON(event.PayloadJSON).
		SetExitCode(event.ExitCode).
		SetErrorMessage(event.ErrorMessage)
	if event.ID != "" {
		eventCreate.SetID(event.ID)
	}
	eventRow, err := eventCreate.Save(ctx)
	if err != nil {
		return AgentRun{}, AgentRunEvent{}, err
	}

	// Update the run row: bump event_count, finalize on END.
	update := tx.AgentRun.UpdateOneID(event.RunID).AddEventCount(1)
	if phase == AgentRunPhaseEnd {
		update.
			SetEndedUnix(runInfo.EndedUnix).
			SetExitCode(event.ExitCode).
			SetError(event.ErrorMessage).
			SetSummary(event.Summary)
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return AgentRun{}, AgentRunEvent{}, err
	}
	_ = run
	if commitErr := tx.Commit(); commitErr != nil {
		return AgentRun{}, AgentRunEvent{}, commitErr
	}
	return agentRunFromEnt(updated), agentRunEventFromEnt(eventRow), nil
}

// ListAgentRuns returns a filtered list of runs, newest-first.
func (s *Store) ListAgentRuns(ctx context.Context, opts AgentRunListOptions) ([]AgentRun, error) {
	query := s.client.AgentRun.Query()
	if opts.AgentID != "" {
		query.Where(agentrun.AgentIDEQ(opts.AgentID))
	}
	if opts.ComputerID != "" {
		query.Where(agentrun.ComputerIDEQ(opts.ComputerID))
	}
	if opts.BeforeStarted > 0 {
		query.Where(agentrun.StartedUnixLT(opts.BeforeStarted))
	}
	query.Order(ent.Desc(agentrun.FieldStartedUnix))
	if opts.Limit > 0 {
		query.Limit(opts.Limit)
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AgentRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentRunFromEnt(row))
	}
	return out, nil
}

// GetAgentRun returns the run record plus optionally all its events.
func (s *Store) GetAgentRun(ctx context.Context, runID string, includeEvents bool) (AgentRun, []AgentRunEvent, error) {
	row, err := s.client.AgentRun.Get(ctx, runID)
	if ent.IsNotFound(err) {
		return AgentRun{}, nil, ErrNotFound
	}
	if err != nil {
		return AgentRun{}, nil, err
	}
	run := agentRunFromEnt(row)
	if !includeEvents {
		return run, nil, nil
	}
	eventRows, err := s.client.AgentRunEvent.Query().
		Where(agentrunevent.RunIDEQ(runID)).
		Order(ent.Asc(agentrunevent.FieldAtUnixNano)).
		All(ctx)
	if err != nil {
		return run, nil, err
	}
	events := make([]AgentRunEvent, 0, len(eventRows))
	for _, event := range eventRows {
		events = append(events, agentRunEventFromEnt(event))
	}
	return run, events, nil
}

// SearchAgentRuns performs a LIKE-based fallback over summary + payload
// until FTS5 virtual tables are wired. The query escapes % and _ so user
// input can't produce unexpected matches.
func (s *Store) SearchAgentRuns(ctx context.Context, opts AgentRunSearchOptions) ([]AgentRunSearchHit, error) {
	query := s.client.AgentRunEvent.Query()
	if opts.Query != "" {
		escaped := strings.NewReplacer("%", `\%`, "_", `\_`).Replace(opts.Query)
		pattern := "%" + escaped + "%"
		query.Where(agentrunevent.Or(
			agentrunevent.SummaryContainsFold(pattern),
			agentrunevent.PayloadJSONContainsFold(pattern),
		))
	}
	query.Order(ent.Desc(agentrunevent.FieldAtUnixNano))
	if opts.Limit > 0 {
		query.Limit(opts.Limit)
	}
	eventRows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	hits := make([]AgentRunSearchHit, 0, len(eventRows))
	for _, event := range eventRows {
		runRow, err := s.client.AgentRun.Get(ctx, event.RunID)
		if err != nil {
			continue
		}
		if opts.AgentID != "" && runRow.AgentID != opts.AgentID {
			continue
		}
		if opts.ComputerID != "" && runRow.ComputerID != opts.ComputerID {
			continue
		}
		hits = append(hits, AgentRunSearchHit{
			Run:       agentRunFromEnt(runRow),
			Event:     agentRunEventFromEnt(event),
			Highlight: event.Summary,
		})
	}
	return hits, nil
}

func agentRunFromEnt(row *ent.AgentRun) AgentRun {
	return AgentRun{
		ID:          row.ID,
		AgentID:     row.AgentID,
		ComputerID:  row.ComputerID,
		StartedUnix: row.StartedUnix,
		EndedUnix:   row.EndedUnix,
		ExitCode:    row.ExitCode,
		Summary:     row.Summary,
		Error:       row.Error,
		EventCount:  row.EventCount,
	}
}

func agentRunEventFromEnt(row *ent.AgentRunEvent) AgentRunEvent {
	return AgentRunEvent{
		ID:           row.ID,
		RunID:        row.RunID,
		AtUnixNano:   row.AtUnixNano,
		Phase:        row.Phase,
		Summary:      row.Summary,
		PayloadJSON:  row.PayloadJSON,
		ExitCode:     row.ExitCode,
		ErrorMessage: row.ErrorMessage,
	}
}

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ca-x/nekode/internal/storage"
)

// --- decision request types ---------------------------------------------

type proposeDecisionRequest struct {
	Title                string `json:"title"`
	Body                 string `json:"body"`
	SupersedesDecisionID string `json:"supersedesDecisionId"`
}

type voteDecisionRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type retireDecisionRequest struct {
	Reason string `json:"reason"`
}

type ratifyDecisionRequest struct {
	Force bool `json:"force"`
}

// --- decision handlers ---------------------------------------------------

// handleListChannelDecisions returns decisions scoped to a channel target.
// Accepts optional ?status=proposed,ratified filter and ?limit=N.
func (s *Server) handleListChannelDecisions(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.PathValue("target"))
	if target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}
	var statusFilter []string
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			if entry != "" {
				statusFilter = append(statusFilter, entry)
			}
		}
	}
	rows, err := s.store.ListDecisions(r.Context(), storage.ChannelDecisionListOptions{
		Target:       target,
		StatusFilter: statusFilter,
		Limit:        intQuery(r, "limit", 100),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list decisions failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

// handleProposeChannelDecision creates a decision row in `proposed` state.
func (s *Server) handleProposeChannelDecision(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.PathValue("target"))
	if target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}
	var req proposeDecisionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	if title == "" || body == "" {
		writeError(w, http.StatusBadRequest, "title and body are required")
		return
	}
	principal := principalFromContext(r.Context())
	decision, err := s.store.CreateDecision(r.Context(), storage.ChannelDecision{
		Target:               target,
		Title:                title,
		Body:                 body,
		Status:               storage.DecisionStatusProposed,
		ProposerID:           principal.User.ID,
		ProposerKind:         "human",
		SupersedesDecisionID: strings.TrimSpace(req.SupersedesDecisionID),
	})
	if errors.Is(err, storage.ErrInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create decision failed")
		return
	}
	writeJSON(w, http.StatusCreated, decision)
}

// handleVoteChannelDecision records or updates the signed-in user's vote.
// The response always includes the refreshed decision so the client can
// show the fresh tally (and auto-ratify state) without a second round-trip.
func (s *Server) handleVoteChannelDecision(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "decision id is required")
		return
	}
	var req voteDecisionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	principal := principalFromContext(r.Context())
	decision, vote, err := s.store.UpsertDecisionVote(r.Context(), storage.ChannelDecisionVote{
		DecisionID: id,
		VoterID:    principal.User.ID,
		VoterKind:  "human",
		Decision:   strings.TrimSpace(req.Decision),
		Reason:     strings.TrimSpace(req.Reason),
	})
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "decision not found")
		return
	}
	if errors.Is(err, storage.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "decision is not open for voting or vote value is invalid")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "vote failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": decision, "vote": vote})
}

// handleRatifyChannelDecision promotes a proposed decision to ratified
// either when quorum is met or when `force` is requested by an admin.
func (s *Server) handleRatifyChannelDecision(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "decision id is required")
		return
	}
	req := ratifyDecisionRequest{}
	// Body is optional; ignore decode errors when the body is empty.
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	current, err := s.store.GetDecision(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "decision not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load decision failed")
		return
	}
	if current.Status != storage.DecisionStatusProposed {
		writeError(w, http.StatusConflict, "decision is not proposed")
		return
	}
	principal := principalFromContext(r.Context())
	isAdmin := strings.EqualFold(principal.User.Role, "admin")
	if req.Force && !isAdmin {
		writeError(w, http.StatusForbidden, "admin role required to force ratify")
		return
	}
	quorumMet := current.ApproveCount >= 2 && current.RejectCount == 0
	if !req.Force && !quorumMet {
		writeError(w, http.StatusPreconditionFailed, "quorum not met")
		return
	}
	ratified, err := s.store.ForceRatifyDecision(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ratify failed")
		return
	}
	writeJSON(w, http.StatusOK, ratified)
}

// handleRetireChannelDecision marks a proposed / ratified decision retired.
// Anyone who can reach the channel can retire; body carries a reason.
func (s *Server) handleRetireChannelDecision(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "decision id is required")
		return
	}
	req := retireDecisionRequest{}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	current, err := s.store.GetDecision(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "decision not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load decision failed")
		return
	}
	if current.Status != storage.DecisionStatusProposed && current.Status != storage.DecisionStatusRatified {
		writeError(w, http.StatusConflict, "decision is already closed")
		return
	}
	principal := principalFromContext(r.Context())
	retired, err := s.store.RetireDecision(r.Context(), id, principal.User.ID, strings.TrimSpace(req.Reason))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "retire failed")
		return
	}
	writeJSON(w, http.StatusOK, retired)
}

// handleListDecisionVotes returns audit-style vote history for a decision.
func (s *Server) handleListDecisionVotes(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "decision id is required")
		return
	}
	votes, err := s.store.ListDecisionVotes(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list votes failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": votes})
}

// --- agent runs handlers -------------------------------------------------

// handleListAgentRuns returns recent runs, optionally filtered by agent
// or computer id.
func (s *Server) handleListAgentRuns(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListAgentRuns(r.Context(), storage.AgentRunListOptions{
		AgentID:    strings.TrimSpace(r.URL.Query().Get("agentId")),
		ComputerID: strings.TrimSpace(r.URL.Query().Get("computerId")),
		Limit:      intQuery(r, "limit", 50),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list runs failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

// handleGetAgentRun returns a run by id. Include events when ?events=1.
func (s *Server) handleGetAgentRun(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "run id is required")
		return
	}
	include := r.URL.Query().Get("events") == "1" || r.URL.Query().Get("events") == "true"
	run, events, err := s.store.GetAgentRun(r.Context(), id, include)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get run failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run, "events": events})
}

// handleSearchAgentRuns runs a substring search over event summary / payload.
// FTS5 swap lands later; the API shape stays stable.
func (s *Server) handleSearchAgentRuns(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	hits, err := s.store.SearchAgentRuns(r.Context(), storage.AgentRunSearchOptions{
		Query:      query,
		AgentID:    strings.TrimSpace(r.URL.Query().Get("agentId")),
		ComputerID: strings.TrimSpace(r.URL.Query().Get("computerId")),
		Limit:      intQuery(r, "limit", 50),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search runs failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hits})
}

package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/storage"
)

// --- request types ------------------------------------------------------

type createTunnelRequest struct {
	ComputerID   string `json:"computerId"`
	LocalPort    uint32 `json:"localPort"`
	Label        string `json:"label"`
	AccessPolicy string `json:"accessPolicy"`
	TTLSeconds   uint32 `json:"ttlSeconds"`
}

type tunnelActionRequest struct {
	Reason string `json:"reason"`
}

// --- defaults and limits ------------------------------------------------

const (
	tunnelDefaultTTLSeconds uint32 = 2 * 60 * 60  // 2 hours
	tunnelMaxTTLSeconds     uint32 = 24 * 60 * 60 // 24 hours
)

// --- handlers -----------------------------------------------------------

// handleCreateTunnel opens a new tunnel record. Only admins can create
// tunnels through the REST path — they're the only users with a durable
// trust relationship with every computer in the workspace, so gating
// this endpoint on the admin role is the simplest correct approximation
// of "the person allowed to publish a port on this machine" until the
// Computer entity grows an explicit owner field.
//
// Agent-initiated tunnels arrive over gRPC (daemonrpc.CreateTunnel),
// always land in pending_approval, and require an admin to release them
// — so the agent path does not bypass this check.
func (s *Server) handleCreateTunnel(w http.ResponseWriter, r *http.Request) {
	var req createTunnelRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	computerID := strings.TrimSpace(req.ComputerID)
	if computerID == "" || req.LocalPort == 0 {
		writeError(w, http.StatusBadRequest, "computerId and localPort are required")
		return
	}
	principal := principalFromContext(r.Context())
	if !strings.EqualFold(principal.User.Role, "admin") {
		writeError(w, http.StatusForbidden, "admin role required to create tunnels")
		return
	}
	policy := strings.TrimSpace(req.AccessPolicy)
	if policy == "" {
		policy = storage.TunnelAccessPolicyMembers
	}
	ttl := req.TTLSeconds
	if ttl == 0 {
		ttl = tunnelDefaultTTLSeconds
	}
	if ttl > tunnelMaxTTLSeconds {
		ttl = tunnelMaxTTLSeconds
	}
	// Admin creator → active on creation per locked policy in
	// docs/preview-tunnels-design.md §Locked call boundaries.
	state := storage.TunnelStateActive
	now := time.Now().Unix()
	record, err := s.store.CreateTunnel(r.Context(), storage.TunnelRecord{
		ComputerID:   computerID,
		LocalPort:    req.LocalPort,
		Label:        strings.TrimSpace(req.Label),
		State:        state,
		AccessPolicy: policy,
		CreatorID:    principal.User.ID,
		CreatorKind:  "human",
		ExpiresUnix:  now + int64(ttl),
		ApprovedUnix: now,
		ApprovedBy:   principal.User.ID,
	})
	if errors.Is(err, storage.ErrInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create tunnel failed")
		return
	}
	// Build the public URL from the inbound request host so operators
	// fronted by a reverse proxy get the externally-reachable URL back.
	record = populatePublicURL(r, record)
	writeJSON(w, http.StatusCreated, record)
}

// handleListTunnels returns tunnels scoped by computer id. Admins see
// every tunnel (optionally filtered); non-admins see only tunnels they
// created, regardless of the query parameters — this prevents the
// endpoint from leaking labels, ports, and creator IDs to every
// authenticated workspace member.
func (s *Server) handleListTunnels(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	principal := principalFromContext(r.Context())
	isAdmin := strings.EqualFold(principal.User.Role, "admin")
	opts := storage.TunnelListOptions{
		ComputerID:  strings.TrimSpace(q.Get("computerId")),
		StateFilter: strings.TrimSpace(q.Get("state")),
		Limit:       intQuery(r, "limit", 50),
	}
	rows, err := s.store.ListTunnels(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list tunnels failed")
		return
	}
	out := make([]storage.TunnelRecord, 0, len(rows))
	for _, row := range rows {
		if !isAdmin && row.CreatorID != principal.User.ID {
			continue
		}
		row.Token = "" // never return tokens in a list response
		row = populatePublicURL(r, row)
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// handleApproveTunnel flips a pending_approval tunnel to active. Only an
// admin or the computer owner can approve; everyone else gets 403.
func (s *Server) handleApproveTunnel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	principal := principalFromContext(r.Context())
	if !strings.EqualFold(principal.User.Role, "admin") {
		writeError(w, http.StatusForbidden, "admin role required to approve tunnels")
		return
	}
	updated, err := s.store.UpdateTunnelState(r.Context(), id, storage.TunnelStateActive, principal.User.ID, "")
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}
	if errors.Is(err, storage.ErrInvalid) {
		writeError(w, http.StatusConflict, "tunnel is not pending approval")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "approve tunnel failed")
		return
	}
	updated.Token = ""
	writeJSON(w, http.StatusOK, populatePublicURL(r, updated))
}

// handleRejectTunnel marks a pending_approval tunnel as rejected.
func (s *Server) handleRejectTunnel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	principal := principalFromContext(r.Context())
	if !strings.EqualFold(principal.User.Role, "admin") {
		writeError(w, http.StatusForbidden, "admin role required to reject tunnels")
		return
	}
	req := tunnelActionRequest{}
	if r.ContentLength > 0 {
		_ = decodeJSON(w, r, &req)
	}
	updated, err := s.store.UpdateTunnelState(r.Context(), id, storage.TunnelStateRejected, principal.User.ID, strings.TrimSpace(req.Reason))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}
	if errors.Is(err, storage.ErrInvalid) {
		writeError(w, http.StatusConflict, "tunnel is not pending approval")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reject tunnel failed")
		return
	}
	updated.Token = ""
	writeJSON(w, http.StatusOK, updated)
}

// handleCloseTunnel terminates an active or pending tunnel. The tunnel
// creator and any admin can close; anyone else is forbidden.
func (s *Server) handleCloseTunnel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	principal := principalFromContext(r.Context())
	current, err := s.store.GetTunnel(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load tunnel failed")
		return
	}
	isAdmin := strings.EqualFold(principal.User.Role, "admin")
	isCreator := current.CreatorID == principal.User.ID
	if !isAdmin && !isCreator {
		writeError(w, http.StatusForbidden, "only the creator or an admin can close this tunnel")
		return
	}
	req := tunnelActionRequest{}
	if r.ContentLength > 0 {
		_ = decodeJSON(w, r, &req)
	}
	updated, err := s.store.UpdateTunnelState(r.Context(), id, storage.TunnelStateClosed, principal.User.ID, strings.TrimSpace(req.Reason))
	if errors.Is(err, storage.ErrInvalid) {
		writeError(w, http.StatusConflict, "tunnel is already closed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "close tunnel failed")
		return
	}
	updated.Token = ""
	writeJSON(w, http.StatusOK, updated)
}

// --- helpers ------------------------------------------------------------

// populatePublicURL fills record.PublicURL from the request's externally-
// visible scheme+host when a token is present. List responses strip tokens
// before calling this, so the URL is left blank for non-creators — the UI
// shows "hidden" rather than leaking a URL that other viewers can't use.
func populatePublicURL(r *http.Request, record storage.TunnelRecord) storage.TunnelRecord {
	if record.Token == "" {
		return record
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := r.Host
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	record.PublicURL = scheme + "://" + host + "/preview/" + record.Token + "/"
	return record
}

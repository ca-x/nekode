package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/daemonrpc"
	"github.com/ca-x/nekode/internal/storage"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) handleDaemonInfo(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	statuses, _ := s.daemon.ListAgentStatuses(r.Context(), &daemonv1.ListAgentStatusesRequest{Limit: 200})
	runs, _ := s.daemon.ListRuns(r.Context(), &daemonv1.ListRunsRequest{Limit: 200})
	activities, _ := s.daemon.ListActivity(r.Context(), &daemonv1.ListActivityRequest{Limit: 200})
	health := "ok"
	if len(statuses.GetStatuses()) == 0 {
		health = "idle"
	}
	for _, status := range statuses.GetStatuses() {
		if agentStatusNeedsAttention(status) {
			health = "degraded"
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"serverId":           s.daemon.ServerID(),
		"serverName":         s.daemon.ServerName(),
		"protocolVersion":    s.daemon.ProtocolVersion(),
		"minProtocolVersion": s.daemon.ProtocolVersion(),
		"maxProtocolVersion": s.daemon.ProtocolVersion(),
		"grpcAddr":           s.cfg.GRPCAddr,
		"daemonTransport":    s.cfg.DaemonTransport,
		"cacheDriver":        s.cfg.CacheDriver,
		"serverTimeUnix":     time.Now().Unix(),
		"health":             health,
		"agentStatusCount":   len(statuses.GetStatuses()),
		"runCount":           len(runs.GetRuns()),
		"activityCount":      len(activities.GetActivities()),
	})
}

func agentStatusNeedsAttention(status *daemonv1.AgentStatusSnapshot) bool {
	if status.GetHealth() != daemonv1.AgentHealth_AGENT_HEALTH_OK &&
		status.GetHealth() != daemonv1.AgentHealth_AGENT_HEALTH_UNSPECIFIED {
		return true
	}
	switch status.GetPresence() {
	case daemonv1.AgentPresence_AGENT_PRESENCE_STALE,
		daemonv1.AgentPresence_AGENT_PRESENCE_OFFLINE,
		daemonv1.AgentPresence_AGENT_PRESENCE_DEGRADED:
		return true
	default:
		return false
	}
}

func (s *Server) handleDaemonInventory(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": s.daemon.ListComputerInventories(intQuery(r, "limit", 100)),
	})
}

func (s *Server) handleCreateDaemonAgent(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	var input daemonrpc.CreateAgentInstanceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.daemon.CreateAgentInstance(input)
	if err != nil {
		writeError(w, daemonAgentHTTPStatus(err), grpcstatus.Convert(err).Message())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleControlDaemonAgent(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	agentID := strings.TrimSpace(r.PathValue("agentID"))
	var req daemonAgentControlRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	action, ok := daemonAgentControlAction(req.Action)
	if agentID == "" || !ok {
		writeError(w, http.StatusBadRequest, "agentID and valid action are required")
		return
	}
	requestID := firstNonEmptyString(strings.TrimSpace(req.RequestID), storage.NewID("ctlreq"))
	resp, err := s.daemon.ControlAgent(r.Context(), &daemonv1.ControlAgentRequest{
		AgentId:          agentID,
		ComputerId:       strings.TrimSpace(req.ComputerID),
		RuntimeProfileId: strings.TrimSpace(req.RuntimeProfileID),
		Action:           action,
		Reason:           strings.TrimSpace(req.Reason),
		RequestId:        requestID,
		IdempotencyKey:   requestID,
		Context:          daemonHTTPContext(r),
	})
	if err != nil {
		writeError(w, daemonAgentHTTPStatus(err), grpcstatus.Convert(err).Message())
		return
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (s *Server) handleSendDaemonAgentDirectMessage(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	agentID := strings.TrimSpace(r.PathValue("agentID"))
	var req daemonAgentDirectMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	content := strings.TrimSpace(req.Content)
	if agentID == "" || content == "" {
		writeError(w, http.StatusBadRequest, "agentID and content are required")
		return
	}
	principal := principalFromContext(r.Context())
	requestID := firstNonEmptyString(strings.TrimSpace(req.RequestID), storage.NewID("dmreq"))
	resp, err := s.daemon.SendAgentDirectMessage(r.Context(), &daemonv1.SendAgentDirectMessageRequest{
		AgentId:          agentID,
		Content:          content,
		RequestId:        requestID,
		IdempotencyKey:   requestID,
		AttachmentIds:    append([]string(nil), req.AttachmentIDs...),
		ReplyToMessageId: strings.TrimSpace(req.ReplyToMessageID),
		Context:          daemonHTTPContext(r),
		Sender: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_HUMAN,
			UserId:      principal.User.ID,
			DisplayName: firstNonEmptyString(principal.User.DisplayName, principal.User.Username, "User"),
		},
	})
	if err != nil {
		writeError(w, daemonAgentHTTPStatus(err), grpcstatus.Convert(err).Message())
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func daemonAgentHTTPStatus(err error) int {
	switch grpcstatus.Code(err) {
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.AlreadyExists:
		return http.StatusConflict
	case codes.NotFound:
		return http.StatusNotFound
	case codes.FailedPrecondition:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func daemonAgentControlAction(value string) (daemonv1.AgentControlAction, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "terminate", "agent_control_action_terminate":
		return daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_TERMINATE, true
	case "restart", "agent_control_action_restart":
		return daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART, true
	case "restart_reset_session", "agent_control_action_restart_reset_session":
		return daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART_RESET_SESSION, true
	case "restart_full_reset", "agent_control_action_restart_full_reset":
		return daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_RESTART_FULL_RESET, true
	default:
		return daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_UNSPECIFIED, false
	}
}

func daemonHTTPContext(r *http.Request) *daemonv1.RequestContext {
	principal := principalFromContext(r.Context())
	return &daemonv1.RequestContext{
		TraceId: storage.NewID("trace"),
		Actor: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_HUMAN,
			UserId:      principal.User.ID,
			DisplayName: firstNonEmptyString(principal.User.DisplayName, principal.User.Username, "User"),
		},
	}
}

func (s *Server) handleDaemonAgentStatuses(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	resp, err := s.daemon.ListAgentStatuses(r.Context(), &daemonv1.ListAgentStatusesRequest{
		AgentId: strings.TrimSpace(r.URL.Query().Get("agentId")),
		Target:  strings.TrimSpace(r.URL.Query().Get("target")),
		Limit:   uint32(intQuery(r, "limit", 100)),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list daemon agent statuses failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      resp.GetStatuses(),
		"nextCursor": resp.GetNextCursor(),
	})
}

func (s *Server) handleDaemonActivity(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	resp, err := s.daemon.ListActivity(r.Context(), &daemonv1.ListActivityRequest{
		Target:  strings.TrimSpace(r.URL.Query().Get("target")),
		AgentId: strings.TrimSpace(r.URL.Query().Get("agentId")),
		Limit:   uint32(intQuery(r, "limit", 100)),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list daemon activity failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      resp.GetActivities(),
		"nextCursor": resp.GetNextCursor(),
	})
}

func (s *Server) handleDaemonRuns(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	resp, err := s.daemon.ListRuns(r.Context(), &daemonv1.ListRunsRequest{
		Target:  strings.TrimSpace(r.URL.Query().Get("target")),
		TaskId:  strings.TrimSpace(r.URL.Query().Get("taskId")),
		AgentId: strings.TrimSpace(r.URL.Query().Get("agentId")),
		Limit:   uint32(intQuery(r, "limit", 100)),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list daemon runs failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      resp.GetRuns(),
		"nextCursor": resp.GetNextCursor(),
	})
}

func (s *Server) handleDaemonEvents(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	resp, err := s.daemon.ListEventsSince(r.Context(), &daemonv1.ListEventsSinceRequest{
		Cursor: &daemonv1.EventCursor{
			Target:          strings.TrimSpace(r.URL.Query().Get("target")),
			AggregateId:     strings.TrimSpace(r.URL.Query().Get("aggregateId")),
			Sequence:        int64Query(r, "sequence", 0),
			ProtocolVersion: s.daemon.ProtocolVersion(),
			ServerId:        s.daemon.ServerID(),
		},
		Limit: uint32(intQuery(r, "limit", 100)),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list daemon events failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      resp.GetEvents(),
		"nextCursor": resp.GetNextCursor(),
	})
}

func (s *Server) handleCreateDaemonEnrollment(w http.ResponseWriter, r *http.Request) {
	if s.daemonEnrollments == nil {
		writeError(w, http.StatusServiceUnavailable, errDaemonEnrollmentNotReady.Error())
		return
	}
	var req daemonEnrollmentCreate
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ExpiresUnix > 0 && req.ExpiresUnix <= time.Now().Unix() {
		writeError(w, http.StatusBadRequest, "expiresUnix must be in the future")
		return
	}
	enrollment, token, installCode, err := s.daemonEnrollments.create(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create daemon enrollment failed")
		return
	}
	writeJSON(w, http.StatusCreated, s.daemonEnrollmentResponse(r, enrollment, token, installCode))
}

func (s *Server) handleGetDaemonEnrollment(w http.ResponseWriter, r *http.Request) {
	if s.daemonEnrollments == nil {
		writeError(w, http.StatusServiceUnavailable, errDaemonEnrollmentNotReady.Error())
		return
	}
	enrollment, err := s.daemonEnrollments.get(r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "daemon enrollment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get daemon enrollment failed")
		return
	}
	writeJSON(w, http.StatusOK, s.daemonEnrollmentResponse(r, enrollment, "", ""))
}

func (s *Server) handleRevokeDaemonEnrollment(w http.ResponseWriter, r *http.Request) {
	if s.daemonEnrollments == nil {
		writeError(w, http.StatusServiceUnavailable, errDaemonEnrollmentNotReady.Error())
		return
	}
	enrollment, err := s.daemonEnrollments.revoke(r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "daemon enrollment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "revoke daemon enrollment failed")
		return
	}
	writeJSON(w, http.StatusOK, s.daemonEnrollmentResponse(r, enrollment, "", ""))
}

func (s *Server) handleDaemonEnrollmentInstallShell(w http.ResponseWriter, r *http.Request) {
	s.handleDaemonEnrollmentInstallScript(w, r, "sh")
}

func (s *Server) handleDaemonEnrollmentInstallPowerShell(w http.ResponseWriter, r *http.Request) {
	s.handleDaemonEnrollmentInstallScript(w, r, "ps1")
}

func (s *Server) handleDaemonUpgradeShell(w http.ResponseWriter, r *http.Request) {
	s.handleDaemonManagementScript(w, r, "upgrade", "sh")
}

func (s *Server) handleDaemonReinstallShell(w http.ResponseWriter, r *http.Request) {
	s.handleDaemonManagementScript(w, r, "reinstall", "sh")
}

func (s *Server) handleDaemonUninstallShell(w http.ResponseWriter, r *http.Request) {
	s.handleDaemonManagementScript(w, r, "uninstall", "sh")
}

func (s *Server) handleDaemonUpgradePowerShell(w http.ResponseWriter, r *http.Request) {
	s.handleDaemonManagementScript(w, r, "upgrade", "ps1")
}

func (s *Server) handleDaemonReinstallPowerShell(w http.ResponseWriter, r *http.Request) {
	s.handleDaemonManagementScript(w, r, "reinstall", "ps1")
}

func (s *Server) handleDaemonUninstallPowerShell(w http.ResponseWriter, r *http.Request) {
	s.handleDaemonManagementScript(w, r, "uninstall", "ps1")
}

func (s *Server) handleDaemonManagementScript(w http.ResponseWriter, _ *http.Request, action, scriptKind string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch scriptKind {
	case "ps1":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(s.renderDaemonManagementPowerShell(action)))
	default:
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		_, _ = w.Write([]byte(s.renderDaemonManagementShell(action)))
	}
}

type daemonAgentControlRequest struct {
	Action           string `json:"action"`
	Reason           string `json:"reason"`
	ComputerID       string `json:"computerId"`
	RuntimeProfileID string `json:"runtimeProfileId"`
	RequestID        string `json:"requestId"`
}

type daemonAgentDirectMessageRequest struct {
	Content          string   `json:"content"`
	ReplyToMessageID string   `json:"replyToMessageId"`
	AttachmentIDs    []string `json:"attachmentIds"`
	RequestID        string   `json:"requestId"`
}

func (s *Server) handleDaemonEnrollmentInstallScript(w http.ResponseWriter, r *http.Request, scriptKind string) {
	if s.daemonEnrollments == nil {
		writeError(w, http.StatusServiceUnavailable, errDaemonEnrollmentNotReady.Error())
		return
	}
	enrollment, token, err := s.daemonEnrollments.consumeInstallCode(r.PathValue("id"), r.URL.Query().Get("code"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "daemon install code not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create daemon install script failed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch scriptKind {
	case "ps1":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(s.renderDaemonInstallPowerShell(r, enrollment, token)))
	default:
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		_, _ = w.Write([]byte(s.renderDaemonInstallShell(r, enrollment, token)))
	}
}

func (s *Server) handleServerEvents(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is unsupported")
		return
	}

	cursor := eventCursorFromRequest(r, s.daemon.ProtocolVersion(), s.daemon.ServerID())
	if cursor.GetSequence() == 0 {
		cursor.Sequence = sequenceFromCursor(cursor.GetCursor())
	}
	filterTarget := cursor.GetTarget()
	filterAggregateID := cursor.GetAggregateId()
	limit := uint32(intQuery(r, "limit", 100))
	if limit == 0 || limit > 200 {
		limit = 100
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_, _ = fmt.Fprint(w, "retry: 5000\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		resp, err := s.daemon.ListEventsSince(r.Context(), &daemonv1.ListEventsSinceRequest{
			Cursor: cursor,
			Limit:  limit,
		})
		if err != nil {
			writeSSE(w, "error", "", map[string]string{"error": "list daemon events failed"})
			flusher.Flush()
			return
		}
		for _, event := range resp.GetEvents() {
			writeSSE(w, "message", event.GetEventId(), event)
			cursor = &daemonv1.EventCursor{
				Cursor:          cursorString(event.GetSequence(), filterTarget, filterAggregateID),
				Target:          filterTarget,
				AggregateId:     filterAggregateID,
				Sequence:        event.GetSequence(),
				ProtocolVersion: s.daemon.ProtocolVersion(),
				ServerId:        s.daemon.ServerID(),
			}
		}
		flusher.Flush()
		if len(resp.GetEvents()) > 0 {
			continue
		}

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if len(resp.GetEvents()) == 0 {
				writeSSE(w, "ping", "", map[string]int64{"serverTimeUnix": time.Now().Unix()})
				flusher.Flush()
			}
		}
	}
}

func cursorString(sequence int64, target, aggregateID string) string {
	scope := strings.TrimSpace(target)
	if scope == "" {
		scope = strings.TrimSpace(aggregateID)
	}
	if scope == "" {
		return strconv.FormatInt(sequence, 10)
	}
	return fmt.Sprintf("%s:%d", scope, sequence)
}

func eventCursorFromRequest(r *http.Request, protocolVersion int32, serverID string) *daemonv1.EventCursor {
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	aggregateID := strings.TrimSpace(r.URL.Query().Get("aggregateId"))
	return &daemonv1.EventCursor{
		Cursor:          strings.TrimSpace(r.URL.Query().Get("cursor")),
		Target:          target,
		AggregateId:     aggregateID,
		Sequence:        int64Query(r, "sequence", 0),
		ProtocolVersion: protocolVersion,
		ServerId:        serverID,
	}
}

func writeSSE(w http.ResponseWriter, eventType, id string, value any) {
	if id != "" {
		_, _ = fmt.Fprintf(w, "id: %s\n", id)
	}
	if eventType != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", eventType)
	}
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(`{"error":"encode event failed"}`)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func sequenceFromCursor(cursor string) int64 {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0
	}
	raw := cursor
	if idx := strings.LastIndex(cursor, ":"); idx >= 0 && idx < len(cursor)-1 {
		raw = cursor[idx+1:]
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func int64Query(r *http.Request, name string, fallback int64) int64 {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

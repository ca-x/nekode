package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/config"
	"github.com/ca-x/nekode/internal/storage"
)

func TestHealth(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	s.Handler().ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %q, want ok", body["status"])
	}
}

func TestProtocolEndpoint(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/protocol", nil)
	s.Handler().ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["protoPath"] != ProtocolPath {
		t.Fatalf("protoPath = %q, want %q", body["protoPath"], ProtocolPath)
	}
}

func TestAuthAndCoreAPIs(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))

	token := bootstrapToken(t, s)
	endpoint := doJSON(t, s, http.MethodPost, "/api/interaction-endpoints", token, map[string]any{
		"kind":            "web",
		"provider":        "browser",
		"displayName":     "Web Console",
		"inboundEnabled":  true,
		"outboundEnabled": true,
	})
	if endpoint.Code != http.StatusCreated {
		t.Fatalf("create endpoint status = %d body=%s", endpoint.Code, endpoint.Body.String())
	}

	message := doJSON(t, s, http.MethodPost, "/api/messages", token, map[string]any{
		"target":  "#general",
		"content": "hello",
	})
	if message.Code != http.StatusCreated {
		t.Fatalf("create message status = %d body=%s", message.Code, message.Body.String())
	}

	messages := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/messages?target=%23general", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(messages, req)
	if messages.Code != http.StatusOK {
		t.Fatalf("list messages status = %d body=%s", messages.Code, messages.Body.String())
	}

	task := doJSON(t, s, http.MethodPost, "/api/tasks", token, map[string]any{
		"summary": "wire backend",
		"target":  "#general",
	})
	if task.Code != http.StatusCreated {
		t.Fatalf("create task status = %d body=%s", task.Code, task.Body.String())
	}
	var taskBody storage.Task
	if err := json.Unmarshal(task.Body.Bytes(), &taskBody); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	updated := doJSON(t, s, http.MethodPatch, "/api/tasks/"+taskBody.ID, token, map[string]any{
		"state": "in_progress",
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("update task status = %d body=%s", updated.Code, updated.Body.String())
	}
	blocked := doJSON(t, s, http.MethodPatch, "/api/tasks/"+taskBody.ID, token, map[string]any{
		"state": "blocked",
	})
	if blocked.Code != http.StatusOK {
		t.Fatalf("block task status = %d body=%s", blocked.Code, blocked.Body.String())
	}
	listBlocked := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/tasks?state=blocked&target=%23general", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(listBlocked, req)
	if listBlocked.Code != http.StatusOK {
		t.Fatalf("list blocked tasks status = %d body=%s", listBlocked.Code, listBlocked.Body.String())
	}
	cancelledTask := doJSON(t, s, http.MethodPost, "/api/tasks", token, map[string]any{
		"summary": "cancel stale work",
		"target":  "#general",
		"state":   "cancelled",
	})
	if cancelledTask.Code != http.StatusCreated {
		t.Fatalf("create cancelled task status = %d body=%s", cancelledTask.Code, cancelledTask.Body.String())
	}
	var cancelledBody storage.Task
	if err := json.Unmarshal(cancelledTask.Body.Bytes(), &cancelledBody); err != nil {
		t.Fatalf("decode cancelled task: %v", err)
	}
	if cancelledBody.State != "canceled" {
		t.Fatalf("cancelled alias stored as %q, want canceled", cancelledBody.State)
	}
	invalidList := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/tasks?state=reviewing", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(invalidList, req)
	if invalidList.Code != http.StatusBadRequest {
		t.Fatalf("list invalid state status = %d body=%s, want 400", invalidList.Code, invalidList.Body.String())
	}
	events, err := s.daemon.ListEventsSince(context.Background(), &daemonv1.ListEventsSinceRequest{
		Cursor: &daemonv1.EventCursor{Target: "#general"},
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("ListEventsSince() error = %v", err)
	}
	if findHTTPMutationEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_MESSAGE, daemonv1.EventOperation_EVENT_OPERATION_APPENDED) == nil {
		t.Fatalf("HTTP mutation events = %+v, want appended message event", events.GetEvents())
	}
	if findHTTPMutationEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_TASK, daemonv1.EventOperation_EVENT_OPERATION_CREATED) == nil {
		t.Fatalf("HTTP mutation events = %+v, want created task event", events.GetEvents())
	}
	changed := findHTTPMutationEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_TASK, daemonv1.EventOperation_EVENT_OPERATION_STATE_CHANGED)
	if changed == nil || changed.GetScope().GetScopeType() != daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TASK {
		t.Fatalf("HTTP mutation events = %+v, want task state_changed event with task scope", events.GetEvents())
	}
}

func findHTTPMutationEvent(events []*daemonv1.CollaborationEvent, kind daemonv1.CollaborationEventKind, operation daemonv1.EventOperation) *daemonv1.CollaborationEvent {
	for _, event := range events {
		if event.GetKind() == kind && event.GetOperation() == operation {
			return event
		}
	}
	return nil
}

func TestDaemonBridgeEndpoints(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))
	token := bootstrapToken(t, s)

	unauthorized := httptest.NewRecorder()
	s.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/daemon/info", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("daemon info without auth status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	info := doGET(t, s, "/api/daemon/info", token)
	if info.Code != http.StatusOK {
		t.Fatalf("daemon info status = %d body=%s", info.Code, info.Body.String())
	}
	var infoBody map[string]any
	if err := json.Unmarshal(info.Body.Bytes(), &infoBody); err != nil {
		t.Fatalf("decode daemon info: %v", err)
	}
	if infoBody["serverId"] == "" || infoBody["protocolVersion"] == float64(0) {
		t.Fatalf("daemon info body = %+v, want server identity and protocol version", infoBody)
	}

	if _, err := s.daemon.UpdateAgentStatus(context.Background(), &daemonv1.UpdateAgentStatusRequest{
		Status: &daemonv1.AgentStatusSnapshot{
			AgentId:       "agent-1",
			ComputerId:    "computer-1",
			Presence:      daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE,
			ActivityState: daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_CODING,
			Health:        daemonv1.AgentHealth_AGENT_HEALTH_OK,
		},
	}); err != nil {
		t.Fatalf("UpdateAgentStatus() error = %v", err)
	}
	statuses := doGET(t, s, "/api/daemon/agent-statuses?agentId=agent-1", token)
	if statuses.Code != http.StatusOK {
		t.Fatalf("agent statuses status = %d body=%s", statuses.Code, statuses.Body.String())
	}
	assertJSONItems(t, statuses.Body.Bytes(), 1)

	if _, err := s.daemon.LogActivity(context.Background(), &daemonv1.LogActivityRequest{
		Target:  "#general",
		AgentId: "agent-1",
		Kind:    "test_run",
		Summary: "bridge test",
	}); err != nil {
		t.Fatalf("LogActivity() error = %v", err)
	}
	activity := doGET(t, s, "/api/daemon/activity?target=%23general", token)
	if activity.Code != http.StatusOK {
		t.Fatalf("activity status = %d body=%s", activity.Code, activity.Body.String())
	}
	assertJSONItems(t, activity.Body.Bytes(), 1)

	events := doGET(t, s, "/api/daemon/events?target=%23general", token)
	if events.Code != http.StatusOK {
		t.Fatalf("events status = %d body=%s", events.Code, events.Body.String())
	}
	assertJSONItems(t, events.Body.Bytes(), 1)
}

func TestServerEventsSSEBridge(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))
	token := bootstrapToken(t, s)
	if _, err := s.daemon.LogActivity(context.Background(), &daemonv1.LogActivityRequest{
		Target:  "#general",
		AgentId: "agent-1",
		Kind:    "test_run",
		Summary: "stream test",
	}); err != nil {
		t.Fatalf("LogActivity() error = %v", err)
	}

	testServer := httptest.NewServer(s.Handler())
	t.Cleanup(testServer.Close)
	resp, err := testServer.Client().Get(testServer.URL + "/api/server-events?target=%23general&access_token=" + token)
	if err != nil {
		t.Fatalf("GET server events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("server events status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", ct)
	}
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("server events stream closed before message event")
			}
			if line == "event: message" {
				return
			}
		case <-deadline:
			t.Fatal("server events stream did not emit a message event")
		}
	}
}

func TestServerEventsSSEGlobalStreamKeepsGlobalCursor(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))
	token := bootstrapToken(t, s)
	for _, target := range []string{"#general", "dm:agent-2"} {
		if _, err := s.daemon.LogActivity(context.Background(), &daemonv1.LogActivityRequest{
			Target:  target,
			AgentId: "agent-1",
			Kind:    "test_run",
			Summary: "global stream test",
		}); err != nil {
			t.Fatalf("LogActivity(%s) error = %v", target, err)
		}
	}

	testServer := httptest.NewServer(s.Handler())
	t.Cleanup(testServer.Close)
	resp, err := testServer.Client().Get(testServer.URL + "/api/server-events?limit=1&access_token=" + token)
	if err != nil {
		t.Fatalf("GET server events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("server events status = %d", resp.StatusCode)
	}
	events := readSSEMessages(t, resp, 2)
	got := []string{events[0].Target, events[1].Target}
	want := []string{"#general", "dm:agent-2"}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("global SSE targets = %v, want %v", got, want)
	}
	if events[0].Sequence == 0 || events[1].Sequence <= events[0].Sequence {
		t.Fatalf("global SSE sequences = %+v, want increasing non-zero sequences", events)
	}
}

func testConfig() config.Config {
	return config.Config{
		Addr:     "127.0.0.1:0",
		GRPCAddr: "127.0.0.1:0",
		BaseURL:  "http://127.0.0.1",
		DataDir:  "/tmp/nekode-test",
		DBType:   "sqlite",
		DBDSN:    ":memory:",
	}
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("server_test")+"?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func bootstrapToken(t *testing.T, s *Server) string {
	t.Helper()
	resp := doJSON(t, s, http.MethodPost, "/api/auth/bootstrap", "", map[string]any{
		"username":    "admin",
		"password":    "secret123",
		"displayName": "Admin",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if body.Token == "" {
		t.Fatal("bootstrap token is empty")
	}
	return body.Token
}

func doJSON(t *testing.T, s *Server, method, target, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(resp, req)
	return resp
}

func doGET(t *testing.T, s *Server, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(resp, req)
	return resp
}

func assertJSONItems(t *testing.T, data []byte, want int) {
	t.Helper()
	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(body.Items) != want {
		t.Fatalf("items = %d, want %d; body=%s", len(body.Items), want, string(data))
	}
}

type sseEvent struct {
	Target   string `json:"target"`
	Sequence int64  `json:"sequence"`
}

func readSSEMessages(t *testing.T, resp *http.Response, count int) []sseEvent {
	t.Helper()
	lines := make(chan string, 32)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	deadline := time.After(2 * time.Second)
	events := make([]sseEvent, 0, count)
	messageEvent := false
	for len(events) < count {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("server events stream closed after %d message events", len(events))
			}
			if line == "event: message" {
				messageEvent = true
				continue
			}
			if messageEvent && strings.HasPrefix(line, "data: ") {
				var event sseEvent
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
					t.Fatalf("decode SSE data %q: %v", line, err)
				}
				events = append(events, event)
				messageEvent = false
			}
		case <-deadline:
			t.Fatalf("server events stream emitted %d message events, want %d", len(events), count)
		}
	}
	return events
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

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
}

func testConfig() config.Config {
	return config.Config{
		Addr:    "127.0.0.1:0",
		BaseURL: "http://127.0.0.1",
		DataDir: "/tmp/nekode-test",
		DBType:  "sqlite",
		DBDSN:   ":memory:",
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

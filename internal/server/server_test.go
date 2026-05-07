package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ca-x/nekode/internal/config"
)

func TestHealth(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler))

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
	s := New(testConfig(), slog.New(slog.DiscardHandler))

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

func testConfig() config.Config {
	return config.Config{
		Addr:    "127.0.0.1:0",
		BaseURL: "http://127.0.0.1",
		DataDir: "/tmp/nekode-test",
	}
}

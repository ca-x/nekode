package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ca-x/nekode/internal/config"
)

func TestDaemonRPCURLDerivesRequestBaseWhenBaseURLIsDefault(t *testing.T) {
	s := &Server{cfg: config.Config{BaseURL: config.DefaultBaseURL}}
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/install.sh", nil)
	req.Host = "internal-container:18790"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "nekode.example.test")

	if got := s.daemonRPCURL(req); got != "https://nekode.example.test" {
		t.Fatalf("daemonRPCURL() = %q, want forwarded public URL", got)
	}
}

func TestDaemonRPCURLPrefersExplicitDaemonRPCURL(t *testing.T) {
	s := &Server{cfg: config.Config{
		BaseURL:      config.DefaultBaseURL,
		DaemonRPCURL: "https://daemon.example.test/",
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/install.sh", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "nekode.example.test")

	if got := s.daemonRPCURL(req); got != "https://daemon.example.test" {
		t.Fatalf("daemonRPCURL() = %q, want explicit daemon RPC URL", got)
	}
}

func TestDaemonRPCURLPrefersNonDefaultBaseURL(t *testing.T) {
	s := &Server{cfg: config.Config{BaseURL: "https://configured.example.test/"}}
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/install.sh", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "nekode.example.test")

	if got := s.daemonRPCURL(req); got != "https://configured.example.test" {
		t.Fatalf("daemonRPCURL() = %q, want configured base URL", got)
	}
}

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/gen/go/nekode/daemon/v1/daemonv1connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestConnectReportAgentRunCloseDoesNotWaitForResponse(t *testing.T) {
	release := make(chan struct{})
	received := make(chan struct{})
	handler := blockingReportAgentRunHandler{release: release, received: received}
	path, connectHandler := daemonv1connect.NewDaemonControlServiceHandler(handler)
	mux := http.NewServeMux()
	mux.Handle(path, connectHandler)
	server := httptest.NewServer(h2c.NewHandler(mux, &http2.Server{}))
	defer server.Close()
	defer close(release)

	client := newConnectDaemonClient(defaultConnectHTTPClient(), server.URL)
	stream, err := client.ReportAgentRun(context.Background())
	if err != nil {
		t.Fatalf("ReportAgentRun() error = %v", err)
	}
	if err := stream.Send(&daemonv1.AgentRunEvent{RunId: "run-1"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("server did not receive streamed event")
	}

	done := make(chan error, 1)
	go func() {
		done <- stream.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Close() waited for the server response")
	}
}

type blockingReportAgentRunHandler struct {
	daemonv1connect.UnimplementedDaemonControlServiceHandler
	release  <-chan struct{}
	received chan<- struct{}
}

func (h blockingReportAgentRunHandler) ReportAgentRun(ctx context.Context, stream *connect.ClientStream[daemonv1.AgentRunEvent]) (*connect.Response[daemonv1.ReportAgentRunResponse], error) {
	if !stream.Receive() {
		return nil, stream.Err()
	}
	close(h.received)
	select {
	case <-h.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return connect.NewResponse(&daemonv1.ReportAgentRunResponse{PersistedCount: 1}), nil
}

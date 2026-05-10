package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/storage"
)

// TestPreviewTunnelEndToEnd is a full-stack exercise of the preview
// tunnel path: bootstrap an admin, attach a fake daemon over gRPC
// (via bufconn), create a tunnel, round-trip a /preview/<token>/
// request through the bidi stream, verify the browser got back bytes
// the "daemon" intended to send.
//
// We fake the upstream HTTP server inside the test — whenever the
// fake daemon receives a REQUEST_START it answers with a canned
// RESPONSE_START + BODY + END. This keeps the test hermetic; the
// real daemon-side client (cmd/nekode-daemon/tunnel_proxy.go) is
// exercised separately by unit tests and compile-time checks.
func TestPreviewTunnelEndToEnd(t *testing.T) {
	store := newTestStore(t)
	server := newPreviewTunnelServer(t, store)
	token := bootstrapToken(t, server)

	// Spin up an in-memory gRPC server so the daemonrpc.Server is
	// reachable over a real stream, not a mocked one.
	lis := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	daemonv1.RegisterDaemonControlServiceServer(grpcServer, server.daemon)
	go grpcServer.Serve(lis) //nolint:errcheck // test shutdown handles it
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(context.Background())
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := daemonv1.NewDaemonControlServiceClient(conn)

	// Seed a computer row so the tunnel create passes validation.
	// (No Computer ent entity today — CreateTunnel only validates the
	// id is non-empty — but picking a stable id simplifies assertions.)
	const computerID = "cmp_e2e"

	// Attach a fake daemon. Runs until ctx is cancelled; replies to
	// REQUEST_START with a fixed body so the test can assert on the
	// round-trip.
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	t.Cleanup(daemonCancel)
	daemonReady := make(chan struct{})
	daemonDone := make(chan struct{})
	const bodyReply = "hello from fake daemon"
	go runFakeDaemon(t, client, daemonCtx, computerID, bodyReply, daemonReady, daemonDone)

	select {
	case <-daemonReady:
	case <-time.After(5 * time.Second):
		t.Fatalf("fake daemon never attached")
	}

	// Create the tunnel — admin-only endpoint.
	createResp := doJSON(t, server, http.MethodPost, "/api/tunnels", token, map[string]any{
		"computerId":   computerID,
		"localPort":    9999,
		"label":        "e2e",
		"accessPolicy": "members",
		"ttlSeconds":   60,
	})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create tunnel: %d %s", createResp.Code, createResp.Body.String())
	}
	var created storage.TunnelRecord
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode tunnel: %v", err)
	}
	if created.Token == "" {
		t.Fatalf("create response should include token for creator")
	}

	// Round-trip an inbound preview request through the real HTTP
	// handler + registry + daemon stream.
	previewReq := httptest.NewRequest(http.MethodGet, "/preview/"+created.Token+"/hello", nil)
	previewReq.Header.Set("Authorization", "Bearer "+token)
	previewResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(previewResp, previewReq)

	if previewResp.Code != http.StatusOK {
		t.Fatalf("preview: expected 200, got %d body=%q", previewResp.Code, previewResp.Body.String())
	}
	if !bytes.Contains(previewResp.Body.Bytes(), []byte(bodyReply)) {
		t.Fatalf("preview: body %q missing expected reply %q", previewResp.Body.String(), bodyReply)
	}

	// Close the tunnel; a subsequent preview call must not succeed.
	closeResp := doJSON(t, server, http.MethodPost, "/api/tunnels/"+created.ID+"/close", token, map[string]any{})
	if closeResp.Code != http.StatusOK {
		t.Fatalf("close: %d %s", closeResp.Code, closeResp.Body.String())
	}
	req2 := httptest.NewRequest(http.MethodGet, "/preview/"+created.Token+"/hello", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp2, req2)
	if resp2.Code == http.StatusOK {
		t.Fatalf("closed tunnel should not be reachable, got 200 body=%q", resp2.Body.String())
	}

	daemonCancel()
	select {
	case <-daemonDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("fake daemon didn't shut down")
	}
}

// newPreviewTunnelServer builds a fully-initialized *Server suitable
// for end-to-end tests, using the shared newTestStore helper.
func newPreviewTunnelServer(t *testing.T, store *storage.Store) *Server {
	t.Helper()
	// NewWithCache is the real constructor; passing nil cache is valid
	// and exercises the same code path production uses when cache is
	// disabled.
	srv := NewWithCache(testConfig(), nil, store, nil)
	return srv
}

// runFakeDaemon implements just enough of the daemon's proxy-exchange
// protocol to answer a single REQUEST_START with a canned response.
// The real daemon lives under cmd/nekode-daemon; running it here would
// need its binary plus a loopback socket, which defeats the purpose of
// an in-process integration test.
func runFakeDaemon(
	t *testing.T,
	client daemonv1.DaemonControlServiceClient,
	ctx context.Context,
	computerID, bodyReply string,
	ready, done chan struct{},
) {
	t.Helper()
	defer close(done)

	stream, err := client.ProxyExchange(ctx)
	if err != nil {
		t.Errorf("open proxy exchange: %v", err)
		return
	}
	// Attach frame uses the well-known sentinel tunnel_id the server
	// expects as the first frame on any new stream.
	if err := stream.Send(&daemonv1.ProxyFrame{
		TunnelId:  "__attach__",
		RequestId: computerID,
	}); err != nil {
		t.Errorf("attach send: %v", err)
		return
	}
	close(ready)

	var sendMu sync.Mutex
	safeSend := func(f *daemonv1.ProxyFrame) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(f)
	}

	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			// Cancel on shutdown is expected; anything else is a failure.
			if ctx.Err() == nil {
				t.Errorf("recv: %v", err)
			}
			return
		}
		if frame.GetKind() != daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_REQUEST_START {
			// Ignore REQUEST_BODY / REQUEST_END — our canned upstream
			// doesn't care about request bodies for this test.
			continue
		}
		requestID := frame.GetRequestId()
		tunnelID := frame.GetTunnelId()
		go func() {
			if err := safeSend(&daemonv1.ProxyFrame{
				TunnelId:   tunnelID,
				RequestId:  requestID,
				Kind:       daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_START,
				StatusCode: 200,
				ResponseHeaders: []*daemonv1.ProxyHeader{
					{Name: "Content-Type", Value: []string{"text/plain"}},
				},
			}); err != nil {
				return
			}
			if err := safeSend(&daemonv1.ProxyFrame{
				TunnelId:  tunnelID,
				RequestId: requestID,
				Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_BODY,
				BodyChunk: []byte(bodyReply),
			}); err != nil {
				return
			}
			_ = safeSend(&daemonv1.ProxyFrame{
				TunnelId:  tunnelID,
				RequestId: requestID,
				Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_END,
			})
		}()
	}
}

// Silence unused-import warnings in slim builds.
var (
	_ = storage.TunnelStateActive
	_ = time.Second
)

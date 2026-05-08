package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestDaemonEnrollmentAPIRequiresAuth(t *testing.T) {
	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()

	resp := doJSON(t, server, http.MethodPost, "/api/daemon/enrollments", "", map[string]any{
		"displayName": "Test Computer",
	})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("create enrollment without auth status = %d body=%s, want 401", resp.Code, resp.Body.String())
	}
}

func TestDaemonEnrollmentAPICreatesGeneratedToken(t *testing.T) {
	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()

	token := bootstrapToken(t, server)
	enrollment := createDaemonEnrollment(t, server, token)

	if enrollment.Token == "" || enrollment.TokenPrefix == "" {
		t.Fatalf("enrollment token fields = %+v, want generated token and prefix", enrollment)
	}
	if enrollment.Token == enrollment.TokenPrefix {
		t.Fatalf("token prefix leaked full token: %+v", enrollment)
	}
	if enrollment.Status != "pending" {
		t.Fatalf("enrollment status = %q, want pending", enrollment.Status)
	}
	if enrollment.StatusURL == "" || enrollment.InstallCommand == "" {
		t.Fatalf("enrollment response = %+v, want status URL and install command", enrollment)
	}

	status := getDaemonEnrollment(t, server, token, enrollment.ID)
	if status.Token != "" || status.InstallCommand != "" {
		t.Fatalf("status response leaked token/install command: %+v", status)
	}
}

func TestDaemonGRPCTokenAuth(t *testing.T) {
	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()
	token := bootstrapToken(t, server)
	enrollment := createDaemonEnrollment(t, server, token)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client := cleanup.client
	_, err := client.RegisterComputer(ctx, registerComputerRequest("missing"))
	assertGRPCCode(t, err, codes.Unauthenticated)

	_, err = client.RegisterComputer(withDaemonToken(ctx, "wrong-secret"), registerComputerRequest("wrong"))
	assertGRPCCode(t, err, codes.Unauthenticated)

	resp, err := client.RegisterComputer(withDaemonToken(ctx, enrollment.Token), registerComputerRequest("ok"))
	if err != nil {
		t.Fatalf("RegisterComputer(with token) error = %v", err)
	}
	if !resp.GetAccepted() || resp.GetLease().GetLeaseId() == "" {
		t.Fatalf("RegisterComputer(with token) = %+v, want accepted lease", resp)
	}

	status := getDaemonEnrollment(t, server, token, enrollment.ID)
	if status.Status != "connected" {
		t.Fatalf("enrollment status = %q, want connected", status.Status)
	}
	if status.ComputerID != "computer-ok" || status.DaemonID != "daemon-ok" || status.LastHeartbeatUnix == 0 {
		t.Fatalf("connected enrollment = %+v, want computer/daemon heartbeat fields", status)
	}
}

func TestDaemonGRPCRejectsExpiredEnrollmentToken(t *testing.T) {
	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()

	_, token, err := server.daemonEnrollments.create(daemonEnrollmentCreate{
		DisplayName: "Expired Computer",
		ExpiresUnix: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("create expired daemon enrollment: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = cleanup.client.RegisterComputer(withDaemonToken(ctx, token), registerComputerRequest("expired"))
	assertGRPCCode(t, err, codes.Unauthenticated)
}

func TestDaemonGRPCStreamTokenAuth(t *testing.T) {
	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()
	token := bootstrapToken(t, server)
	enrollment := createDaemonEnrollment(t, server, token)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client := cleanup.client
	stream, err := client.SubscribeServerEvents(ctx, &daemonv1.SubscribeServerEventsRequest{
		DaemonId:   "daemon-stream",
		ComputerId: "computer-stream",
		RequestId:  "stream-missing",
	})
	if err == nil {
		_, err = stream.Recv()
	}
	assertGRPCCode(t, err, codes.Unauthenticated)

	stream, err = client.SubscribeServerEvents(withDaemonToken(ctx, enrollment.Token), &daemonv1.SubscribeServerEventsRequest{
		DaemonId:   "daemon-stream",
		ComputerId: "computer-stream",
		RequestId:  "stream-ok",
	})
	if err != nil {
		t.Fatalf("SubscribeServerEvents(with token) error = %v", err)
	}
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("SubscribeServerEvents(with token) Recv() error = %v", err)
	}
	if event.GetEvent().GetKind() != daemonv1.ServerEventKind_SERVER_EVENT_KIND_PING {
		t.Fatalf("stream event kind = %v, want ping", event.GetEvent().GetKind())
	}
}

type daemonGRPCTestCleanup struct {
	client daemonv1.DaemonControlServiceClient
	fn     func()
}

func (c daemonGRPCTestCleanup) close() {
	c.fn()
}

func newDaemonGRPCTestClient(t *testing.T) (*Server, daemonGRPCTestCleanup) {
	t.Helper()
	cfg := testConfig()
	cfg.DataDir = t.TempDir()
	cfg.GRPCAddr = "127.0.0.1:0"
	server := New(cfg, slog.New(slog.DiscardHandler), newTestStore(t))
	grpcServer, listener, err := server.startGRPC()
	if err != nil {
		t.Fatalf("startGRPC() error = %v", err)
	}
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		grpcServer.Stop()
		_ = listener.Close()
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	client := daemonv1.NewDaemonControlServiceClient(conn)
	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = listener.Close()
	}
	return server, daemonGRPCTestCleanup{client: client, fn: cleanup}
}

func registerComputerRequest(suffix string) *daemonv1.RegisterComputerRequest {
	return &daemonv1.RegisterComputerRequest{
		Info: &daemonv1.ComputerInfo{
			DaemonId:   "daemon-" + suffix,
			ComputerId: "computer-" + suffix,
			Hostname:   "test-host",
			Status:     daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE,
		},
		RequestId:      "register-" + suffix,
		IdempotencyKey: "register-" + suffix,
	}
}

func withDaemonToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

func assertGRPCCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if status.Code(err) != want {
		t.Fatalf("status.Code(%v) = %v, want %v", err, status.Code(err), want)
	}
}

func createDaemonEnrollment(t *testing.T, s *Server, token string) daemonEnrollmentResponse {
	t.Helper()
	resp := doJSON(t, s, http.MethodPost, "/api/daemon/enrollments", token, map[string]any{
		"displayName": "Test Computer",
		"computerId":  "computer-pending",
		"hostname":    "pending-host",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("create daemon enrollment status = %d body=%s", resp.Code, resp.Body.String())
	}
	var enrollment daemonEnrollmentResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &enrollment); err != nil {
		t.Fatalf("decode daemon enrollment: %v", err)
	}
	return enrollment
}

func getDaemonEnrollment(t *testing.T, s *Server, token, id string) daemonEnrollmentResponse {
	t.Helper()
	resp := doGET(t, s, "/api/daemon/enrollments/"+id, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("get daemon enrollment status = %d body=%s", resp.Code, resp.Body.String())
	}
	var enrollment daemonEnrollmentResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &enrollment); err != nil {
		t.Fatalf("decode daemon enrollment status: %v", err)
	}
	return enrollment
}

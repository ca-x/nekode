package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
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
	if enrollment.InstallScriptURL == "" {
		t.Fatalf("enrollment response = %+v, want install script URL", enrollment)
	}
	if strings.Contains(enrollment.InstallCommand, enrollment.Token) || strings.Contains(enrollment.InstallScriptURL, enrollment.Token) {
		t.Fatalf("install command/url leaked full daemon token: %+v", enrollment)
	}
	if !strings.Contains(enrollment.InstallCommand, "/install.sh?") {
		t.Fatalf("install command = %q, want install.sh one-liner", enrollment.InstallCommand)
	}

	status := getDaemonEnrollment(t, server, token, enrollment.ID)
	if status.Token != "" || status.InstallCommand != "" {
		t.Fatalf("status response leaked token/install command: %+v", status)
	}
}

func TestDaemonEnrollmentInstallScriptConsumesCodeAndRotatesToken(t *testing.T) {
	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()
	token := bootstrapToken(t, server)
	enrollment := createDaemonEnrollment(t, server, token)

	resp := doGET(t, server, enrollment.InstallScriptURL, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("install script status = %d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "systemctl enable --now nekode-daemon") || !strings.Contains(body, "launchctl bootstrap system") {
		t.Fatalf("install script missing service install paths:\n%s", body)
	}
	if strings.Contains(body, enrollment.Token) {
		t.Fatalf("install script reused original create token")
	}
	rotatedToken := extractDaemonToken(t, body)

	replay := doGET(t, server, enrollment.InstallScriptURL, "")
	if replay.Code != http.StatusNotFound {
		t.Fatalf("replayed install script status = %d body=%s, want 404", replay.Code, replay.Body.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := cleanup.client.RegisterComputer(withDaemonToken(ctx, enrollment.Token), registerComputerRequest("old-token"))
	assertGRPCCode(t, err, codes.Unauthenticated)
	_, err = cleanup.client.RegisterComputer(withDaemonToken(ctx, rotatedToken), registerComputerRequest("rotated-token"))
	if err != nil {
		t.Fatalf("RegisterComputer(with rotated install token) error = %v", err)
	}
}

func TestDaemonEnrollmentInstallPowerShell(t *testing.T) {
	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()
	token := bootstrapToken(t, server)
	enrollment := createDaemonEnrollment(t, server, token)
	target := strings.Replace(enrollment.InstallScriptURL, "/install.sh?", "/install.ps1?", 1)

	resp := doGET(t, server, target, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("install ps1 status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, "New-Service") || !strings.Contains(body, "nekode-daemon_${version}_windows_${goarch}.zip") {
		t.Fatalf("powershell installer missing service/artifact paths:\n%s", body)
	}
	if strings.Contains(body, enrollment.Token) {
		t.Fatalf("powershell installer reused original create token")
	}
}

func TestDaemonManagementScripts(t *testing.T) {
	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()
	token := bootstrapToken(t, server)
	enrollment := createDaemonEnrollment(t, server, token)

	tests := []struct {
		path string
		want []string
	}{
		{
			path: "/api/daemon/scripts/upgrade.sh",
			want: []string{"ACTION=\"upgrade\"", "install_binary", "systemctl enable --now", "launchctl bootstrap system"},
		},
		{
			path: "/api/daemon/scripts/reinstall.sh",
			want: []string{"ACTION=\"reinstall\"", "remove_service", "install_service"},
		},
		{
			path: "/api/daemon/scripts/uninstall.sh",
			want: []string{"ACTION=\"uninstall\"", "NEKODE_PURGE_CONFIG", "rm -f \"$BIN_PATH\""},
		},
		{
			path: "/api/daemon/scripts/upgrade.ps1",
			want: []string{"$action = \"upgrade\"", "Install-DaemonBinary", "Start-Service"},
		},
		{
			path: "/api/daemon/scripts/reinstall.ps1",
			want: []string{"$action = \"reinstall\"", "Remove-DaemonService", "Install-DaemonService"},
		},
		{
			path: "/api/daemon/scripts/uninstall.ps1",
			want: []string{"$action = \"uninstall\"", "NEKODE_PURGE_CONFIG", "sc.exe delete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp := doGET(t, server, tt.path, "")
			if resp.Code != http.StatusOK {
				t.Fatalf("management script status = %d body=%s", resp.Code, resp.Body.String())
			}
			if got := resp.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			body := resp.Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Fatalf("management script %s missing %q:\n%s", tt.path, want, body)
				}
			}
			if strings.Contains(body, enrollment.Token) || strings.Contains(body, "ndt_") {
				t.Fatalf("management script leaked daemon token material:\n%s", body)
			}
			if strings.Contains(body, "/api/daemon/enrollments/") {
				t.Fatalf("management script should not reuse enrollment install code paths:\n%s", body)
			}
		})
	}
}

func TestDaemonInstallScriptsPinReleaseArtifactDownload(t *testing.T) {
	t.Setenv("NEKODE_DAEMON_DOWNLOAD_VERSION", "v9.8.7")
	t.Setenv("NEKODE_DAEMON_DOWNLOAD_BASE_URL", "https://downloads.example.test/nekode///")

	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()
	token := bootstrapToken(t, server)
	enrollment := createDaemonEnrollment(t, server, token)

	resp := doGET(t, server, enrollment.InstallScriptURL, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("install script status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{
		`VERSION="${NEKODE_DAEMON_VERSION:-v9.8.7}"`,
		`DOWNLOAD_BASE_URL="${NEKODE_DAEMON_DOWNLOAD_BASE_URL:-https://downloads.example.test/nekode}"`,
		`ARTIFACT="nekode-daemon_${VERSION}_${OS}_${ARCH}.tar.gz"`,
		`"$DOWNLOAD_BASE_URL/$ARTIFACT"`,
		`"$DOWNLOAD_BASE_URL/SHA256SUMS.txt"`,
		`grep "  $ARTIFACT$" "$TMPDIR/SHA256SUMS.txt"`,
		`sha256sum -c "$ARTIFACT.sha256"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("install script missing %q:\n%s", want, body)
		}
	}
}

func TestDaemonManagementScriptsPinReleaseArtifactDownload(t *testing.T) {
	t.Setenv("NEKODE_DAEMON_DOWNLOAD_VERSION", "v9.8.7")
	t.Setenv("NEKODE_DAEMON_DOWNLOAD_BASE_URL", "https://downloads.example.test/nekode/")

	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()

	shellResp := doGET(t, server, "/api/daemon/scripts/upgrade.sh", "")
	if shellResp.Code != http.StatusOK {
		t.Fatalf("upgrade shell status = %d body=%s", shellResp.Code, shellResp.Body.String())
	}
	shell := shellResp.Body.String()
	for _, want := range []string{
		`VERSION="${NEKODE_DAEMON_VERSION:-v9.8.7}"`,
		`DOWNLOAD_BASE_URL="${NEKODE_DAEMON_DOWNLOAD_BASE_URL:-https://downloads.example.test/nekode}"`,
		`ARTIFACT="nekode-daemon_${VERSION}_${OS}_${ARCH}.tar.gz"`,
		`"$DOWNLOAD_BASE_URL/SHA256SUMS.txt"`,
		`tar -xzf "$TMPDIR/$ARTIFACT"`,
	} {
		if !strings.Contains(shell, want) {
			t.Fatalf("upgrade shell missing %q:\n%s", want, shell)
		}
	}

	powerShellResp := doGET(t, server, "/api/daemon/scripts/upgrade.ps1", "")
	if powerShellResp.Code != http.StatusOK {
		t.Fatalf("upgrade powershell status = %d body=%s", powerShellResp.Code, powerShellResp.Body.String())
	}
	powerShell := powerShellResp.Body.String()
	for _, want := range []string{
		`$version = if ($env:NEKODE_DAEMON_VERSION) { $env:NEKODE_DAEMON_VERSION } else { "v9.8.7" }`,
		`$downloadBaseUrl = if ($env:NEKODE_DAEMON_DOWNLOAD_BASE_URL) { $env:NEKODE_DAEMON_DOWNLOAD_BASE_URL.TrimEnd("/") } else { "https://downloads.example.test/nekode" }`,
		`$artifact = "nekode-daemon_${version}_windows_${goarch}.zip"`,
		`"$downloadBaseUrl/SHA256SUMS.txt"`,
		`Get-FileHash -Algorithm SHA256`,
	} {
		if !strings.Contains(powerShell, want) {
			t.Fatalf("upgrade powershell missing %q:\n%s", want, powerShell)
		}
	}
}

func TestDaemonEnrollmentRevokeBlocksTokenAndInstallCode(t *testing.T) {
	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()
	token := bootstrapToken(t, server)
	enrollment := createDaemonEnrollment(t, server, token)

	resp := doJSON(t, server, http.MethodPost, "/api/daemon/enrollments/"+enrollment.ID+"/revoke", token, map[string]any{})
	if resp.Code != http.StatusOK {
		t.Fatalf("revoke enrollment status = %d body=%s", resp.Code, resp.Body.String())
	}
	var revoked daemonEnrollmentResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &revoked); err != nil {
		t.Fatalf("decode revoked enrollment: %v", err)
	}
	if revoked.Status != "revoked" || revoked.RevokedUnix == 0 {
		t.Fatalf("revoked enrollment = %+v, want revoked status/time", revoked)
	}

	script := doGET(t, server, enrollment.InstallScriptURL, "")
	if script.Code != http.StatusNotFound {
		t.Fatalf("revoked install script status = %d body=%s, want 404", script.Code, script.Body.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := cleanup.client.RegisterComputer(withDaemonToken(ctx, enrollment.Token), registerComputerRequest("revoked"))
	assertGRPCCode(t, err, codes.Unauthenticated)
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

func extractDaemonToken(t *testing.T, body string) string {
	t.Helper()
	matches := regexp.MustCompile(`"token":\s*"(ndt_[^"]+)"`).FindStringSubmatch(body)
	if len(matches) != 2 {
		t.Fatalf("install script did not contain daemon token config:\n%s", body)
	}
	return matches[1]
}

func TestDaemonGRPCRejectsExpiredEnrollmentToken(t *testing.T) {
	server, cleanup := newDaemonGRPCTestClient(t)
	defer cleanup.close()

	_, token, _, err := server.daemonEnrollments.create(daemonEnrollmentCreate{
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

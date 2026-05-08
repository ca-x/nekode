package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/version"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type daemonConfig struct {
	ConfigPath        string
	GRPCAddr          string
	Token             string
	DaemonID          string
	ComputerID        string
	DisplayName       string
	Hostname          string
	HeartbeatInterval time.Duration
	AgentID           string
	RuntimeKind       string
	Target            string
	Once              bool
}

type daemonConfigFile struct {
	GRPCAddr          string `json:"grpcAddr"`
	Token             string `json:"token"`
	DaemonID          string `json:"daemonId"`
	ComputerID        string `json:"computerId"`
	DisplayName       string `json:"displayName"`
	Hostname          string `json:"hostname"`
	HeartbeatInterval string `json:"heartbeatInterval"`
	AgentID           string `json:"agentId"`
	RuntimeKind       string `json:"runtimeKind"`
	Target            string `json:"target"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("daemon failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		args = []string{"run"}
	}
	switch args[0] {
	case "run":
		return runDaemon(args[1:])
	case "version":
		info := version.Current()
		fmt.Printf("nekode-daemon %s (%s, %s)\n", info.Version, info.Commit, info.BuildTime)
		return nil
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runDaemon(args []string) error {
	cfg, err := loadConfig(args)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := grpc.NewClient(
		cfg.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("connect daemon grpc: %w", err)
	}
	defer conn.Close()

	client := daemonv1.NewDaemonControlServiceClient(conn)
	session := &daemonSession{cfg: cfg, client: client}
	if err := session.register(ctx); err != nil {
		return err
	}
	if err := session.reportStatus(ctx, daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING, "daemon registered"); err != nil {
		return err
	}
	if cfg.Once {
		if err := session.heartbeat(ctx); err != nil {
			return err
		}
		slog.Info("daemon smoke completed", "computer_id", cfg.ComputerID, "daemon_id", cfg.DaemonID)
		return nil
	}
	return session.loop(ctx)
}

type daemonSession struct {
	cfg    daemonConfig
	client daemonv1.DaemonControlServiceClient
	lease  *daemonv1.Lease
}

func (s *daemonSession) register(ctx context.Context) error {
	req := &daemonv1.RegisterComputerRequest{
		Info:           s.computerInfo(daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE),
		Inventory:      s.inventory(),
		RequestId:      newRequestID("register"),
		IdempotencyKey: newRequestID("register"),
		Context:        s.requestContext(),
	}
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	resp, err := s.client.RegisterComputer(callCtx, req)
	if err != nil {
		return fmt.Errorf("register computer: %w", err)
	}
	if !resp.GetAccepted() {
		return fmt.Errorf("register computer was not accepted")
	}
	s.lease = resp.GetLease()
	slog.Info("daemon registered", "computer_id", s.cfg.ComputerID, "lease_id", s.lease.GetLeaseId())
	return nil
}

func (s *daemonSession) heartbeat(ctx context.Context) error {
	status := s.agentStatus(daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING, "daemon heartbeat")
	req := &daemonv1.HeartbeatComputerRequest{
		Info:             s.computerInfo(daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE),
		LeaseId:          s.lease.GetLeaseId(),
		RequestId:        newRequestID("heartbeat"),
		IdempotencyKey:   newRequestID("heartbeat"),
		AgentStatuses:    []*daemonv1.AgentStatusSnapshot{status},
		Context:          s.requestContext(),
		InventoryVersion: "daemon-minimal-v1",
	}
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	resp, err := s.client.HeartbeatComputer(callCtx, req)
	if err != nil {
		return fmt.Errorf("heartbeat computer: %w", err)
	}
	if !resp.GetAccepted() {
		return fmt.Errorf("heartbeat was not accepted")
	}
	s.lease = resp.GetLease()
	slog.Info("daemon heartbeat accepted", "computer_id", s.cfg.ComputerID, "next_seconds", resp.GetNextHeartbeatAfterSeconds())
	return nil
}

func (s *daemonSession) reportStatus(ctx context.Context, state daemonv1.AgentActivityState, summary string) error {
	req := &daemonv1.UpdateAgentStatusRequest{
		Status:         s.agentStatus(state, summary),
		RequestId:      newRequestID("status"),
		IdempotencyKey: newRequestID("status"),
		Context:        s.requestContext(),
	}
	callCtx, cancel := context.WithTimeout(s.withToken(ctx), 10*time.Second)
	defer cancel()
	if _, err := s.client.UpdateAgentStatus(callCtx, req); err != nil {
		return fmt.Errorf("update agent status: %w", err)
	}
	slog.Info("daemon agent status reported", "agent_id", s.cfg.AgentID, "state", state.String())
	return nil
}

func (s *daemonSession) loop(ctx context.Context) error {
	interval := s.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.heartbeat(ctx); err != nil {
				return err
			}
		}
	}
}

func (s *daemonSession) computerInfo(status daemonv1.ComputerStatus) *daemonv1.ComputerInfo {
	return &daemonv1.ComputerInfo{
		DaemonId:     s.cfg.DaemonID,
		ComputerId:   s.cfg.ComputerID,
		DisplayName:  s.cfg.DisplayName,
		Hostname:     s.cfg.Hostname,
		Os:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Version:      version.Current().Version,
		Status:       status,
		LastSeenUnix: time.Now().Unix(),
		LeaseId:      s.lease.GetLeaseId(),
		Capabilities: []*daemonv1.Capability{
			{Name: "heartbeat", Description: "daemon liveness reporting", Enabled: true},
			{Name: "agent_status", Description: "agent status reporting", Enabled: true},
		},
	}
}

func (s *daemonSession) inventory() *daemonv1.ComputerInventory {
	runtimeID := "runtime-" + s.cfg.ComputerID + "-" + s.cfg.RuntimeKind
	profileID := "profile-" + s.cfg.AgentID
	return &daemonv1.ComputerInventory{
		Runtimes: []*daemonv1.Runtime{{
			RuntimeId:   runtimeID,
			ComputerId:  s.cfg.ComputerID,
			Kind:        s.cfg.RuntimeKind,
			DisplayName: s.cfg.RuntimeKind,
			Command:     s.cfg.RuntimeKind,
			Installed:   true,
			Healthy:     true,
			Capabilities: []*daemonv1.Capability{
				{Name: "agent_instance.create", Description: "runtime can host multiple agent instances", Enabled: true},
				{Name: "heartbeat", Description: "daemon liveness reporting", Enabled: true},
				{Name: "agent_status", Description: "agent status reporting", Enabled: true},
			},
		}},
		RuntimeProfiles: []*daemonv1.RuntimeProfile{{
			RuntimeProfileId: profileID,
			Kind:             s.cfg.RuntimeKind,
			Provider:         "daemon",
			Capabilities: []*daemonv1.Capability{{
				Name:        "agent_instance.create",
				Description: "Web can create multiple agent instances for this runtime kind",
				Enabled:     true,
			}},
		}},
		Agents: []*daemonv1.AgentProfile{{
			AgentId:          s.cfg.AgentID,
			Name:             s.cfg.AgentID,
			DisplayName:      s.cfg.AgentID,
			Enabled:          true,
			ComputerId:       s.cfg.ComputerID,
			RuntimeProfileId: profileID,
			RuntimeKind:      s.cfg.RuntimeKind,
			DaemonVersion:    version.Current().Version,
			Status:           daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE,
			Capabilities: []*daemonv1.Capability{{
				Name:        "agent_instance.template",
				Description: "template status for Web-created agent instances",
				Enabled:     true,
			}},
		}},
	}
}

func (s *daemonSession) agentStatus(state daemonv1.AgentActivityState, summary string) *daemonv1.AgentStatusSnapshot {
	now := time.Now().Unix()
	return &daemonv1.AgentStatusSnapshot{
		AgentId:          s.cfg.AgentID,
		ComputerId:       s.cfg.ComputerID,
		RuntimeProfileId: "profile-" + s.cfg.AgentID,
		Presence:         daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE,
		ActivityState:    state,
		Health:           daemonv1.AgentHealth_AGENT_HEALTH_OK,
		Severity:         daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_INFO,
		Summary:          summary,
		Target:           s.cfg.Target,
		StartedTimeUnix:  now,
		UpdatedTimeUnix:  now,
		ExpiresTimeUnix:  now + int64((2*s.cfg.HeartbeatInterval)/time.Second),
	}
}

func (s *daemonSession) requestContext() *daemonv1.RequestContext {
	return &daemonv1.RequestContext{
		TraceId: newRequestID("trace"),
		Actor: &daemonv1.Actor{
			ActorKind:   daemonv1.ActorKind_ACTOR_KIND_DAEMON,
			DaemonId:    s.cfg.DaemonID,
			DisplayName: s.cfg.DisplayName,
		},
		Client: &daemonv1.ClientInfo{
			Platform: "daemon",
			Version:  version.Current().Version,
			Os:       runtime.GOOS,
		},
	}
}

func (s *daemonSession) withToken(ctx context.Context) context.Context {
	if strings.TrimSpace(s.cfg.Token) == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+s.cfg.Token)
}

func loadConfig(args []string) (daemonConfig, error) {
	hostname, _ := os.Hostname()
	cfg := daemonConfig{
		ConfigPath:        firstConfigPathArg(args, env("NEKODE_DAEMON_CONFIG", defaultConfigPath())),
		GRPCAddr:          env("NEKODE_DAEMON_GRPC_ADDR", env("NEKODE_GRPC_ADDR", "127.0.0.1:18789")),
		Token:             env("NEKODE_DAEMON_TOKEN", ""),
		DaemonID:          env("NEKODE_DAEMON_ID", "daemon-"+hostname),
		ComputerID:        env("NEKODE_COMPUTER_ID", "computer-"+hostname),
		DisplayName:       env("NEKODE_COMPUTER_NAME", hostname),
		Hostname:          env("NEKODE_HOSTNAME", hostname),
		HeartbeatInterval: 30 * time.Second,
		AgentID:           env("NEKODE_AGENT_ID", "daemon-agent-"+hostname),
		RuntimeKind:       env("NEKODE_RUNTIME_KIND", "codex"),
		Target:            env("NEKODE_DAEMON_TARGET", "#general"),
	}
	if err := applyConfigFile(&cfg); err != nil {
		return cfg, err
	}

	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.StringVar(&cfg.ConfigPath, "config", cfg.ConfigPath, "daemon install config path")
	flags.StringVar(&cfg.GRPCAddr, "grpc-addr", cfg.GRPCAddr, "Nekode server gRPC address")
	flags.StringVar(&cfg.GRPCAddr, "server-grpc", cfg.GRPCAddr, "Nekode server gRPC address")
	flags.StringVar(&cfg.Token, "token", cfg.Token, "daemon install token; normally read from generated daemon config")
	flags.StringVar(&cfg.DaemonID, "daemon-id", cfg.DaemonID, "stable daemon id")
	flags.StringVar(&cfg.ComputerID, "computer-id", cfg.ComputerID, "stable computer id")
	flags.StringVar(&cfg.DisplayName, "display-name", cfg.DisplayName, "computer display name")
	flags.StringVar(&cfg.Hostname, "hostname", cfg.Hostname, "reported hostname")
	flags.DurationVar(&cfg.HeartbeatInterval, "heartbeat-interval", cfg.HeartbeatInterval, "heartbeat interval")
	flags.StringVar(&cfg.AgentID, "agent-id", cfg.AgentID, "minimal agent id reported by this daemon")
	flags.StringVar(&cfg.RuntimeKind, "runtime-kind", cfg.RuntimeKind, "runtime kind advertised by this daemon")
	flags.StringVar(&cfg.Target, "target", cfg.Target, "status target")
	flags.BoolVar(&cfg.Once, "once", false, "register and heartbeat once, then exit")
	if err := flags.Parse(args); err != nil {
		return cfg, err
	}
	if strings.TrimSpace(cfg.GRPCAddr) == "" {
		return cfg, fmt.Errorf("grpc addr is required")
	}
	if strings.TrimSpace(cfg.DaemonID) == "" || strings.TrimSpace(cfg.ComputerID) == "" {
		return cfg, fmt.Errorf("daemon-id and computer-id are required")
	}
	if cfg.HeartbeatInterval <= 0 {
		return cfg, fmt.Errorf("heartbeat interval must be positive")
	}
	return cfg, nil
}

func firstConfigPathArg(args []string, fallback string) string {
	for index, arg := range args {
		if arg == "--config" && index+1 < len(args) {
			return strings.TrimSpace(args[index+1])
		}
		if value, ok := strings.CutPrefix(arg, "--config="); ok {
			return strings.TrimSpace(value)
		}
	}
	return fallback
}

func applyConfigFile(cfg *daemonConfig) error {
	if strings.TrimSpace(cfg.ConfigPath) == "" {
		return nil
	}
	content, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read daemon config: %w", err)
	}
	var file daemonConfigFile
	if err := json.Unmarshal(content, &file); err != nil {
		return fmt.Errorf("parse daemon config: %w", err)
	}
	overlayString(&cfg.GRPCAddr, file.GRPCAddr)
	overlayString(&cfg.Token, file.Token)
	overlayString(&cfg.DaemonID, file.DaemonID)
	overlayString(&cfg.ComputerID, file.ComputerID)
	overlayString(&cfg.DisplayName, file.DisplayName)
	overlayString(&cfg.Hostname, file.Hostname)
	overlayString(&cfg.AgentID, file.AgentID)
	overlayString(&cfg.RuntimeKind, file.RuntimeKind)
	overlayString(&cfg.Target, file.Target)
	if strings.TrimSpace(file.HeartbeatInterval) != "" {
		duration, err := time.ParseDuration(file.HeartbeatInterval)
		if err != nil {
			return fmt.Errorf("parse daemon heartbeat interval: %w", err)
		}
		cfg.HeartbeatInterval = duration
	}
	return nil
}

func overlayString(dst *string, value string) {
	if strings.TrimSpace(value) != "" {
		*dst = strings.TrimSpace(value)
	}
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".nekode", "daemon.json")
}

func env(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func newRequestID(prefix string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf[:])
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  nekode-daemon run [--config ~/.nekode/daemon.json] [--grpc-addr 127.0.0.1:18789] [--server-grpc 127.0.0.1:18789] [--token <install-token>] [--daemon-id daemon-host] [--computer-id computer-host] [--heartbeat-interval 30s] [--once]")
	fmt.Fprintln(os.Stderr, "  nekode-daemon version")
}

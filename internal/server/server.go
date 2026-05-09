package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/auth"
	"github.com/ca-x/nekode/internal/cache"
	"github.com/ca-x/nekode/internal/config"
	"github.com/ca-x/nekode/internal/daemonrpc"
	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/imbinding"
	imfeishu "github.com/ca-x/nekode/internal/imchannels/feishu"
	imtelegram "github.com/ca-x/nekode/internal/imchannels/telegram"
	imwechat "github.com/ca-x/nekode/internal/imchannels/weixin"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/immedia"
	"github.com/ca-x/nekode/internal/impolicy"
	"github.com/ca-x/nekode/internal/runtimecatalog"
	"github.com/ca-x/nekode/internal/storage"
	"github.com/ca-x/nekode/internal/version"
	"github.com/ca-x/nekode/internal/webdist"
	"google.golang.org/grpc"
)

const ProtocolPath = "proto/nekode/daemon/v1/daemon.proto"

type contextKey string

const principalKey contextKey = "principal"

type Server struct {
	cfg               config.Config
	logger            *slog.Logger
	mux               *http.ServeMux
	store             *storage.Store
	cache             cache.Cache
	auth              *auth.Service
	daemon            *daemonrpc.Server
	daemonEnrollments *daemonEnrollmentStore
	imBindings        *imbinding.Store
}

type Principal struct {
	User    storage.User
	Session storage.Session
}

func New(cfg config.Config, logger *slog.Logger, store *storage.Store) *Server {
	return NewWithCache(cfg, logger, store, nil)
}

func NewWithCache(cfg config.Config, logger *slog.Logger, store *storage.Store, cacheStore cache.Cache) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		cfg:        cfg,
		logger:     logger,
		mux:        http.NewServeMux(),
		store:      store,
		cache:      cacheStore,
		imBindings: imbinding.NewStore(5 * time.Minute),
	}
	if store != nil {
		s.auth = auth.New(store)
		serverID, err := cfg.ServerID()
		if err != nil {
			logger.Warn("failed to load server id; using ephemeral id", "error", err)
		}
		s.daemonEnrollments = newDaemonEnrollmentStore(filepath.Join(cfg.DataDir, daemonEnrollmentDir))
		s.daemon = daemonrpc.New(store, serverID)
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	grpcServer, listener, err := s.startGRPC()
	if err != nil {
		return err
	}
	if grpcServer != nil {
		defer grpcServer.Stop()
		defer listener.Close()
	}

	errs := make(chan error, 1)
	go func() {
		s.logger.Info("nekode server starting", "addr", s.cfg.Addr)
		errs <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) startGRPC() (*grpc.Server, net.Listener, error) {
	if s.daemon == nil || strings.TrimSpace(s.cfg.GRPCAddr) == "" {
		return nil, nil, nil
	}
	listener, err := net.Listen("tcp", s.cfg.GRPCAddr)
	if err != nil {
		return nil, nil, err
	}
	grpcServer := grpc.NewServer(s.grpcServerOptions()...)
	daemonv1.RegisterDaemonControlServiceServer(grpcServer, s.daemon)
	go func() {
		s.logger.Info("nekode daemon grpc starting", "addr", s.cfg.GRPCAddr)
		if err := grpcServer.Serve(listener); err != nil {
			s.logger.Error("daemon grpc stopped", "error", err)
		}
	}()
	return grpcServer, listener, nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /api/version", s.handleVersion)
	s.mux.HandleFunc("GET /api/protocol", s.handleProtocol)
	s.mux.HandleFunc("GET /api/auth/setup-status", s.handleSetupStatus)
	s.mux.HandleFunc("GET /api/auth/init-status", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/auth/bootstrap", s.handleBootstrap)
	s.mux.HandleFunc("POST /api/auth/init", s.handleBootstrap)
	s.mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.handleLogout))
	s.mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.handleMe))
	s.mux.HandleFunc("GET /api/im/providers", s.requireAuth(s.handleListIMProviders))
	s.mux.HandleFunc("GET /api/im/policies/effective", s.requireAuth(s.handleGetIMEffectivePolicy))
	s.mux.HandleFunc("POST /api/im/telegram/{endpointID}/webhook", s.handleTelegramWebhook)
	s.mux.HandleFunc("POST /api/im/feishu/{endpointID}/callback", s.handleFeishuCallback)
	s.mux.HandleFunc("GET /api/im/weixin/{endpointID}/callback", s.handleWeChatCallback)
	s.mux.HandleFunc("POST /api/im/weixin/{endpointID}/callback", s.handleWeChatCallback)
	s.mux.HandleFunc("GET /api/interaction-endpoints", s.requireAuth(s.handleListInteractionEndpoints))
	s.mux.HandleFunc("POST /api/interaction-endpoints", s.requireAuth(s.handleCreateInteractionEndpoint))
	s.mux.HandleFunc("PATCH /api/interaction-endpoints/{id}", s.requireAuth(s.handleUpdateInteractionEndpoint))
	s.mux.HandleFunc("DELETE /api/interaction-endpoints/{id}", s.requireAuth(s.handleDeleteInteractionEndpoint))
	s.mux.HandleFunc("POST /api/interaction-endpoints/{id}/test", s.requireAuth(s.handleTestInteractionEndpoint))
	s.mux.HandleFunc("POST /api/interaction-endpoints/{id}/binding-sessions", s.requireAuth(s.handleCreateBindingSession))
	s.mux.HandleFunc("GET /api/interaction-endpoints/{id}/binding-sessions/{sessionID}", s.requireAuth(s.handleGetBindingSession))
	s.mux.HandleFunc("POST /api/interaction-endpoints/{id}/binding-sessions/{sessionID}/cancel", s.requireAuth(s.handleCancelBindingSession))
	s.mux.HandleFunc("GET /api/notification-routes", s.requireAuth(s.handleListNotificationRoutes))
	s.mux.HandleFunc("POST /api/notification-routes", s.requireAuth(s.handleCreateNotificationRoute))
	s.mux.HandleFunc("GET /api/notification-routes/resolve", s.requireAuth(s.handleResolveNotificationRoutes))
	s.mux.HandleFunc("PATCH /api/notification-routes/{id}", s.requireAuth(s.handleUpdateNotificationRoute))
	s.mux.HandleFunc("DELETE /api/notification-routes/{id}", s.requireAuth(s.handleDeleteNotificationRoute))
	s.mux.HandleFunc("POST /api/attachments", s.requireAuth(s.handleUploadAttachment))
	s.mux.HandleFunc("GET /api/attachments/{id}", s.requireAuth(s.handleGetAttachment))
	s.mux.HandleFunc("GET /api/attachments/{id}/content", s.requireAuth(s.handleDownloadAttachment))
	s.mux.HandleFunc("GET /api/channels", s.requireAuth(s.handleListChannels))
	s.mux.HandleFunc("POST /api/channels", s.requireAuth(s.handleCreateChannel))
	s.mux.HandleFunc("PATCH /api/channels/{target}", s.requireAuth(s.handleUpdateChannel))
	s.mux.HandleFunc("DELETE /api/channels/{target}", s.requireAuth(s.handleDeleteChannel))
	s.mux.HandleFunc("GET /api/channels/{target}/members", s.requireAuth(s.handleListChannelMembers))
	s.mux.HandleFunc("POST /api/channels/{target}/members", s.requireAuth(s.handleUpsertChannelMember))
	s.mux.HandleFunc("PATCH /api/channels/{target}/members/{kind}/{memberID}", s.requireAuth(s.handleUpdateChannelMember))
	s.mux.HandleFunc("DELETE /api/channels/{target}/members/{kind}/{memberID}", s.requireAuth(s.handleDeleteChannelMember))
	s.mux.HandleFunc("GET /api/messages", s.requireAuth(s.handleListMessages))
	s.mux.HandleFunc("GET /api/messages/search", s.requireAuth(s.handleSearchMessages))
	s.mux.HandleFunc("GET /api/messages/saved", s.requireAuth(s.handleListSavedMessages))
	s.mux.HandleFunc("POST /api/messages", s.requireAuth(s.handleCreateMessage))
	s.mux.HandleFunc("POST /api/messages/{id}/save", s.requireAuth(s.handleSaveMessage))
	s.mux.HandleFunc("DELETE /api/messages/{id}/save", s.requireAuth(s.handleUnsaveMessage))
	s.mux.HandleFunc("GET /api/inbox/threads", s.requireAuth(s.handleListThreadInbox))
	s.mux.HandleFunc("POST /api/inbox/threads/{threadID}/read", s.requireAuth(s.handleMarkThreadRead))
	s.mux.HandleFunc("POST /api/inbox/threads/read-all", s.requireAuth(s.handleMarkThreadInboxRead))
	s.mux.HandleFunc("GET /api/tasks", s.requireAuth(s.handleListTasks))
	s.mux.HandleFunc("POST /api/tasks", s.requireAuth(s.handleCreateTask))
	s.mux.HandleFunc("GET /api/tasks/{id}/comments", s.requireAuth(s.handleListTaskComments))
	s.mux.HandleFunc("POST /api/tasks/{id}/comments", s.requireAuth(s.handleCreateTaskComment))
	s.mux.HandleFunc("GET /api/tasks/{id}/timeline", s.requireAuth(s.handleTaskTimeline))
	s.mux.HandleFunc("PATCH /api/tasks/{id}", s.requireAuth(s.handleUpdateTask))
	s.mux.HandleFunc("GET /api/reminders", s.requireAuth(s.handleListReminders))
	s.mux.HandleFunc("POST /api/reminders", s.requireAuth(s.handleCreateReminder))
	s.mux.HandleFunc("GET /api/reminders/{id}/log", s.requireAuth(s.handleReminderLog))
	s.mux.HandleFunc("POST /api/reminders/{id}/cancel", s.requireAuth(s.handleCancelReminder))
	s.mux.HandleFunc("POST /api/reminders/{id}/snooze", s.requireAuth(s.handleSnoozeReminder))
	s.mux.HandleFunc("PATCH /api/reminders/{id}", s.requireAuth(s.handleUpdateReminder))
	s.mux.HandleFunc("GET /api/runtime-presets", s.requireAuth(s.handleListRuntimePresets))
	s.mux.HandleFunc("GET /api/daemon/info", s.requireAuth(s.handleDaemonInfo))
	s.mux.HandleFunc("GET /api/daemon/inventory", s.requireAuth(s.handleDaemonInventory))
	s.mux.HandleFunc("GET /api/daemon/agent-statuses", s.requireAuth(s.handleDaemonAgentStatuses))
	s.mux.HandleFunc("GET /api/daemon/activity", s.requireAuth(s.handleDaemonActivity))
	s.mux.HandleFunc("GET /api/daemon/runs", s.requireAuth(s.handleDaemonRuns))
	s.mux.HandleFunc("GET /api/daemon/events", s.requireAuth(s.handleDaemonEvents))
	s.mux.HandleFunc("POST /api/daemon/agents", s.requireAuth(s.handleCreateDaemonAgent))
	s.mux.HandleFunc("POST /api/daemon/agents/{agentID}/control", s.requireAuth(s.handleControlDaemonAgent))
	s.mux.HandleFunc("POST /api/daemon/agents/{agentID}/messages", s.requireAuth(s.handleSendDaemonAgentDirectMessage))
	s.mux.HandleFunc("POST /api/daemon/enrollments", s.requireAuth(s.handleCreateDaemonEnrollment))
	s.mux.HandleFunc("GET /api/daemon/enrollments/{id}", s.requireAuth(s.handleGetDaemonEnrollment))
	s.mux.HandleFunc("POST /api/daemon/enrollments/{id}/revoke", s.requireAuth(s.handleRevokeDaemonEnrollment))
	s.mux.HandleFunc("GET /api/daemon/enrollments/{id}/install.sh", s.handleDaemonEnrollmentInstallShell)
	s.mux.HandleFunc("GET /api/daemon/enrollments/{id}/install.ps1", s.handleDaemonEnrollmentInstallPowerShell)
	s.mux.HandleFunc("GET /api/daemon/scripts/upgrade.sh", s.handleDaemonUpgradeShell)
	s.mux.HandleFunc("GET /api/daemon/scripts/reinstall.sh", s.handleDaemonReinstallShell)
	s.mux.HandleFunc("GET /api/daemon/scripts/uninstall.sh", s.handleDaemonUninstallShell)
	s.mux.HandleFunc("GET /api/daemon/scripts/upgrade.ps1", s.handleDaemonUpgradePowerShell)
	s.mux.HandleFunc("GET /api/daemon/scripts/reinstall.ps1", s.handleDaemonReinstallPowerShell)
	s.mux.HandleFunc("GET /api/daemon/scripts/uninstall.ps1", s.handleDaemonUninstallPowerShell)
	s.mux.HandleFunc("GET /api/server-events", s.requireAuthOrQueryToken(s.handleServerEvents))
	s.mux.HandleFunc("GET /", s.handleWebConsole)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": version.Version,
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, version.Current())
}

func (s *Server) handleProtocol(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":          "nekode-daemon-v1",
		"protoPath":     ProtocolPath,
		"documentation": "docs/slock-style-daemon-runtime.md",
		"compatibility": "slock-style daemon/server runtime contract",
	})
}

func (s *Server) handleWebConsole(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	webFS, ok := s.webFileSystem()
	if !ok {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" || rel == "." {
		rel = "index.html"
	}
	if hasDotPathSegment(rel) {
		http.NotFound(w, r)
		return
	}
	file, err := webFS.Open(rel)
	if err == nil {
		defer file.Close()
		info, statErr := file.Stat()
		if statErr == nil && !info.IsDir() {
			http.FileServer(webFS).ServeHTTP(w, r)
			return
		}
	}
	if path.Ext(rel) != "" {
		http.NotFound(w, r)
		return
	}
	index, err := webFS.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer index.Close()
	info, err := index.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "index.html", info.ModTime(), index)
}

func (s *Server) webFileSystem() (http.FileSystem, bool) {
	if dir := strings.TrimSpace(s.cfg.WebDistDir); dir != "" {
		if hasIndexFile(os.DirFS(dir)) {
			return http.Dir(dir), true
		}
	}
	embedded, err := fs.Sub(webdist.FS, webdist.Root)
	if err == nil && hasIndexFile(embedded) {
		return http.FS(embedded), true
	}
	for _, candidate := range []string{"web/dist", "/app/web/dist"} {
		if hasIndexFile(os.DirFS(candidate)) {
			return http.Dir(candidate), true
		}
	}
	return nil, false
}

func hasIndexFile(fsys fs.FS) bool {
	info, err := fs.Stat(fsys, "index.html")
	return err == nil && !info.IsDir()
}

func hasDotPathSegment(rel string) bool {
	for _, segment := range strings.Split(rel, "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func (s *Server) BootstrapFromEnvironment(ctx context.Context) error {
	username := strings.TrimSpace(s.cfg.BootstrapAdminUsername)
	password := s.cfg.BootstrapAdminPassword
	displayName := strings.TrimSpace(s.cfg.BootstrapAdminName)
	if username == "" && strings.TrimSpace(password) == "" {
		return nil
	}
	if username == "" || strings.TrimSpace(password) == "" {
		missing := make([]string, 0, 2)
		if username == "" {
			missing = append(missing, "NEKODE_BOOTSTRAP_ADMIN_USERNAME")
		}
		if strings.TrimSpace(password) == "" {
			missing = append(missing, "NEKODE_BOOTSTRAP_ADMIN_PASSWORD")
		}
		s.logger.Warn("bootstrap admin env is incomplete", "missing", strings.Join(missing, ","))
		return nil
	}
	if err := validateCredentials(username, password); err != nil {
		return err
	}
	_, err := s.auth.Bootstrap(ctx, username, password, displayName)
	if errors.Is(err, auth.ErrBootstrapClosed) {
		s.logger.Info("bootstrap admin skipped: already_initialized")
		return nil
	}
	if err != nil {
		return err
	}
	s.logger.Info("bootstrap admin created from environment")
	return nil
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.auth.Initialized(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "setup status failed")
		return
	}
	methods := []string{"env"}
	if !s.cfg.BootstrapDisableWeb {
		methods = append(methods, "web")
	}
	serverID, err := s.cfg.ServerID()
	if err != nil {
		s.logger.Warn("failed to load server id for setup status", "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"initialized":      initialized,
		"webSetupEnabled":  !s.cfg.BootstrapDisableWeb,
		"bootstrapMethods": methods,
		"serverId":         serverID,
		"dataDir":          s.cfg.DataDir,
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if s.cfg.BootstrapDisableWeb {
		initialized, err := s.auth.Initialized(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "setup status failed")
			return
		}
		if initialized {
			writeError(w, http.StatusConflict, "already_initialized")
			return
		}
		writeError(w, http.StatusForbidden, "web setup is disabled")
		return
	}
	var req authRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := validateCredentials(req.Username, req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, err := s.auth.Bootstrap(r.Context(), req.Username, req.Password, req.DisplayName)
	if errors.Is(err, auth.ErrBootstrapClosed) {
		writeError(w, http.StatusConflict, "already_initialized")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bootstrap failed")
		return
	}
	writeJSON(w, http.StatusCreated, token)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	token, err := s.auth.Login(r.Context(), req.Username, req.Password)
	if errors.Is(err, auth.ErrInvalidCredential) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}
	writeJSON(w, http.StatusOK, token)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if err := s.auth.Logout(r.Context(), principal.Session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "logout failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, principalFromContext(r.Context()).User)
}

func (s *Server) handleListInteractionEndpoints(w http.ResponseWriter, r *http.Request) {
	endpoints, err := s.store.ListInteractionEndpoints(r.Context(), intQuery(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list interaction endpoints failed")
		return
	}
	for i := range endpoints {
		endpoints[i] = redactInteractionEndpoint(endpoints[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": endpoints})
}

func (s *Server) handleListIMProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": imadapter.ListProviders()})
}

func (s *Server) handleGetIMEffectivePolicy(w http.ResponseWriter, r *http.Request) {
	endpointID := strings.TrimSpace(r.URL.Query().Get("endpointId"))
	conversationID := strings.TrimSpace(r.URL.Query().Get("conversationId"))
	if endpointID == "" || conversationID == "" {
		writeError(w, http.StatusBadRequest, "endpointId and conversationId are required")
		return
	}
	endpoint, err := s.store.GetInteractionEndpoint(r.Context(), endpointID)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction endpoint not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read interaction endpoint failed")
		return
	}
	if !strings.EqualFold(endpoint.Kind, "im") {
		writeError(w, http.StatusBadRequest, "interaction endpoint is not an IM endpoint")
		return
	}
	policy, err := impolicy.Resolve(endpoint.ID, endpoint.Provider, endpoint.ConfigJSON, conversationID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (s *Server) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	endpointID := strings.TrimSpace(r.PathValue("endpointID"))
	if endpointID == "" {
		writeError(w, http.StatusBadRequest, "endpointID is required")
		return
	}
	endpoint, err := s.store.GetInteractionEndpoint(r.Context(), endpointID)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction endpoint not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read interaction endpoint failed")
		return
	}
	if !strings.EqualFold(endpoint.Kind, "im") || !strings.EqualFold(endpoint.Provider, imadapter.ProviderTelegram) {
		writeError(w, http.StatusBadRequest, "interaction endpoint is not a Telegram IM endpoint")
		return
	}
	if !endpoint.InboundEnabled {
		writeError(w, http.StatusForbidden, "interaction endpoint inbound is disabled")
		return
	}
	cfg, err := imtelegram.ConfigFromEndpoint(endpoint)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read telegram webhook failed")
		return
	}
	coord := imcoord.New(s.store, nil)
	result, err := (imtelegram.Webhook{
		Config:      cfg,
		Normalizer:  imadapter.Normalizer{},
		Coordinator: coord,
	}).Handle(r.Context(), r.Header, body)
	if errors.Is(err, imtelegram.ErrUnauthorizedWebhook) {
		writeError(w, http.StatusUnauthorized, "invalid telegram webhook secret")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"ignored":   result.Ignored,
		"reason":    result.Reason,
		"messageId": result.Message.ID,
	})
}

func (s *Server) handleFeishuCallback(w http.ResponseWriter, r *http.Request) {
	endpointID := strings.TrimSpace(r.PathValue("endpointID"))
	if endpointID == "" {
		writeError(w, http.StatusBadRequest, "endpointID is required")
		return
	}
	endpoint, err := s.store.GetInteractionEndpoint(r.Context(), endpointID)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction endpoint not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read interaction endpoint failed")
		return
	}
	if !strings.EqualFold(endpoint.Kind, "im") || !strings.EqualFold(endpoint.Provider, imadapter.ProviderFeishu) {
		writeError(w, http.StatusBadRequest, "interaction endpoint is not a Feishu IM endpoint")
		return
	}
	if !endpoint.InboundEnabled {
		writeError(w, http.StatusForbidden, "interaction endpoint inbound is disabled")
		return
	}
	cfg, err := imfeishu.ConfigFromEndpoint(endpoint)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read feishu callback failed")
		return
	}
	result, err := (imfeishu.Callback{
		Config:      cfg,
		Normalizer:  imadapter.Normalizer{},
		Coordinator: imcoord.New(s.store, nil),
	}).Handle(r.Context(), r.Header, body)
	if errors.Is(err, imfeishu.ErrUnauthorizedCallback) {
		writeError(w, http.StatusUnauthorized, "invalid feishu verification token")
		return
	}
	if errors.Is(err, imfeishu.ErrEncryptedUnsupported) {
		writeError(w, http.StatusBadRequest, "encrypted feishu callbacks are not supported yet")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if result.Challenge != "" {
		writeJSON(w, http.StatusOK, map[string]string{"challenge": result.Challenge})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"ignored":   result.Ignored,
		"reason":    result.Reason,
		"messageId": result.Message.ID,
	})
}

func (s *Server) handleWeChatCallback(w http.ResponseWriter, r *http.Request) {
	endpointID := strings.TrimSpace(r.PathValue("endpointID"))
	if endpointID == "" {
		writeError(w, http.StatusBadRequest, "endpointID is required")
		return
	}
	endpoint, err := s.store.GetInteractionEndpoint(r.Context(), endpointID)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction endpoint not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read interaction endpoint failed")
		return
	}
	if !strings.EqualFold(endpoint.Kind, "im") || !strings.EqualFold(endpoint.Provider, imadapter.ProviderWeixin) {
		writeError(w, http.StatusBadRequest, "interaction endpoint is not a WeChat official account endpoint")
		return
	}
	if !endpoint.InboundEnabled {
		writeError(w, http.StatusForbidden, "interaction endpoint inbound is disabled")
		return
	}
	cfg, err := imwechat.ConfigFromEndpoint(endpoint)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := imwechat.Query{
		Signature: r.URL.Query().Get("signature"),
		Timestamp: r.URL.Query().Get("timestamp"),
		Nonce:     r.URL.Query().Get("nonce"),
		Echo:      r.URL.Query().Get("echostr"),
	}
	webhook := imwechat.Webhook{Config: cfg, Normalizer: imadapter.Normalizer{}, Coordinator: imcoord.New(s.store, nil)}
	if r.Method == http.MethodGet {
		echo, err := webhook.VerifyURL(query)
		if errors.Is(err, imwechat.ErrUnauthorizedWebhook) {
			writeError(w, http.StatusUnauthorized, "invalid wechat callback signature")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(echo))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read wechat callback failed")
		return
	}
	result, err := webhook.Handle(r.Context(), query, body)
	if errors.Is(err, imwechat.ErrUnauthorizedWebhook) {
		writeError(w, http.StatusUnauthorized, "invalid wechat callback signature")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"ignored":   result.Ignored,
		"reason":    result.Reason,
		"messageId": result.Message.ID,
	})
}

func redactInteractionEndpoint(endpoint storage.InteractionEndpoint) storage.InteractionEndpoint {
	if !strings.EqualFold(endpoint.Kind, "im") {
		return endpoint
	}
	redacted, err := imadapter.RedactConfig(endpoint.Provider, endpoint.ConfigJSON)
	if err == nil {
		endpoint.ConfigJSON = redacted
	}
	return endpoint
}

func (s *Server) handleCreateInteractionEndpoint(w http.ResponseWriter, r *http.Request) {
	var req endpointRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	endpoint := storage.InteractionEndpoint{
		Kind:            strings.TrimSpace(req.Kind),
		Provider:        strings.TrimSpace(req.Provider),
		DisplayName:     strings.TrimSpace(req.DisplayName),
		TargetPrefix:    strings.TrimSpace(req.TargetPrefix),
		InboundEnabled:  req.InboundEnabled,
		OutboundEnabled: req.OutboundEnabled,
		AuthMode:        strings.TrimSpace(req.AuthMode),
		ConfigJSON:      normalizedJSON(req.ConfigJSON),
	}
	if endpoint.Kind == "" || endpoint.Provider == "" || endpoint.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "kind, provider, and displayName are required")
		return
	}
	if endpoint.TargetPrefix == "" {
		endpoint.TargetPrefix = "#"
	}
	if endpoint.AuthMode == "" {
		endpoint.AuthMode = "bearer"
	}
	if strings.EqualFold(endpoint.Kind, "im") {
		endpoint.Provider = imadapter.CanonicalProvider(endpoint.Provider)
		if err := imadapter.ValidateConfig(endpoint.Provider, endpoint.ConfigJSON); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	created, err := s.store.CreateInteractionEndpoint(r.Context(), endpoint)
	if errors.Is(err, storage.ErrConflict) {
		writeError(w, http.StatusConflict, "interaction endpoint already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create interaction endpoint failed")
		return
	}
	writeJSON(w, http.StatusCreated, redactInteractionEndpoint(created))
}

func (s *Server) handleUpdateInteractionEndpoint(w http.ResponseWriter, r *http.Request) {
	var req endpointPatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	endpoint, err := s.store.GetInteractionEndpoint(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction endpoint not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read interaction endpoint failed")
		return
	}
	patch := storage.InteractionEndpointPatch{
		DisplayName:     optionalTrimmed(req.DisplayName),
		TargetPrefix:    optionalTrimmed(req.TargetPrefix),
		InboundEnabled:  req.InboundEnabled,
		OutboundEnabled: req.OutboundEnabled,
		AuthMode:        optionalTrimmed(req.AuthMode),
		ConfigJSON:      optionalTrimmed(req.ConfigJSON),
	}
	if patch.ConfigJSON != nil && strings.EqualFold(endpoint.Kind, "im") {
		if strings.Contains(*patch.ConfigJSON, `"***"`) {
			patch.ConfigJSON = nil
		} else {
			if err := imadapter.ValidateConfig(endpoint.Provider, *patch.ConfigJSON); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	updated, err := s.store.UpdateInteractionEndpoint(r.Context(), endpoint.ID, patch)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction endpoint not found")
		return
	}
	if errors.Is(err, storage.ErrConflict) {
		writeError(w, http.StatusConflict, "interaction endpoint already exists")
		return
	}
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "invalid interaction endpoint update")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update interaction endpoint failed")
		return
	}
	writeJSON(w, http.StatusOK, redactInteractionEndpoint(updated))
}

func (s *Server) handleDeleteInteractionEndpoint(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteInteractionEndpoint(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction endpoint not found")
		return
	}
	if errors.Is(err, storage.ErrConflict) {
		writeError(w, http.StatusConflict, "interaction endpoint is still referenced by notification routes or deliveries")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete interaction endpoint failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTestInteractionEndpoint(w http.ResponseWriter, r *http.Request) {
	endpoint, err := s.store.GetInteractionEndpoint(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction endpoint not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read interaction endpoint failed")
		return
	}
	checks := []map[string]any{
		{"name": "schema", "ok": endpoint.Kind != "" && endpoint.Provider != ""},
		{"name": "inbound", "ok": endpoint.InboundEnabled},
		{"name": "outbound", "ok": endpoint.OutboundEnabled},
	}
	if strings.EqualFold(endpoint.Kind, "im") {
		if err := imadapter.ValidateConfig(endpoint.Provider, endpoint.ConfigJSON); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ready":       false,
				"runtimeLive": false,
				"summary":     err.Error(),
				"checks":      append(checks, map[string]any{"name": "provider_config", "ok": false, "detail": err.Error()}),
			})
			return
		}
		redacted := redactInteractionEndpoint(endpoint)
		writeJSON(w, http.StatusOK, map[string]any{
			"ready":       true,
			"runtimeLive": false,
			"summary":     "Endpoint configuration is valid. Provider receive/send runtime still requires its platform smoke task.",
			"checks": append(checks,
				map[string]any{"name": "provider_config", "ok": true},
				map[string]any{"name": "provider_runtime", "ok": false, "detail": "Not a live provider send/receive test."},
			),
			"endpoint": redacted,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ready":       true,
		"runtimeLive": false,
		"summary":     "Endpoint metadata is reachable. No external provider runtime was exercised.",
		"checks":      checks,
		"endpoint":    endpoint,
	})
}

func (s *Server) handleCreateBindingSession(w http.ResponseWriter, r *http.Request) {
	endpoint, ok := s.readBindingEndpoint(w, r)
	if !ok {
		return
	}
	var req bindingSessionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	method := strings.ToLower(strings.TrimSpace(req.Method))
	if method == "" {
		method = imbinding.MethodQRCode
	}
	if imadapter.CanonicalProvider(endpoint.Provider) == imadapter.ProviderWeixin &&
		method == imbinding.MethodQRCode &&
		!imwechat.SupportsILinkBinding(endpoint) {
		writeError(w, http.StatusUnprocessableEntity, "weixin QR binding requires mode=ilink")
		return
	}
	req.Method = method
	session, err := s.imBindings.Create(endpoint, req.Method)
	if errors.Is(err, imbinding.ErrEndpointUnsupported) {
		writeError(w, http.StatusUnprocessableEntity, "binding method is not available for this endpoint")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create binding session failed")
		return
	}
	if imadapter.CanonicalProvider(endpoint.Provider) == imadapter.ProviderWeixin {
		if updated, err := imwechat.StartILinkBindingSession(endpoint, s.imBindings, session); err == nil {
			session = updated
		} else {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleGetBindingSession(w http.ResponseWriter, r *http.Request) {
	endpoint, ok := s.readBindingEndpoint(w, r)
	if !ok {
		return
	}
	session, err := s.imBindings.Get(endpoint, r.PathValue("sessionID"))
	if errors.Is(err, imbinding.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "binding session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read binding session failed")
		return
	}
	if imadapter.CanonicalProvider(endpoint.Provider) == imadapter.ProviderWeixin {
		update, err := imwechat.PollILinkBindingSession(endpoint, s.imBindings, session)
		if update.Session.ID != "" {
			session = update.Session
		}
		if err == nil && update.Bound && update.ConfigJSON != "" {
			if _, err := s.store.UpdateInteractionEndpoint(r.Context(), endpoint.ID, storage.InteractionEndpointPatch{ConfigJSON: &update.ConfigJSON}); err != nil {
				writeError(w, http.StatusInternalServerError, "persist weixin binding failed")
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleCancelBindingSession(w http.ResponseWriter, r *http.Request) {
	endpoint, ok := s.readBindingEndpoint(w, r)
	if !ok {
		return
	}
	session, err := s.imBindings.Cancel(endpoint, r.PathValue("sessionID"))
	if errors.Is(err, imbinding.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "binding session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cancel binding session failed")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) readBindingEndpoint(w http.ResponseWriter, r *http.Request) (storage.InteractionEndpoint, bool) {
	endpoint, err := s.store.GetInteractionEndpoint(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction endpoint not found")
		return storage.InteractionEndpoint{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read interaction endpoint failed")
		return storage.InteractionEndpoint{}, false
	}
	if !strings.EqualFold(endpoint.Kind, "im") {
		writeError(w, http.StatusBadRequest, "interaction endpoint is not an IM endpoint")
		return storage.InteractionEndpoint{}, false
	}
	return endpoint, true
}

func (s *Server) handleListNotificationRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := s.store.ListNotificationRoutes(r.Context(), storage.NotificationRouteListOptions{
		Target:     strings.TrimSpace(r.URL.Query().Get("target")),
		ThreadID:   strings.TrimSpace(r.URL.Query().Get("threadId")),
		EndpointID: strings.TrimSpace(r.URL.Query().Get("endpointId")),
		EventKind:  strings.TrimSpace(r.URL.Query().Get("eventKind")),
		Enabled:    optionalBoolQuery(r, "enabled"),
		Limit:      intQuery(r, "limit", 100),
	})
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "invalid notification route filter")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list notification routes failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": routes})
}

func (s *Server) handleCreateNotificationRoute(w http.ResponseWriter, r *http.Request) {
	var req notificationRouteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	route := storage.NotificationRoute{
		Target:     strings.TrimSpace(req.Target),
		ThreadID:   strings.TrimSpace(req.ThreadID),
		EndpointID: strings.TrimSpace(req.EndpointID),
		EventKind:  strings.TrimSpace(req.EventKind),
		Preference: strings.TrimSpace(req.Preference),
		Enabled:    req.Enabled == nil || *req.Enabled,
		ConfigJSON: normalizedJSON(req.ConfigJSON),
	}
	if route.Target == "" || route.EndpointID == "" {
		writeError(w, http.StatusBadRequest, "target and endpointId are required")
		return
	}
	if _, err := s.store.GetInteractionEndpoint(r.Context(), route.EndpointID); errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "endpointId must reference an existing endpoint")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "get interaction endpoint failed")
		return
	}
	created, err := s.store.CreateNotificationRoute(r.Context(), route)
	if errors.Is(err, storage.ErrConflict) {
		writeError(w, http.StatusConflict, "notification route already exists")
		return
	}
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "invalid notification route")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create notification route failed")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleResolveNotificationRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := s.store.ResolveNotificationRoutes(r.Context(), storage.NotificationRouteResolveOptions{
		Target:    strings.TrimSpace(r.URL.Query().Get("target")),
		ThreadID:  strings.TrimSpace(r.URL.Query().Get("threadId")),
		EventKind: strings.TrimSpace(r.URL.Query().Get("eventKind")),
		Limit:     intQuery(r, "limit", 100),
	})
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "target and valid eventKind are required")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve notification routes failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": routes})
}

func (s *Server) handleUpdateNotificationRoute(w http.ResponseWriter, r *http.Request) {
	var req notificationRoutePatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	patch := storage.NotificationRoutePatch{
		Target:     optionalTrimmed(req.Target),
		ThreadID:   optionalTrimmed(req.ThreadID),
		EndpointID: optionalTrimmed(req.EndpointID),
		EventKind:  optionalTrimmed(req.EventKind),
		Preference: optionalTrimmed(req.Preference),
		Enabled:    req.Enabled,
		ConfigJSON: optionalTrimmed(req.ConfigJSON),
	}
	updated, err := s.store.UpdateNotificationRoute(r.Context(), r.PathValue("id"), patch)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "notification route not found")
		return
	}
	if errors.Is(err, storage.ErrConflict) {
		writeError(w, http.StatusConflict, "notification route already exists")
		return
	}
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "invalid notification route update")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update notification route failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteNotificationRoute(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteNotificationRoute(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "notification route not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete notification route failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, immedia.DefaultMaxAttachmentBytes+(1<<20))
	if err := r.ParseMultipartForm(immedia.DefaultMaxAttachmentBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart attachment upload")
		return
	}
	target := strings.TrimSpace(r.FormValue("target"))
	if target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	principal := principalFromContext(r.Context())
	stored, err := immedia.Store(s.cfg.DataDir, immedia.StoreInput{
		Target:   target,
		OwnerID:  principal.User.ID,
		Filename: header.Filename,
		MimeType: header.Header.Get("Content-Type"),
		Content:  file,
		MaxBytes: immedia.DefaultMaxAttachmentBytes,
	})
	if errors.Is(err, immedia.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, immedia.ErrTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "attachment exceeds maximum size")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save attachment metadata failed")
		return
	}
	writeJSON(w, http.StatusCreated, stored.Attachment)
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, ok := s.readAttachmentForRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, attachment)
}

func (s *Server) handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, ok := s.readAttachmentForRequest(w, r)
	if !ok {
		return
	}
	contentPath := immedia.ContentPath(s.cfg.DataDir, attachment)
	file, err := os.Open(contentPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment content not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "attachment content not found")
		return
	}
	disposition := "inline"
	if !immedia.IsInlineAttachment(attachment.MimeType) {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", attachment.MimeType)
	w.Header().Set("Content-Disposition", disposition+`; filename="`+strings.ReplaceAll(attachment.Filename, `"`, "")+`"`)
	http.ServeContent(w, r, attachment.Filename, info.ModTime(), file)
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.channelSummaries(r.Context(), principalFromContext(r.Context()).User, boolQuery(r, "joinedOnly"), intQuery(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list channels failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": channels})
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context()).User
	if !canManageChannels(principal) {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}
	var req channelRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	channel, err := s.store.CreateChannel(r.Context(), storage.ChannelSummary{
		Target:          req.Target,
		DisplayName:     req.DisplayName,
		ChannelType:     "channel",
		Visibility:      req.Visibility,
		CreatedByUserID: principal.ID,
	})
	if err != nil {
		writeStorageError(w, err, "create channel failed")
		return
	}
	if _, err := s.store.UpsertChannelMember(r.Context(), storage.ChannelMember{
		Target:      channel.Target,
		MemberID:    principal.ID,
		Username:    principal.Username,
		DisplayName: firstNonEmptyString(principal.DisplayName, principal.Username, "Admin"),
		Kind:        "human",
		Role:        "admin",
	}); err != nil {
		writeStorageError(w, err, "create channel owner member failed")
		return
	}
	channel.MemberCount = 1
	channel.CurrentUserRole = "admin"
	channel.Joined = true
	writeJSON(w, http.StatusCreated, channel)
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context()).User
	target := pathChannelTarget(r)
	if !s.canManageChannel(r.Context(), principal, target) {
		writeError(w, http.StatusForbidden, "channel admin role required")
		return
	}
	var req channelPatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	channel, err := s.store.UpdateChannel(r.Context(), target, storage.ChannelPatch{
		DisplayName: req.DisplayName,
		Visibility:  req.Visibility,
	})
	if err != nil {
		writeStorageError(w, err, "update channel failed")
		return
	}
	s.decorateChannelSummary(r.Context(), principal, &channel)
	writeJSON(w, http.StatusOK, channel)
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context()).User
	target := pathChannelTarget(r)
	if !s.canManageChannel(r.Context(), principal, target) {
		writeError(w, http.StatusForbidden, "channel admin role required")
		return
	}
	if err := s.store.DeleteChannel(r.Context(), target); err != nil {
		writeStorageError(w, err, "delete channel failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListChannelMembers(w http.ResponseWriter, r *http.Request) {
	target := pathChannelTarget(r)
	if target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}
	if !s.canReadChannel(r.Context(), principalFromContext(r.Context()).User, target) {
		writeError(w, http.StatusForbidden, "channel membership is private")
		return
	}
	members, err := s.channelMembers(r.Context(), principalFromContext(r.Context()).User, target)
	if err != nil {
		writeStorageError(w, err, "list channel members failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": members})
}

func (s *Server) handleUpsertChannelMember(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context()).User
	target := pathChannelTarget(r)
	if !s.canManageChannel(r.Context(), principal, target) {
		writeError(w, http.StatusForbidden, "channel admin role required")
		return
	}
	var req channelMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	_, existed := s.findStoredChannelMember(r.Context(), target, req.Kind, req.MemberID)
	member, err := s.store.UpsertChannelMember(r.Context(), storage.ChannelMember{
		Target:      target,
		MemberID:    req.MemberID,
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Kind:        req.Kind,
		Role:        req.Role,
	})
	if err != nil {
		writeStorageError(w, err, "upsert channel member failed")
		return
	}
	if !existed {
		if err := s.recordChannelMembershipSystemMessage(r.Context(), "member_added", member); err != nil {
			writeStorageError(w, err, "record channel member system message failed")
			return
		}
	}
	writeJSON(w, http.StatusCreated, member)
}

func (s *Server) handleUpdateChannelMember(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context()).User
	target := pathChannelTarget(r)
	if !s.canManageChannel(r.Context(), principal, target) {
		writeError(w, http.StatusForbidden, "channel admin role required")
		return
	}
	var req channelMemberPatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	member, err := s.store.UpdateChannelMember(r.Context(), target, r.PathValue("kind"), r.PathValue("memberID"), storage.ChannelMemberPatch{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Role:        req.Role,
	})
	if err != nil {
		writeStorageError(w, err, "update channel member failed")
		return
	}
	writeJSON(w, http.StatusOK, member)
}

func (s *Server) handleDeleteChannelMember(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context()).User
	target := pathChannelTarget(r)
	if !s.canManageChannel(r.Context(), principal, target) {
		writeError(w, http.StatusForbidden, "channel admin role required")
		return
	}
	member, _ := s.findStoredChannelMember(r.Context(), target, r.PathValue("kind"), r.PathValue("memberID"))
	if err := s.store.DeleteChannelMember(r.Context(), target, r.PathValue("kind"), r.PathValue("memberID")); err != nil {
		writeStorageError(w, err, "delete channel member failed")
		return
	}
	if member.MemberID != "" {
		if err := s.recordChannelMembershipSystemMessage(r.Context(), "member_removed", member); err != nil {
			writeStorageError(w, err, "record channel member system message failed")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) findStoredChannelMember(ctx context.Context, target, kind, memberID string) (storage.ChannelMember, bool) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "human"
	}
	memberID = strings.TrimSpace(memberID)
	members, err := s.store.ListChannelMembers(ctx, target, 200)
	if err != nil {
		return storage.ChannelMember{}, false
	}
	for _, member := range members {
		if strings.EqualFold(member.Kind, kind) && member.MemberID == memberID {
			return member, true
		}
	}
	return storage.ChannelMember{}, false
}

func (s *Server) recordChannelMembershipSystemMessage(ctx context.Context, operation string, member storage.ChannelMember) error {
	display := firstNonEmptyString(member.DisplayName, member.Username, member.MemberID)
	action := "joined"
	if operation == "member_removed" {
		action = "left"
	}
	metadata, err := json.Marshal(map[string]string{
		"kind":       "channel_membership",
		"operation":  operation,
		"memberId":   member.MemberID,
		"memberKind": member.Kind,
		"role":       member.Role,
	})
	if err != nil {
		return err
	}
	message, err := s.store.CreateMessage(ctx, storage.Message{
		Target:            member.Target,
		Role:              "system",
		Content:           fmt.Sprintf("%s %s %s as %s.", display, action, member.Target, member.Role),
		SenderDisplayName: "Nekode",
		SenderKind:        "system",
		MetadataJSON:      string(metadata),
	})
	if err != nil {
		return err
	}
	return s.daemon.RecordMessageMutation(ctx, message, daemonv1.EventOperation_EVENT_OPERATION_APPENDED)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	if target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}
	messages, err := s.store.ListMessages(r.Context(), target, strings.TrimSpace(r.URL.Query().Get("threadId")),
		intQuery(r, "limit", 50))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list messages failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": messages})
}

func (s *Server) channelSummaries(ctx context.Context, user storage.User, joinedOnly bool, limit int) ([]storage.ChannelSummary, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	persisted, err := s.store.ListChannels(ctx, storage.ChannelListOptions{
		JoinedOnly: joinedOnly,
		UserID:     user.ID,
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{
		"#general": {},
		"#ops":     {},
		"#release": {},
	}
	targets := []string{"#general", "#ops", "#release"}
	out := make([]storage.ChannelSummary, 0, len(persisted)+len(targets))
	for _, channel := range persisted {
		seen[channel.Target] = struct{}{}
		s.decorateChannelSummary(ctx, user, &channel)
		if !channel.Joined && channel.Visibility == "private" {
			continue
		}
		if joinedOnly && !channel.Joined {
			continue
		}
		out = append(out, channel)
		if len(out) >= limit {
			return out, nil
		}
	}
	messageTargets, err := s.store.ListMessageTargets(ctx, limit)
	if err != nil {
		return nil, err
	}
	taskTargets, err := s.store.ListTaskTargets(ctx, limit)
	if err != nil {
		return nil, err
	}
	for _, target := range append(messageTargets, taskTargets...) {
		if _, ok := seen[target]; ok || !strings.HasPrefix(target, "#") {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	for _, target := range targets {
		role := s.channelRoleFor(ctx, user, target)
		joined := role != ""
		if joinedOnly && !joined {
			continue
		}
		visibility := "public"
		if s.isPrivateChannel(ctx, target) {
			visibility = "private"
		}
		if !joined && visibility == "private" {
			continue
		}
		members, err := s.channelMembers(ctx, user, target)
		if err != nil {
			return nil, err
		}
		out = append(out, storage.ChannelSummary{
			Target:          target,
			DisplayName:     strings.TrimPrefix(target, "#"),
			ChannelType:     "channel",
			Visibility:      visibility,
			Joined:          joined,
			MemberCount:     len(members),
			CurrentUserRole: role,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Server) decorateChannelSummary(ctx context.Context, user storage.User, channel *storage.ChannelSummary) {
	role := s.channelRoleFor(ctx, user, channel.Target)
	channel.CurrentUserRole = role
	channel.Joined = role != ""
	members, err := s.channelMembers(ctx, user, channel.Target)
	if err == nil {
		channel.MemberCount = len(members)
	}
}

func (s *Server) channelMembers(ctx context.Context, user storage.User, target string) ([]storage.ChannelMember, error) {
	persisted, err := s.store.ListChannelMembers(ctx, target, 200)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if len(persisted) > 0 {
		return persisted, nil
	}
	members := []storage.ChannelMember{
		{
			Target:         target,
			MemberID:       user.ID,
			Username:       user.Username,
			DisplayName:    firstNonEmptyString(user.DisplayName, user.Username, "Signed in user"),
			Kind:           "human",
			Role:           firstNonEmptyString(s.channelRoleFor(ctx, user, target), "viewer"),
			JoinedTimeUnix: user.CreatedUnix,
		},
	}
	if target == "#general" || target == "#release" {
		members = append(members, storage.ChannelMember{
			Target:         target,
			MemberID:       "agent:onboarding",
			DisplayName:    "Onboarding Agent",
			Kind:           "agent",
			Role:           "member",
			JoinedTimeUnix: user.CreatedUnix,
		})
	}
	return members, nil
}

func canReadChannel(user storage.User, target string) bool {
	return !isPrivateChannel(target) || channelRoleFor(user, target) != ""
}

func channelRoleFor(user storage.User, target string) string {
	if strings.EqualFold(user.Role, "owner") || strings.EqualFold(user.Role, "admin") {
		return "admin"
	}
	if isPrivateChannel(target) {
		return ""
	}
	return "member"
}

func isPrivateChannel(target string) bool {
	name := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(target), "#"))
	return strings.HasPrefix(name, "private-") || strings.HasPrefix(name, "priv-")
}

func (s *Server) canReadChannel(ctx context.Context, user storage.User, target string) bool {
	return !s.isPrivateChannel(ctx, target) || s.channelRoleFor(ctx, user, target) != ""
}

func (s *Server) canManageChannel(ctx context.Context, user storage.User, target string) bool {
	role := s.channelRoleFor(ctx, user, target)
	return role == "admin"
}

func canManageChannels(user storage.User) bool {
	return strings.EqualFold(user.Role, "owner") || strings.EqualFold(user.Role, "admin")
}

func (s *Server) channelRoleFor(ctx context.Context, user storage.User, target string) string {
	if canManageChannels(user) {
		return "admin"
	}
	members, err := s.store.ListChannelMembers(ctx, target, 200)
	if err == nil {
		for _, member := range members {
			if member.Kind == "human" && member.MemberID == user.ID {
				return member.Role
			}
		}
		if len(members) > 0 {
			return ""
		}
	}
	if s.isPrivateChannel(ctx, target) {
		return ""
	}
	return "member"
}

func (s *Server) isPrivateChannel(ctx context.Context, target string) bool {
	if channel, err := s.store.GetChannel(ctx, target); err == nil {
		return channel.Visibility == "private"
	}
	return isPrivateChannel(target)
}

func pathChannelTarget(r *http.Request) string {
	target := strings.TrimSpace(r.PathValue("target"))
	if target == "" {
		return ""
	}
	return "#" + strings.TrimPrefix(target, "#")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Server) handleSearchMessages(w http.ResponseWriter, r *http.Request) {
	messages, err := s.store.SearchMessages(r.Context(), storage.MessageSearchOptions{
		Query:         strings.TrimSpace(r.URL.Query().Get("q")),
		Target:        strings.TrimSpace(r.URL.Query().Get("target")),
		SenderHandle:  strings.TrimSpace(r.URL.Query().Get("sender")),
		HasAttachment: boolQuery(r, "hasAttachment"),
		Sort:          strings.TrimSpace(r.URL.Query().Get("sort")),
		Limit:         intQuery(r, "limit", 50),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search messages failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": messages})
}

func (s *Server) handleCreateMessage(w http.ResponseWriter, r *http.Request) {
	var req messageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	target := strings.TrimSpace(req.Target)
	content := strings.TrimSpace(req.Content)
	if target == "" || content == "" {
		writeError(w, http.StatusBadRequest, "target and content are required")
		return
	}
	principal := principalFromContext(r.Context())
	attachments, err := s.loadMessageAttachments(strings.TrimSpace(req.Target), req.AttachmentIDs)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, storage.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "user"
	}
	message, err := s.store.CreateMessage(r.Context(), storage.Message{
		Target:            target,
		ThreadID:          strings.TrimSpace(req.ThreadID),
		Role:              role,
		Content:           content,
		ReplyToMessageID:  strings.TrimSpace(req.ReplyToMessageID),
		SenderUserID:      principal.User.ID,
		SenderDisplayName: principal.User.DisplayName,
		SenderKind:        "human",
		SourceEndpointID:  strings.TrimSpace(req.SourceEndpointID),
		ExternalMessageID: strings.TrimSpace(req.ExternalMessageID),
		MetadataJSON:      normalizedJSON(req.MetadataJSON),
		Attachments:       attachments,
		RequestID:         strings.TrimSpace(req.RequestID),
	})
	if errors.Is(err, storage.ErrConflict) {
		writeError(w, http.StatusConflict, "duplicate request id")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create message failed")
		return
	}
	if err := s.daemon.RecordMessageMutation(r.Context(), message, daemonv1.EventOperation_EVENT_OPERATION_APPENDED); err != nil {
		writeError(w, http.StatusInternalServerError, "append message event failed")
		return
	}
	writeJSON(w, http.StatusCreated, message)
}

func (s *Server) handleListThreadInbox(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	items, err := s.store.ListThreadInbox(r.Context(), principal.User.ID,
		strings.TrimSpace(r.URL.Query().Get("targetPrefix")), intQuery(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list thread inbox failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleMarkThreadRead(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	var req threadReadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	target := strings.TrimSpace(req.Target)
	threadID := strings.TrimSpace(r.PathValue("threadID"))
	if target == "" || threadID == "" {
		writeError(w, http.StatusBadRequest, "target and threadId are required")
		return
	}
	if err := s.store.MarkThreadRead(r.Context(), principal.User.ID, target, threadID); errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "mark thread read failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMarkThreadInboxRead(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	var req threadInboxReadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.MarkThreadInboxRead(r.Context(), principal.User.ID, strings.TrimSpace(req.TargetPrefix)); err != nil {
		writeError(w, http.StatusInternalServerError, "mark thread inbox read failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSaveMessage(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	if target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}
	principal := principalFromContext(r.Context())
	saved, err := s.store.SaveMessage(r.Context(), target, r.PathValue("id"), principal.User.ID, "")
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save message failed")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleUnsaveMessage(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	if target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}
	principal := principalFromContext(r.Context())
	saved, err := s.store.UnsaveMessage(r.Context(), target, r.PathValue("id"), principal.User.ID, "")
	if errors.Is(err, storage.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]bool{"removed": false})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "unsave message failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": true, "savedMessage": saved})
}

func (s *Server) handleListSavedMessages(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	saved, err := s.store.ListSavedMessagesWithOptions(r.Context(), storage.SavedMessageListOptions{
		Target:        strings.TrimSpace(r.URL.Query().Get("target")),
		UserID:        principal.User.ID,
		Query:         strings.TrimSpace(r.URL.Query().Get("q")),
		HasAttachment: boolQuery(r, "hasAttachment"),
		Limit:         intQuery(r, "limit", 50),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list saved messages failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": saved})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTasks(r.Context(), strings.TrimSpace(r.URL.Query().Get("state")),
		strings.TrimSpace(r.URL.Query().Get("target")), intQuery(r, "limit", 100))
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "invalid task state")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list tasks failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tasks})
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req taskRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	summary := strings.TrimSpace(req.Summary)
	target := strings.TrimSpace(req.Target)
	if summary == "" || target == "" {
		writeError(w, http.StatusBadRequest, "summary and target are required")
		return
	}
	principal := principalFromContext(r.Context())
	task, err := s.store.CreateTask(r.Context(), storage.Task{
		Summary:         summary,
		Description:     strings.TrimSpace(req.Description),
		State:           strings.TrimSpace(req.State),
		Target:          target,
		AssigneeID:      strings.TrimSpace(req.AssigneeID),
		CreatedByUserID: principal.User.ID,
		BlockedReason:   strings.TrimSpace(req.BlockedReason),
	})
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "invalid task state")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create task failed")
		return
	}
	if err := s.daemon.RecordTaskMutation(r.Context(), task, daemonv1.EventOperation_EVENT_OPERATION_CREATED); err != nil {
		writeError(w, http.StatusInternalServerError, "append task event failed")
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	var req taskPatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	patch := storage.TaskPatch{
		Summary:       optionalTrimmed(req.Summary),
		Description:   optionalTrimmed(req.Description),
		State:         optionalTrimmed(req.State),
		AssigneeID:    optionalTrimmed(req.AssigneeID),
		BlockedReason: optionalTrimmed(req.BlockedReason),
	}
	task, err := s.store.UpdateTask(r.Context(), r.PathValue("id"), patch)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "invalid task state")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update task failed")
		return
	}
	operation := daemonv1.EventOperation_EVENT_OPERATION_UPDATED
	if req.State != nil {
		operation = daemonv1.EventOperation_EVENT_OPERATION_STATE_CHANGED
	}
	if err := s.daemon.RecordTaskMutation(r.Context(), task, operation); err != nil {
		writeError(w, http.StatusInternalServerError, "append task event failed")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleListTaskComments(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(r.PathValue("id"))
	if _, err := s.store.GetTask(r.Context(), taskID); errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "get task failed")
		return
	}
	messages, err := s.store.ListTaskComments(r.Context(), taskID, intQuery(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list task comments failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": messages})
}

func (s *Server) handleCreateTaskComment(w http.ResponseWriter, r *http.Request) {
	var req taskCommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	taskID := strings.TrimSpace(r.PathValue("id"))
	task, err := s.store.GetTask(r.Context(), taskID)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get task failed")
		return
	}
	principal := principalFromContext(r.Context())
	message, err := s.store.CreateMessage(r.Context(), storage.Message{
		Target:            task.Target,
		ThreadID:          task.ID,
		Role:              "user",
		Content:           content,
		SenderUserID:      principal.User.ID,
		SenderDisplayName: principal.User.DisplayName,
		SenderKind:        "human",
		MetadataJSON:      "{}",
		RequestID:         strings.TrimSpace(req.RequestID),
	})
	if errors.Is(err, storage.ErrConflict) {
		writeError(w, http.StatusConflict, "duplicate request id")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create task comment failed")
		return
	}
	if err := s.daemon.RecordMessageMutation(r.Context(), message, daemonv1.EventOperation_EVENT_OPERATION_APPENDED); err != nil {
		writeError(w, http.StatusInternalServerError, "append message event failed")
		return
	}
	writeJSON(w, http.StatusCreated, message)
}

func (s *Server) handleTaskTimeline(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon bridge is disabled")
		return
	}
	taskID := strings.TrimSpace(r.PathValue("id"))
	if _, err := s.store.GetTask(r.Context(), taskID); errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "get task failed")
		return
	}
	resp, err := s.daemon.ListEventsSince(r.Context(), &daemonv1.ListEventsSinceRequest{
		Cursor: &daemonv1.EventCursor{
			AggregateId:     taskID,
			Sequence:        int64Query(r, "sequence", 0),
			ProtocolVersion: s.daemon.ProtocolVersion(),
			ServerId:        s.daemon.ServerID(),
		},
		Limit: uint32(intQuery(r, "limit", 100)),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list task timeline failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      resp.GetEvents(),
		"nextCursor": resp.GetNextCursor(),
	})
}

func (s *Server) handleListReminders(w http.ResponseWriter, r *http.Request) {
	statuses := r.URL.Query()["status"]
	reminders, err := s.store.ListReminders(r.Context(), strings.TrimSpace(r.URL.Query().Get("target")),
		statuses, boolQuery(r, "includeCanceled"), intQuery(r, "limit", 100))
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "invalid reminder status")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list reminders failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": reminders})
}

func (s *Server) handleCreateReminder(w http.ResponseWriter, r *http.Request) {
	var req reminderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}
	plan, err := reminderSchedulePlanFromInput(req.DelaySeconds, req.FireAt, req.ScheduleKind, req.Schedule, req.Timezone)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	prompt := strings.TrimSpace(req.Prompt)
	if title == "" {
		title = prompt
	}
	if title == "" {
		writeError(w, http.StatusBadRequest, "title or prompt is required")
		return
	}
	principal := principalFromContext(r.Context())
	created, err := s.store.CreateReminder(r.Context(), storage.Reminder{
		Target:                target,
		ScheduleKind:          plan.Kind,
		Schedule:              plan.Schedule,
		Prompt:                prompt,
		Enabled:               true,
		NextRunUnix:           plan.NextRunUnix,
		Title:                 title,
		Status:                "active",
		MsgRef:                strings.TrimSpace(req.MsgRef),
		RecurrenceRule:        plan.RecurrenceRule,
		RecurrenceDescription: plan.RecurrenceDescription,
		RecurrenceTimezone:    plan.RecurrenceTimezone,
		CancelToken:           storage.NewID("rtk"),
	}, "human", principal.User.ID, "created")
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "invalid reminder schedule")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create reminder failed")
		return
	}
	if err := s.daemon.RecordReminderMutation(r.Context(), created, daemonv1.EventOperation_EVENT_OPERATION_CREATED); err != nil {
		writeError(w, http.StatusInternalServerError, "append reminder event failed")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleCancelReminder(w http.ResponseWriter, r *http.Request) {
	var req cancelReminderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	reminderID := strings.TrimSpace(r.PathValue("id"))
	current, err := s.store.GetReminder(r.Context(), reminderID)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "reminder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get reminder failed")
		return
	}
	if current.CancelToken != "" && strings.TrimSpace(req.CancelToken) != "" && current.CancelToken != strings.TrimSpace(req.CancelToken) {
		writeError(w, http.StatusForbidden, "invalid cancel token")
		return
	}
	principal := principalFromContext(r.Context())
	updated, err := s.store.CancelReminder(r.Context(), reminderID, "human", principal.User.ID, "canceled")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cancel reminder failed")
		return
	}
	if err := s.daemon.RecordReminderMutation(r.Context(), updated, daemonv1.EventOperation_EVENT_OPERATION_CANCELED); err != nil {
		writeError(w, http.StatusInternalServerError, "append reminder event failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleSnoozeReminder(w http.ResponseWriter, r *http.Request) {
	var req snoozeReminderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DelaySeconds == 0 {
		writeError(w, http.StatusBadRequest, "delaySeconds is required")
		return
	}
	nextRun := time.Now().Add(time.Duration(req.DelaySeconds) * time.Second).Unix()
	schedule := "in " + strconv.FormatUint(uint64(req.DelaySeconds), 10) + "s"
	principal := principalFromContext(r.Context())
	updated, err := s.store.SnoozeReminder(r.Context(), strings.TrimSpace(r.PathValue("id")),
		nextRun, schedule, "human", principal.User.ID, "snoozed")
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "reminder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "snooze reminder failed")
		return
	}
	if err := s.daemon.RecordReminderMutation(r.Context(), updated, daemonv1.EventOperation_EVENT_OPERATION_UPDATED); err != nil {
		writeError(w, http.StatusInternalServerError, "append reminder event failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleUpdateReminder(w http.ResponseWriter, r *http.Request) {
	var req reminderPatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	patch := storage.ReminderPatch{Title: optionalTrimmed(req.Title)}
	if req.DelaySeconds != nil || req.FireAt != nil || req.Schedule != nil || req.ScheduleKind != nil {
		var delay uint32
		if req.DelaySeconds != nil {
			delay = *req.DelaySeconds
		}
		plan, err := reminderSchedulePlanFromInput(delay, optionalStringValue(req.FireAt),
			optionalStringValue(req.ScheduleKind), optionalStringValue(req.Schedule), optionalStringValue(req.Timezone))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		patch.ScheduleKind = &plan.Kind
		patch.Schedule = &plan.Schedule
		patch.NextRunUnix = &plan.NextRunUnix
		patch.RecurrenceRule = &plan.RecurrenceRule
		patch.RecurrenceDescription = &plan.RecurrenceDescription
		patch.RecurrenceTimezone = &plan.RecurrenceTimezone
	} else if req.Timezone != nil {
		timezone := strings.TrimSpace(*req.Timezone)
		patch.RecurrenceTimezone = &timezone
	}
	principal := principalFromContext(r.Context())
	updated, err := s.store.UpdateReminder(r.Context(), strings.TrimSpace(r.PathValue("id")),
		patch, "human", principal.User.ID, "updated")
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "reminder not found")
		return
	}
	if errors.Is(err, storage.ErrInvalidState) {
		writeError(w, http.StatusBadRequest, "invalid reminder update")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update reminder failed")
		return
	}
	if err := s.daemon.RecordReminderMutation(r.Context(), updated, daemonv1.EventOperation_EVENT_OPERATION_UPDATED); err != nil {
		writeError(w, http.StatusInternalServerError, "append reminder event failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleReminderLog(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.ListReminderEvents(r.Context(), strings.TrimSpace(r.PathValue("id")), intQuery(r, "limit", 100))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "reminder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list reminder log failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events})
}

func (s *Server) handleListRuntimePresets(w http.ResponseWriter, r *http.Request) {
	includeExperimental := boolQuery(r, "includeExperimental")
	presets := runtimecatalog.List(includeExperimental, strings.TrimSpace(r.URL.Query().Get("kindPrefix")), uint32(intQuery(r, "limit", 200)))
	writeJSON(w, http.StatusOK, map[string]any{"items": runtimePresetResponses(presets)})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "bearer token is required")
			return
		}
		user, session, err := s.auth.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		ctx := context.WithValue(r.Context(), principalKey, Principal{User: user, Session: session})
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) requireAuthOrQueryToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			token = strings.TrimSpace(r.URL.Query().Get("access_token"))
		}
		if token == "" {
			token = strings.TrimSpace(r.URL.Query().Get("token"))
		}
		if token == "" {
			writeError(w, http.StatusUnauthorized, "bearer token is required")
			return
		}
		user, session, err := s.auth.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		ctx := context.WithValue(r.Context(), principalKey, Principal{User: user, Session: session})
		next(w, r.WithContext(ctx))
	}
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

func principalFromContext(ctx context.Context) Principal {
	principal, _ := ctx.Value(principalKey).(Principal)
	return principal
}

func (s *Server) loadMessageAttachments(target string, attachmentIDs []string) ([]storage.Attachment, error) {
	attachments := make([]storage.Attachment, 0, len(attachmentIDs))
	seen := make(map[string]struct{}, len(attachmentIDs))
	for _, rawID := range attachmentIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		attachment, err := immedia.ReadMetadata(s.cfg.DataDir, id)
		if err != nil {
			return nil, err
		}
		if attachment.Target != "" && attachment.Target != target {
			return nil, errors.New("attachment target mismatch")
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}

func (s *Server) readAttachmentForRequest(w http.ResponseWriter, r *http.Request) (storage.Attachment, bool) {
	attachment, err := immedia.ReadMetadata(s.cfg.DataDir, r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "attachment not found")
		return storage.Attachment{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read attachment failed")
		return storage.Attachment{}, false
	}
	return attachment, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeStorageError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, fallback)
	case errors.Is(err, storage.ErrConflict):
		writeError(w, http.StatusConflict, fallback)
	case errors.Is(err, storage.ErrInvalidState):
		writeError(w, http.StatusBadRequest, fallback)
	default:
		writeError(w, http.StatusInternalServerError, fallback)
	}
}

func intQuery(r *http.Request, name string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func boolQuery(r *http.Request, name string) bool {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	value, err := strconv.ParseBool(raw)
	return err == nil && value
}

func optionalBoolQuery(r *http.Request, name string) *bool {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil
	}
	return &value
}

func normalizedJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return "{}"
	}
	return value
}

func optionalTrimmed(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

type reminderSchedulePlan struct {
	Kind                  string
	Schedule              string
	NextRunUnix           int64
	RecurrenceRule        string
	RecurrenceDescription string
	RecurrenceTimezone    string
}

func reminderSchedulePlanFromInput(delaySeconds uint32, fireAt, scheduleKind, schedule, timezone string) (reminderSchedulePlan, error) {
	if delaySeconds > 0 {
		return reminderSchedulePlan{
			Kind:               "at",
			Schedule:           "in " + strconv.FormatUint(uint64(delaySeconds), 10) + "s",
			NextRunUnix:        time.Now().Add(time.Duration(delaySeconds) * time.Second).Unix(),
			RecurrenceTimezone: strings.TrimSpace(timezone),
		}, nil
	}
	if strings.TrimSpace(fireAt) != "" {
		nextRun, err := parseReminderFireAt(fireAt)
		if err != nil {
			return reminderSchedulePlan{}, err
		}
		return reminderSchedulePlan{
			Kind:               "at",
			Schedule:           strings.TrimSpace(fireAt),
			NextRunUnix:        nextRun,
			RecurrenceTimezone: strings.TrimSpace(timezone),
		}, nil
	}
	schedule = strings.TrimSpace(schedule)
	if schedule == "" {
		return reminderSchedulePlan{}, errors.New("schedule is required")
	}
	kind := normalizeReminderScheduleKindForHTTP(scheduleKind)
	if kind == "" {
		kind = "natural"
	}
	if !validReminderScheduleKindForHTTP(kind) {
		return reminderSchedulePlan{}, errors.New("invalid reminder schedule kind")
	}
	plan := reminderSchedulePlan{
		Kind:                  kind,
		Schedule:              schedule,
		RecurrenceDescription: schedule,
		RecurrenceTimezone:    strings.TrimSpace(timezone),
	}
	if kind == "at" {
		nextRun, err := parseReminderFireAt(schedule)
		if err != nil {
			return reminderSchedulePlan{}, err
		}
		plan.NextRunUnix = nextRun
	}
	if kind == "rrule" {
		plan.RecurrenceRule = schedule
	}
	return plan, nil
}

func parseReminderFireAt(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("fireAt is required")
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil && unix > 0 {
		return unix, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return parsed.Unix(), nil
		}
	}
	return 0, errors.New("fireAt must be unix seconds or RFC3339 time")
}

func normalizeReminderScheduleKindForHTTP(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "reminder_schedule_kind_cron":
		return "cron"
	case "reminder_schedule_kind_every":
		return "every"
	case "reminder_schedule_kind_at":
		return "at"
	case "reminder_schedule_kind_rrule":
		return "rrule"
	case "reminder_schedule_kind_natural":
		return "natural"
	default:
		return value
	}
}

func validReminderScheduleKindForHTTP(value string) bool {
	switch normalizeReminderScheduleKindForHTTP(value) {
	case "cron", "every", "at", "rrule", "natural":
		return true
	default:
		return false
	}
}

func validateCredentials(username, password string) error {
	if strings.TrimSpace(username) == "" {
		return errors.New("username is required")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

type authRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type endpointRequest struct {
	Kind            string `json:"kind"`
	Provider        string `json:"provider"`
	DisplayName     string `json:"displayName"`
	TargetPrefix    string `json:"targetPrefix"`
	InboundEnabled  bool   `json:"inboundEnabled"`
	OutboundEnabled bool   `json:"outboundEnabled"`
	AuthMode        string `json:"authMode"`
	ConfigJSON      string `json:"configJson"`
}

type bindingSessionRequest struct {
	Method string `json:"method"`
}

type endpointPatchRequest struct {
	DisplayName     *string `json:"displayName"`
	TargetPrefix    *string `json:"targetPrefix"`
	InboundEnabled  *bool   `json:"inboundEnabled"`
	OutboundEnabled *bool   `json:"outboundEnabled"`
	AuthMode        *string `json:"authMode"`
	ConfigJSON      *string `json:"configJson"`
}

type channelRequest struct {
	Target      string `json:"target"`
	DisplayName string `json:"displayName"`
	Visibility  string `json:"visibility"`
}

type channelPatchRequest struct {
	DisplayName *string `json:"displayName"`
	Visibility  *string `json:"visibility"`
}

type channelMemberRequest struct {
	MemberID    string `json:"memberId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	Role        string `json:"role"`
}

type channelMemberPatchRequest struct {
	Username    *string `json:"username"`
	DisplayName *string `json:"displayName"`
	Role        *string `json:"role"`
}

type notificationRouteRequest struct {
	Target     string `json:"target"`
	ThreadID   string `json:"threadId"`
	EndpointID string `json:"endpointId"`
	EventKind  string `json:"eventKind"`
	Preference string `json:"preference"`
	Enabled    *bool  `json:"enabled"`
	ConfigJSON string `json:"configJson"`
}

type notificationRoutePatchRequest struct {
	Target     *string `json:"target"`
	ThreadID   *string `json:"threadId"`
	EndpointID *string `json:"endpointId"`
	EventKind  *string `json:"eventKind"`
	Preference *string `json:"preference"`
	Enabled    *bool   `json:"enabled"`
	ConfigJSON *string `json:"configJson"`
}

type messageRequest struct {
	Target            string   `json:"target"`
	ThreadID          string   `json:"threadId"`
	Role              string   `json:"role"`
	Content           string   `json:"content"`
	ReplyToMessageID  string   `json:"replyToMessageId"`
	AttachmentIDs     []string `json:"attachmentIds"`
	SourceEndpointID  string   `json:"sourceEndpointId"`
	ExternalMessageID string   `json:"externalMessageId"`
	MetadataJSON      string   `json:"metadataJson"`
	RequestID         string   `json:"requestId"`
}

type threadReadRequest struct {
	Target string `json:"target"`
}

type threadInboxReadRequest struct {
	TargetPrefix string `json:"targetPrefix"`
}

type taskRequest struct {
	Summary       string `json:"summary"`
	Description   string `json:"description"`
	State         string `json:"state"`
	Target        string `json:"target"`
	AssigneeID    string `json:"assigneeId"`
	BlockedReason string `json:"blockedReason"`
}

type taskPatchRequest struct {
	Summary       *string `json:"summary"`
	Description   *string `json:"description"`
	State         *string `json:"state"`
	AssigneeID    *string `json:"assigneeId"`
	BlockedReason *string `json:"blockedReason"`
}

type taskCommentRequest struct {
	Content   string `json:"content"`
	RequestID string `json:"requestId"`
}

type reminderRequest struct {
	Target       string `json:"target"`
	Title        string `json:"title"`
	Prompt       string `json:"prompt"`
	DelaySeconds uint32 `json:"delaySeconds"`
	FireAt       string `json:"fireAt"`
	ScheduleKind string `json:"scheduleKind"`
	Schedule     string `json:"schedule"`
	Timezone     string `json:"timezone"`
	MsgRef       string `json:"msgRef"`
}

type reminderPatchRequest struct {
	Title        *string `json:"title"`
	DelaySeconds *uint32 `json:"delaySeconds"`
	FireAt       *string `json:"fireAt"`
	ScheduleKind *string `json:"scheduleKind"`
	Schedule     *string `json:"schedule"`
	Timezone     *string `json:"timezone"`
}

type snoozeReminderRequest struct {
	DelaySeconds uint32 `json:"delaySeconds"`
}

type cancelReminderRequest struct {
	CancelToken string `json:"cancelToken"`
}

type runtimePresetResponse struct {
	Kind             string   `json:"kind"`
	DisplayName      string   `json:"displayName"`
	Provider         string   `json:"provider"`
	DefaultModel     string   `json:"defaultModel,omitempty"`
	Command          string   `json:"command,omitempty"`
	Aliases          []string `json:"aliases"`
	DefaultArgs      []string `json:"defaultArgs"`
	EnvVarNames      []string `json:"envVarNames"`
	InstallHint      []string `json:"installHint"`
	Capabilities     []string `json:"capabilities"`
	SlockSupported   bool     `json:"slockSupported"`
	MulticaSupported bool     `json:"multicaSupported"`
	Recommended      bool     `json:"recommended"`
	Description      string   `json:"description,omitempty"`
}

func runtimePresetResponses(presets []*daemonv1.RuntimePreset) []runtimePresetResponse {
	out := make([]runtimePresetResponse, 0, len(presets))
	for _, preset := range presets {
		capabilities := make([]string, 0, len(preset.GetCapabilities()))
		for _, capability := range preset.GetCapabilities() {
			if capability.GetName() != "" {
				capabilities = append(capabilities, capability.GetName())
			}
		}
		out = append(out, runtimePresetResponse{
			Kind:             preset.GetKind(),
			DisplayName:      preset.GetDisplayName(),
			Provider:         preset.GetProvider(),
			DefaultModel:     preset.GetDefaultModel(),
			Command:          preset.GetCommand(),
			Aliases:          preset.GetAliases(),
			DefaultArgs:      preset.GetDefaultArgs(),
			EnvVarNames:      preset.GetEnvVarNames(),
			InstallHint:      preset.GetInstallHint(),
			Capabilities:     capabilities,
			SlockSupported:   preset.GetSlockSupported(),
			MulticaSupported: preset.GetMulticaSupported(),
			Recommended:      preset.GetRecommended(),
			Description:      preset.GetDescription(),
		})
	}
	return out
}

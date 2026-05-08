package server

import (
	"context"
	"encoding/json"
	"errors"
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
	cfg    config.Config
	logger *slog.Logger
	mux    *http.ServeMux
	store  *storage.Store
	cache  cache.Cache
	auth   *auth.Service
	daemon *daemonrpc.Server
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
		cfg:    cfg,
		logger: logger,
		mux:    http.NewServeMux(),
		store:  store,
		cache:  cacheStore,
	}
	if store != nil {
		s.auth = auth.New(store)
		serverID, err := cfg.ServerID()
		if err != nil {
			logger.Warn("failed to load server id; using ephemeral id", "error", err)
		}
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
	grpcServer := grpc.NewServer()
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
	s.mux.HandleFunc("GET /api/interaction-endpoints", s.requireAuth(s.handleListInteractionEndpoints))
	s.mux.HandleFunc("POST /api/interaction-endpoints", s.requireAuth(s.handleCreateInteractionEndpoint))
	s.mux.HandleFunc("POST /api/attachments", s.requireAuth(s.handleUploadAttachment))
	s.mux.HandleFunc("GET /api/attachments/{id}", s.requireAuth(s.handleGetAttachment))
	s.mux.HandleFunc("GET /api/attachments/{id}/content", s.requireAuth(s.handleDownloadAttachment))
	s.mux.HandleFunc("GET /api/messages", s.requireAuth(s.handleListMessages))
	s.mux.HandleFunc("POST /api/messages", s.requireAuth(s.handleCreateMessage))
	s.mux.HandleFunc("GET /api/inbox/threads", s.requireAuth(s.handleListThreadInbox))
	s.mux.HandleFunc("POST /api/inbox/threads/{threadID}/read", s.requireAuth(s.handleMarkThreadRead))
	s.mux.HandleFunc("POST /api/inbox/threads/read-all", s.requireAuth(s.handleMarkThreadInboxRead))
	s.mux.HandleFunc("GET /api/tasks", s.requireAuth(s.handleListTasks))
	s.mux.HandleFunc("POST /api/tasks", s.requireAuth(s.handleCreateTask))
	s.mux.HandleFunc("GET /api/tasks/{id}/comments", s.requireAuth(s.handleListTaskComments))
	s.mux.HandleFunc("POST /api/tasks/{id}/comments", s.requireAuth(s.handleCreateTaskComment))
	s.mux.HandleFunc("GET /api/tasks/{id}/timeline", s.requireAuth(s.handleTaskTimeline))
	s.mux.HandleFunc("PATCH /api/tasks/{id}", s.requireAuth(s.handleUpdateTask))
	s.mux.HandleFunc("GET /api/runtime-presets", s.requireAuth(s.handleListRuntimePresets))
	s.mux.HandleFunc("GET /api/daemon/info", s.requireAuth(s.handleDaemonInfo))
	s.mux.HandleFunc("GET /api/daemon/agent-statuses", s.requireAuth(s.handleDaemonAgentStatuses))
	s.mux.HandleFunc("GET /api/daemon/activity", s.requireAuth(s.handleDaemonActivity))
	s.mux.HandleFunc("GET /api/daemon/runs", s.requireAuth(s.handleDaemonRuns))
	s.mux.HandleFunc("GET /api/daemon/events", s.requireAuth(s.handleDaemonEvents))
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
	writeJSON(w, http.StatusOK, map[string]any{"items": endpoints})
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
	created, err := s.store.CreateInteractionEndpoint(r.Context(), endpoint)
	if errors.Is(err, storage.ErrConflict) {
		writeError(w, http.StatusConflict, "interaction endpoint already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create interaction endpoint failed")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	const maxAttachmentBytes = 32 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentBytes)
	if err := r.ParseMultipartForm(maxAttachmentBytes); err != nil {
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

	id := storage.NewID("att")
	filename := safeAttachmentFilename(header.Filename)
	dir := filepath.Join(s.cfg.DataDir, "attachments", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeError(w, http.StatusInternalServerError, "create attachment storage failed")
		return
	}
	relativeStorageRef := filepath.Join("attachments", id, filename)
	contentPath := filepath.Join(s.cfg.DataDir, relativeStorageRef)
	out, err := os.OpenFile(contentPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create attachment failed")
		return
	}
	size, copyErr := io.Copy(out, file)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.RemoveAll(dir)
		writeError(w, http.StatusInternalServerError, "store attachment failed")
		return
	}

	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType == "" || mimeType == "application/octet-stream" {
		detected, err := detectFileContentType(contentPath)
		if err == nil {
			mimeType = detected
		}
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	principal := principalFromContext(r.Context())
	attachment := storage.Attachment{
		ID:          id,
		Target:      target,
		OwnerID:     principal.User.ID,
		Filename:    filename,
		MimeType:    mimeType,
		SizeBytes:   size,
		StorageRef:  filepath.ToSlash(relativeStorageRef),
		DownloadURL: "/api/attachments/" + id + "/content",
		CreatedUnix: time.Now().Unix(),
	}
	if err := s.saveAttachmentMetadata(attachment); err != nil {
		_ = os.RemoveAll(dir)
		writeError(w, http.StatusInternalServerError, "save attachment metadata failed")
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
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
	contentPath := filepath.Join(s.cfg.DataDir, filepath.FromSlash(attachment.StorageRef))
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
	if !isInlineAttachment(attachment.MimeType) {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", attachment.MimeType)
	w.Header().Set("Content-Disposition", disposition+`; filename="`+strings.ReplaceAll(attachment.Filename, `"`, "")+`"`)
	http.ServeContent(w, r, attachment.Filename, info.ModTime(), file)
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
		attachment, err := s.readAttachmentMetadata(id)
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

func (s *Server) attachmentMetadataPath(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\`) || strings.HasPrefix(id, ".") {
		return "", storage.ErrNotFound
	}
	return filepath.Join(s.cfg.DataDir, "attachments", id, "metadata.json"), nil
}

func (s *Server) saveAttachmentMetadata(attachment storage.Attachment) error {
	metadataPath, err := s.attachmentMetadataPath(attachment.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(attachment, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath, data, 0o600)
}

func (s *Server) readAttachmentMetadata(id string) (storage.Attachment, error) {
	metadataPath, err := s.attachmentMetadataPath(id)
	if err != nil {
		return storage.Attachment{}, err
	}
	data, err := os.ReadFile(metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		return storage.Attachment{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.Attachment{}, err
	}
	var attachment storage.Attachment
	if err := json.Unmarshal(data, &attachment); err != nil {
		return storage.Attachment{}, err
	}
	if attachment.ID == "" {
		return storage.Attachment{}, storage.ErrNotFound
	}
	return attachment, nil
}

func (s *Server) readAttachmentForRequest(w http.ResponseWriter, r *http.Request) (storage.Attachment, bool) {
	attachment, err := s.readAttachmentMetadata(r.PathValue("id"))
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

func safeAttachmentFilename(value string) string {
	name := strings.TrimSpace(filepath.Base(value))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "attachment"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', '"', '\r', '\n', 0:
			return -1
		default:
			return r
		}
	}, name)
}

func detectFileContentType(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var head [512]byte
	n, err := file.Read(head[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return http.DetectContentType(head[:n]), nil
}

func isInlineAttachment(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	return strings.HasPrefix(mimeType, "image/") || mimeType == "text/html"
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

type messageRequest struct {
	Target            string   `json:"target"`
	ThreadID          string   `json:"threadId"`
	Role              string   `json:"role"`
	Content           string   `json:"content"`
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

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/storage"
	"github.com/ca-x/nekode/internal/tunnelregistry"
)

// Hop-by-hop headers per RFC 7230 §6.1. These must never be forwarded
// between server and daemon; each hop sets its own transport headers.
var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

const (
	// previewReadBufferBytes is the upload chunk size; chosen at 32 KiB to
	// land below the default gRPC message size while staying large enough
	// to keep small requests in one frame.
	previewReadBufferBytes = 32 * 1024
	// previewResponseTimeout caps per-request wall time across server and
	// daemon so a misbehaving upstream can't pin a goroutine forever.
	previewResponseTimeout = 10 * time.Minute
)

// handlePreviewProxy handles inbound /preview/<token>/... requests. It
// resolves the tunnel, enforces the access policy, consults the
// rate limiter, and bridges the HTTP round-trip over ProxyExchange frames.
func (s *Server) handlePreviewProxy(w http.ResponseWriter, r *http.Request) {
	if s.tunnels == nil {
		http.Error(w, "preview tunnels not enabled", http.StatusServiceUnavailable)
		return
	}
	token := strings.TrimSpace(r.PathValue("token"))
	if token == "" {
		http.Error(w, "token missing from path", http.StatusNotFound)
		return
	}
	record, err := s.store.GetTunnelByToken(r.Context(), token)
	if errors.Is(err, storage.ErrNotFound) {
		http.Error(w, "tunnel not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Warn("preview tunnel lookup failed", "error", err, "token_prefix", safeTokenPrefix(token))
		http.Error(w, "tunnel lookup failed", http.StatusInternalServerError)
		return
	}
	if record.State != storage.TunnelStateActive {
		s.auditPreviewDeny(r, record, "not_active")
		http.Error(w, "tunnel is not active", http.StatusForbidden)
		return
	}
	if record.ExpiresUnix > 0 && record.ExpiresUnix <= time.Now().Unix() {
		s.auditPreviewDeny(r, record, "expired")
		http.Error(w, "tunnel expired", http.StatusGone)
		return
	}
	if deny := s.enforceTunnelACL(r, record); deny != "" {
		s.auditPreviewDeny(r, record, deny)
		http.Error(w, deny, http.StatusForbidden)
		return
	}
	if s.tunnelRate != nil && !s.tunnelRate.Allow(record.ID) {
		s.auditPreviewDeny(r, record, "rate_limited")
		w.Header().Set("Retry-After", "1")
		http.Error(w, "preview tunnel is rate-limited, retry shortly", http.StatusTooManyRequests)
		return
	}
	s.auditPreviewAllow(r, record)
	s.proxyOverTunnel(w, r, record)
}

// enforceTunnelACL returns an empty string when the request is allowed,
// or a short machine-parseable reason string otherwise. The reason also
// doubles as the end-user error message — kept terse on purpose so the
// response body doesn't leak details about why a given identity was
// rejected (distinguishing "logged out" from "not a member" would give
// URL-guessers a signal).
func (s *Server) enforceTunnelACL(r *http.Request, record storage.TunnelRecord) string {
	switch record.AccessPolicy {
	case storage.TunnelAccessPolicyPublic:
		return ""
	case storage.TunnelAccessPolicyPrivate:
		user, ok := s.previewPrincipal(r)
		if !ok {
			return "authentication required"
		}
		if user.ID != record.CreatorID && !strings.EqualFold(user.Role, "admin") {
			return "forbidden"
		}
		return ""
	default:
		// members policy — any authenticated workspace user.
		if _, ok := s.previewPrincipal(r); !ok {
			return "authentication required"
		}
		return ""
	}
}

// previewPrincipal resolves the caller's identity from either a bearer
// token or an `access_token` query param. Returns false for anonymous
// requests so the ACL branch can decide whether to let them through.
func (s *Server) previewPrincipal(r *http.Request) (storage.User, bool) {
	if s.auth == nil {
		return storage.User{}, false
	}
	token := bearerToken(r)
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("access_token"))
	}
	if token == "" {
		if cookie, err := r.Cookie("nekode_session"); err == nil {
			token = cookie.Value
		}
	}
	if token == "" {
		return storage.User{}, false
	}
	user, _, err := s.auth.Authenticate(r.Context(), token)
	if err != nil {
		return storage.User{}, false
	}
	return user, true
}

// auditPreviewAllow and auditPreviewDeny emit structured log lines for
// every inbound preview request. The token is never logged verbatim —
// only the safeTokenPrefix helper's first-6-chars fingerprint — so
// operators can correlate events without the log becoming a lookup
// table for active secrets.
func (s *Server) auditPreviewAllow(r *http.Request, record storage.TunnelRecord) {
	s.logger.Info("preview.allow",
		"tunnel_id", record.ID,
		"computer_id", record.ComputerID,
		"policy", record.AccessPolicy,
		"method", r.Method,
		"path", r.URL.Path,
		"remote", r.RemoteAddr,
		"token_prefix", safeTokenPrefix(record.Token),
	)
}

func (s *Server) auditPreviewDeny(r *http.Request, record storage.TunnelRecord, reason string) {
	s.logger.Warn("preview.deny",
		"tunnel_id", record.ID,
		"computer_id", record.ComputerID,
		"policy", record.AccessPolicy,
		"reason", reason,
		"method", r.Method,
		"path", r.URL.Path,
		"remote", r.RemoteAddr,
		"token_prefix", safeTokenPrefix(record.Token),
	)
}

// proxyOverTunnel does the bidi bridging: register a responder, stream
// request bytes to the daemon, relay response bytes back out. Any error
// short-circuits with a 502. The method assumes record.State == active.
func (s *Server) proxyOverTunnel(w http.ResponseWriter, r *http.Request, record storage.TunnelRecord) {
	responder := tunnelregistry.NewResponder()
	requestID, stream, err := s.tunnels.Register(record.ComputerID, responder)
	if errors.Is(err, tunnelregistry.ErrNoDaemon) {
		http.Error(w, "daemon not connected", http.StatusBadGateway)
		return
	}
	if err != nil {
		http.Error(w, "tunnel dispatch failed", http.StatusInternalServerError)
		return
	}
	defer s.tunnels.Unregister(record.ComputerID, requestID)
	defer responder.Close()

	// WebSocket upgrades keep the stream open indefinitely; everything
	// else is capped at previewResponseTimeout so a misbehaving upstream
	// can't pin a goroutine forever.
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if isWebSocketUpgradeHTTP(r.Header) {
		ctx, cancel = context.WithCancel(r.Context())
	} else {
		ctx, cancel = context.WithTimeout(r.Context(), previewResponseTimeout)
	}
	defer cancel()

	// Build REQUEST_START and strip path prefix so the daemon gets an
	// origin-relative path like "/index.html" (not "/preview/<token>/...").
	requestPath := strings.TrimPrefix(r.URL.RequestURI(), "/preview/"+record.Token)
	if requestPath == "" {
		requestPath = "/"
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	start := &daemonv1.ProxyFrame{
		TunnelId:       record.ID,
		RequestId:      requestID,
		Kind:           daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_REQUEST_START,
		Method:         r.Method,
		Path:           requestPath,
		RequestHeaders: headersToProto(r.Header, record.LocalPort),
		RemoteAddr:     r.RemoteAddr,
	}
	if err := stream.Send(start); err != nil {
		http.Error(w, fmt.Sprintf("tunnel stream send failed: %v", err), http.StatusBadGateway)
		return
	}

	// Daemon → downstream response pipe. WebSocket upgrades need a
	// hijacked conn so we can relay raw frames in BOTH directions after
	// the 101; regular HTTP runs a concurrent request-body pump plus the
	// standard response-writer path.
	if isWebSocketUpgradeHTTP(r.Header) {
		// For WS we send a REQUEST_END immediately — the daemon's raw-TCP
		// WebSocket path doesn't expect request body frames before the
		// upgrade; post-upgrade frames come from the hijacked conn
		// handled inside pumpWebSocket.
		if err := stream.Send(&daemonv1.ProxyFrame{
			TunnelId:  record.ID,
			RequestId: requestID,
			Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_REQUEST_END,
		}); err != nil {
			s.logger.Warn("preview proxy ws REQUEST_END send failed", "error", err, "tunnel_id", record.ID)
			return
		}
		if err := pumpWebSocket(ctx, w, responder, stream, record.ID, requestID); err != nil {
			s.logger.Warn("preview proxy websocket pump failed", "error", err, "tunnel_id", record.ID)
		}
		return
	}

	// Upstream → daemon body pipe. Runs concurrently so the daemon can
	// start responding before the body finishes streaming (important for
	// long uploads and chunked requests).
	sendErrCh := make(chan error, 1)
	go func() { sendErrCh <- streamRequestBody(ctx, stream, record.ID, requestID, r.Body) }()

	if err := pumpResponse(ctx, w, responder); err != nil {
		s.logger.Warn("preview proxy response pump failed", "error", err, "tunnel_id", record.ID)
	}
	if err := <-sendErrCh; err != nil && !errors.Is(err, io.EOF) {
		s.logger.Warn("preview proxy request body send failed", "error", err, "tunnel_id", record.ID)
	}
}

// streamRequestBody reads the inbound HTTP body in fixed-size chunks and
// forwards each as a REQUEST_BODY frame. Terminates with REQUEST_END when
// the reader returns EOF.
func streamRequestBody(ctx context.Context, stream tunnelregistry.Stream, tunnelID, requestID string, body io.Reader) error {
	defer func() {
		_ = stream.Send(&daemonv1.ProxyFrame{
			TunnelId:  tunnelID,
			RequestId: requestID,
			Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_REQUEST_END,
		})
	}()
	buf := make([]byte, previewReadBufferBytes)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := body.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if sendErr := stream.Send(&daemonv1.ProxyFrame{
				TunnelId:  tunnelID,
				RequestId: requestID,
				Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_REQUEST_BODY,
				BodyChunk: chunk,
			}); sendErr != nil {
				return sendErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// pumpResponse blocks on responder.Next until it sees RESPONSE_START,
// writes headers, then streams body chunks to w.
func pumpResponse(ctx context.Context, w http.ResponseWriter, responder *tunnelregistry.Responder) error {
	headersWritten := false
	flusher, _ := w.(http.Flusher)
	for {
		frame, err := responder.Next(ctx)
		if err != nil {
			return err
		}
		if frame == nil {
			if !headersWritten {
				http.Error(w, "daemon disconnected before responding", http.StatusBadGateway)
			}
			return nil
		}
		switch frame.GetKind() {
		case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_START:
			if headersWritten {
				continue
			}
			applyResponseHeaders(w.Header(), frame.GetResponseHeaders())
			status := int(frame.GetStatusCode())
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			headersWritten = true
			if flusher != nil {
				flusher.Flush()
			}
		case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_BODY:
			if !headersWritten {
				w.WriteHeader(http.StatusOK)
				headersWritten = true
			}
			if _, writeErr := w.Write(frame.GetBodyChunk()); writeErr != nil {
				return writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
		case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_END:
			if !headersWritten {
				if errText := frame.GetError(); errText != "" {
					http.Error(w, "upstream error: "+errText, http.StatusBadGateway)
				} else {
					w.WriteHeader(http.StatusNoContent)
				}
			}
			return nil
		case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_CANCEL:
			if !headersWritten {
				http.Error(w, "daemon cancelled the request", http.StatusBadGateway)
			}
			return nil
		default:
			// Ignore unexpected frame kinds; keeps the pump resilient to
			// future proto additions without panicking old deployments.
		}
	}
}

// pumpWebSocket handles the 101-Switching-Protocols response path. It
// hijacks the underlying TCP conn, writes the upstream-supplied status
// line + headers, then fans out two pumps: daemon RESPONSE_BODY frames
// → browser conn, and browser conn bytes → daemon REQUEST_BODY frames.
func pumpWebSocket(ctx context.Context, w http.ResponseWriter, responder *tunnelregistry.Responder, stream tunnelregistry.Stream, tunnelID, requestID string) error {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket upgrade not supported on this server", http.StatusInternalServerError)
		return errors.New("response writer is not a Hijacker")
	}
	// Wait for the daemon's RESPONSE_START before hijacking so we don't
	// leave a half-open browser connection on upstream errors.
	var startFrame *daemonv1.ProxyFrame
	for startFrame == nil {
		frame, err := responder.Next(ctx)
		if err != nil {
			return err
		}
		if frame == nil {
			http.Error(w, "daemon disconnected before responding", http.StatusBadGateway)
			return nil
		}
		if frame.GetKind() == daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_START {
			startFrame = frame
		}
	}
	if startFrame.GetStatusCode() != http.StatusSwitchingProtocols {
		// Upstream refused the upgrade. Write the error response through
		// the normal ResponseWriter so the browser sees it correctly.
		applyResponseHeaders(w.Header(), startFrame.GetResponseHeaders())
		w.WriteHeader(int(startFrame.GetStatusCode()))
		return drainAsBody(ctx, w, responder)
	}
	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "hijack failed", http.StatusInternalServerError)
		return err
	}
	defer conn.Close()
	// Write status line + headers manually; net/http's built-in writer
	// doesn't support Write() after WriteHeader(101).
	if _, err := fmt.Fprintf(bufrw, "HTTP/1.1 %d Switching Protocols\r\n", startFrame.GetStatusCode()); err != nil {
		return err
	}
	for _, h := range startFrame.GetResponseHeaders() {
		if _, isHop := hopByHopHeaders[http.CanonicalHeaderKey(h.GetName())]; isHop && !strings.EqualFold(h.GetName(), "Upgrade") && !strings.EqualFold(h.GetName(), "Connection") {
			continue
		}
		for _, v := range h.GetValue() {
			if _, err := fmt.Fprintf(bufrw, "%s: %s\r\n", h.GetName(), v); err != nil {
				return err
			}
		}
	}
	if _, err := bufrw.WriteString("\r\n"); err != nil {
		return err
	}
	if err := bufrw.Flush(); err != nil {
		return err
	}

	// Browser → daemon pump: read raw bytes from the hijacked conn and
	// emit REQUEST_BODY frames. Runs concurrently with the server→browser
	// pump below so the socket is truly bidirectional.
	clientToDaemon := make(chan error, 1)
	go func() {
		buf := make([]byte, previewReadBufferBytes)
		for {
			n, readErr := bufrw.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				if sendErr := stream.Send(&daemonv1.ProxyFrame{
					TunnelId:  tunnelID,
					RequestId: requestID,
					Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_REQUEST_BODY,
					BodyChunk: chunk,
				}); sendErr != nil {
					clientToDaemon <- sendErr
					return
				}
			}
			if errors.Is(readErr, io.EOF) {
				clientToDaemon <- nil
				return
			}
			if readErr != nil {
				clientToDaemon <- readErr
				return
			}
		}
	}()

	// Daemon → browser pump: RESPONSE_BODY frames to raw bytes until
	// RESPONSE_END or the daemon disconnects.
	pumpErr := func() error {
		for {
			frame, err := responder.Next(ctx)
			if err != nil {
				return err
			}
			if frame == nil {
				return nil
			}
			switch frame.GetKind() {
			case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_BODY:
				if _, err := conn.Write(frame.GetBodyChunk()); err != nil {
					return err
				}
			case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_END,
				daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_CANCEL:
				return nil
			}
		}
	}()
	// Signal the daemon that the client side is done so its WS pump
	// can tear down the upstream socket cleanly.
	_ = stream.Send(&daemonv1.ProxyFrame{
		TunnelId:  tunnelID,
		RequestId: requestID,
		Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_REQUEST_END,
	})
	// Drain the client→daemon goroutine; closing conn (via defer) will
	// unblock its Read with an error.
	<-clientToDaemon
	return pumpErr
}

// drainAsBody consumes remaining BODY/END frames into the current
// ResponseWriter. Used when upstream refused a ws upgrade and replied
// with a normal error response we want to relay transparently.
func drainAsBody(ctx context.Context, w http.ResponseWriter, responder *tunnelregistry.Responder) error {
	flusher, _ := w.(http.Flusher)
	for {
		frame, err := responder.Next(ctx)
		if err != nil {
			return err
		}
		if frame == nil {
			return nil
		}
		switch frame.GetKind() {
		case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_BODY:
			if _, writeErr := w.Write(frame.GetBodyChunk()); writeErr != nil {
				return writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
		case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_END,
			daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_CANCEL:
			return nil
		}
	}
}

func headersToProto(header http.Header, targetPort uint32) []*daemonv1.ProxyHeader {
	out := make([]*daemonv1.ProxyHeader, 0, len(header)+1)
	for name, values := range header {
		canonical := http.CanonicalHeaderKey(name)
		if _, isHop := hopByHopHeaders[canonical]; isHop {
			// Connection and Upgrade are technically hop-by-hop per
			// RFC 7230, but the daemon needs to see them to detect
			// WebSocket / h2c upgrades. Forward them; everything else
			// hop-by-hop (Keep-Alive, TE, Proxy-*, etc.) is dropped.
			if canonical != "Connection" && canonical != "Upgrade" {
				continue
			}
		}
		copied := make([]string, len(values))
		copy(copied, values)
		out = append(out, &daemonv1.ProxyHeader{Name: name, Value: copied})
	}
	// Synthetic header the daemon reads to dial the correct local port.
	// Not part of the real request headers; stripped client-side before
	// the outbound HTTP request is issued.
	out = append(out, &daemonv1.ProxyHeader{
		Name:  "X-Nekode-Tunnel-Port",
		Value: []string{strconv.FormatUint(uint64(targetPort), 10)},
	})
	return out
}

func applyResponseHeaders(dst http.Header, headers []*daemonv1.ProxyHeader) {
	for _, h := range headers {
		if _, isHop := hopByHopHeaders[http.CanonicalHeaderKey(h.GetName())]; isHop {
			continue
		}
		for _, v := range h.GetValue() {
			dst.Add(h.GetName(), v)
		}
	}
}

// safeTokenPrefix returns the first 6 chars of a token for log lines so
// operators can correlate events without leaking the full secret.
func safeTokenPrefix(token string) string {
	if len(token) <= 6 {
		return "***"
	}
	return token[:6] + "…"
}

// isWebSocketUpgradeHTTP mirrors the daemon-side check: WebSocket requests
// carry both Connection: upgrade and Upgrade: websocket per RFC 6455.
func isWebSocketUpgradeHTTP(header http.Header) bool {
	if !strings.EqualFold(header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, v := range header.Values("Connection") {
		if strings.Contains(strings.ToLower(v), "upgrade") {
			return true
		}
	}
	return false
}

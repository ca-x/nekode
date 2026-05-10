package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
)

// tunnelProxyDialTimeout caps how long we wait to reach localhost:<port>.
// Short on purpose — devs running preview tunnels will have the dev server
// already up before announcing it; a long dial timeout just hides errors.
const tunnelProxyDialTimeout = 10 * time.Second

// tunnelProxyBodyChunk matches the server-side REQUEST_BODY chunk size so
// upstream bodies round-trip without reassembly overhead.
const tunnelProxyBodyChunk = 32 * 1024

// tunnelProxyClient maintains the long-lived ProxyExchange bidi stream.
// One instance per daemon; reconnect on stream error with exponential
// backoff. The client does not know or care which tunnels are active —
// the server sends frames with tunnel_id embedded; the client only needs
// local_port per request, which the server supplies via Path routing.
//
// Design note: the server sends REQUEST_START with Path already rewritten
// to an origin-relative path. We need the local port for each tunnel,
// which the server looks up from the token → tunnel row before dialing.
// But we can't query that server-side; the daemon has to be told.
//
// Simpler approach: include the target port inside REQUEST_START. The
// proto has no dedicated field, so we reuse a Host header. The server
// injects `X-Nekode-Tunnel-Port: <port>` as a synthetic header before
// sending. The daemon reads it, strips it, and dials 127.0.0.1:<port>.
const tunnelTargetPortHeader = "X-Nekode-Tunnel-Port"

// proxySender is the Send-only view of a gRPC client stream. The
// per-request workers (handleUpstream / handleWebSocket / pumpReader /
// sendEndErr) only ever write, never read; taking the narrower type
// means we can hand them a mutex-guarded wrapper without exposing the
// full stream surface and risking a racy Recv from the worker side.
type proxySender interface {
	Send(*daemonv1.ProxyFrame) error
}

// lockedSender serializes writes on a single client stream. grpc-go
// allows exactly one concurrent sender, so every handleUpstream /
// handleWebSocket goroutine has to go through this before calling
// stream.Send.
type lockedSender struct {
	mu     sync.Mutex
	stream daemonv1.DaemonControlService_ProxyExchangeClient
}

func (ls *lockedSender) Send(frame *daemonv1.ProxyFrame) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.stream.Send(frame)
}

type tunnelProxyClient struct {
	cfg        daemonConfig
	client     daemonv1.DaemonControlServiceClient
	withToken  func(context.Context) context.Context
	httpClient *http.Client

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // requestID → upstream cancel
}

func newTunnelProxyClient(cfg daemonConfig, client daemonv1.DaemonControlServiceClient, withToken func(context.Context) context.Context) *tunnelProxyClient {
	return &tunnelProxyClient{
		cfg:       cfg,
		client:    client,
		withToken: withToken,
		httpClient: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 30 * time.Second,
				DisableCompression:    true, // we relay bytes as-is
			},
			// Per-request timeout comes from the server via ctx; disable the
			// client-level one so long-lived responses (SSE) stay open.
			Timeout: 0,
		},
		cancels: make(map[string]context.CancelFunc),
	}
}

// run connects and blocks, reconnecting on stream loss until ctx cancels.
func (c *tunnelProxyClient) run(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.serveOne(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("tunnel proxy stream ended; reconnecting", "error", err, "backoff", backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

// serveOne opens the stream, sends the attach frame, and dispatches
// inbound frames until the stream errors. Returns on stream close.
func (c *tunnelProxyClient) serveOne(ctx context.Context) error {
	streamCtx, cancel := context.WithCancel(c.withToken(ctx))
	defer cancel()
	stream, err := c.client.ProxyExchange(streamCtx)
	if err != nil {
		return fmt.Errorf("open proxy exchange: %w", err)
	}
	// grpc-go permits exactly one concurrent sender per client stream.
	// serveOne spawns a handleUpstream goroutine per inbound REQUEST_START,
	// each of which independently writes RESPONSE_* frames, so we wrap
	// Send in a mutex-guarded helper and pass only that to the worker
	// goroutines. Recv stays on the raw stream — it has exactly one
	// reader (this function) so no lock is needed there.
	sender := &lockedSender{stream: stream}
	if err := sender.Send(&daemonv1.ProxyFrame{
		TunnelId:  "__attach__",
		RequestId: c.cfg.ComputerID,
	}); err != nil {
		return fmt.Errorf("send attach: %w", err)
	}
	// Per-request request-body pipes. Keyed by request_id so BODY frames
	// can be appended to the right upstream io.Reader and END closes it.
	bodies := newRequestBodyMux()
	defer bodies.closeAll()
	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch frame.GetKind() {
		case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_REQUEST_START:
			body := bodies.open(frame.GetRequestId())
			go c.handleUpstream(streamCtx, sender, frame, body)
		case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_REQUEST_BODY:
			bodies.write(frame.GetRequestId(), frame.GetBodyChunk())
		case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_REQUEST_END:
			bodies.close(frame.GetRequestId())
		case daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_CANCEL:
			c.cancel(frame.GetRequestId())
			bodies.close(frame.GetRequestId())
		}
	}
}

// handleUpstream executes one HTTP round-trip against the local service
// and streams the response back as RESPONSE_* frames. WebSocket upgrade
// requests take a separate path that keeps a raw TCP connection open and
// relays frames in both directions.
func (c *tunnelProxyClient) handleUpstream(streamCtx context.Context, stream proxySender, start *daemonv1.ProxyFrame, body io.ReadCloser) {
	requestID := start.GetRequestId()
	tunnelID := start.GetTunnelId()
	defer body.Close()

	port, err := extractTargetPort(start.GetRequestHeaders())
	if err != nil {
		c.sendEndErr(stream, tunnelID, requestID, err.Error())
		return
	}
	reqCtx, reqCancel := context.WithCancel(streamCtx)
	c.trackCancel(requestID, reqCancel)
	defer c.untrackCancel(requestID)

	if isWebSocketUpgrade(start.GetRequestHeaders()) {
		c.handleWebSocket(reqCtx, stream, start, body, port)
		return
	}

	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, start.GetPath())
	req, err := http.NewRequestWithContext(reqCtx, start.GetMethod(), url, body)
	if err != nil {
		c.sendEndErr(stream, tunnelID, requestID, "build request: "+err.Error())
		return
	}
	for _, h := range start.GetRequestHeaders() {
		if http.CanonicalHeaderKey(h.GetName()) == tunnelTargetPortHeader {
			continue
		}
		for _, v := range h.GetValue() {
			req.Header.Add(h.GetName(), v)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.sendEndErr(stream, tunnelID, requestID, "upstream dial: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if err := stream.Send(&daemonv1.ProxyFrame{
		TunnelId:        tunnelID,
		RequestId:       requestID,
		Kind:            daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_START,
		StatusCode:      uint32(resp.StatusCode),
		ResponseHeaders: responseHeadersToProto(resp.Header),
	}); err != nil {
		return
	}
	buf := make([]byte, tunnelProxyBodyChunk)
	for {
		if reqCtx.Err() != nil {
			return
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if sendErr := stream.Send(&daemonv1.ProxyFrame{
				TunnelId:  tunnelID,
				RequestId: requestID,
				Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_BODY,
				BodyChunk: chunk,
			}); sendErr != nil {
				return
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			c.sendEndErr(stream, tunnelID, requestID, "upstream read: "+readErr.Error())
			return
		}
	}
	_ = stream.Send(&daemonv1.ProxyFrame{
		TunnelId:  tunnelID,
		RequestId: requestID,
		Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_END,
	})
}

func (c *tunnelProxyClient) sendEndErr(stream proxySender, tunnelID, requestID, message string) {
	_ = stream.Send(&daemonv1.ProxyFrame{
		TunnelId:  tunnelID,
		RequestId: requestID,
		Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_END,
		Error:     message,
	})
}

func (c *tunnelProxyClient) trackCancel(requestID string, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancels[requestID] = cancel
}

func (c *tunnelProxyClient) untrackCancel(requestID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cancels, requestID)
}

func (c *tunnelProxyClient) cancel(requestID string) {
	c.mu.Lock()
	cancel := c.cancels[requestID]
	delete(c.cancels, requestID)
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func responseHeadersToProto(header http.Header) []*daemonv1.ProxyHeader {
	out := make([]*daemonv1.ProxyHeader, 0, len(header))
	for name, values := range header {
		copied := make([]string, len(values))
		copy(copied, values)
		out = append(out, &daemonv1.ProxyHeader{Name: name, Value: copied})
	}
	return out
}

func extractTargetPort(headers []*daemonv1.ProxyHeader) (uint32, error) {
	for _, h := range headers {
		if http.CanonicalHeaderKey(h.GetName()) != tunnelTargetPortHeader {
			continue
		}
		for _, v := range h.GetValue() {
			port, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				return 0, fmt.Errorf("invalid %s: %w", tunnelTargetPortHeader, err)
			}
			if port == 0 || port > 65535 {
				return 0, fmt.Errorf("port %d out of range", port)
			}
			return uint32(port), nil
		}
	}
	return 0, errors.New("missing " + tunnelTargetPortHeader + " header")
}

// requestBodyMux owns one in-memory pipe per in-flight request. The
// server streams BODY frames, we append them to the pipe; the http.Client
// reads from the pipe as request body; END closes the pipe causing Read
// to return io.EOF.
type requestBodyMux struct {
	mu     sync.Mutex
	bodies map[string]*requestBody
}

func newRequestBodyMux() *requestBodyMux {
	return &requestBodyMux{bodies: make(map[string]*requestBody)}
}

func (m *requestBodyMux) open(id string) *requestBody {
	body := newRequestBody()
	m.mu.Lock()
	m.bodies[id] = body
	m.mu.Unlock()
	return body
}

func (m *requestBodyMux) write(id string, chunk []byte) {
	m.mu.Lock()
	body := m.bodies[id]
	m.mu.Unlock()
	if body != nil {
		body.write(chunk)
	}
}

func (m *requestBodyMux) close(id string) {
	m.mu.Lock()
	body := m.bodies[id]
	delete(m.bodies, id)
	m.mu.Unlock()
	if body != nil {
		body.close()
	}
}

func (m *requestBodyMux) closeAll() {
	m.mu.Lock()
	bodies := m.bodies
	m.bodies = make(map[string]*requestBody)
	m.mu.Unlock()
	for _, body := range bodies {
		body.close()
	}
}

// requestBody is a one-writer / one-reader buffer; the server pushes
// chunks via write(), and the local http.Client pulls via Read(). Any
// chunk larger than the reader drains blocks the writer — good, it's
// natural backpressure so the daemon doesn't OOM on large uploads.
type requestBody struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool
}

func newRequestBody() *requestBody {
	rb := &requestBody{}
	rb.cond = sync.NewCond(&rb.mu)
	return rb
}

func (r *requestBody) write(chunk []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.buf.Write(chunk)
	r.cond.Broadcast()
}

func (r *requestBody) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.cond.Broadcast()
}

func (r *requestBody) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.buf.Len() == 0 && !r.closed {
		r.cond.Wait()
	}
	if r.buf.Len() == 0 && r.closed {
		return 0, io.EOF
	}
	return r.buf.Read(p)
}

func (r *requestBody) Close() error {
	r.close()
	return nil
}

// isWebSocketUpgrade detects an RFC 6455 WebSocket upgrade request.
// Vite HMR and other dev servers rely on this; the generic http.Client
// can't handle it because it consumes the 101 response as a normal one.
func isWebSocketUpgrade(headers []*daemonv1.ProxyHeader) bool {
	var hasUpgrade, hasConnection bool
	for _, h := range headers {
		name := http.CanonicalHeaderKey(h.GetName())
		switch name {
		case "Upgrade":
			for _, v := range h.GetValue() {
				if strings.EqualFold(v, "websocket") {
					hasUpgrade = true
				}
			}
		case "Connection":
			for _, v := range h.GetValue() {
				if strings.Contains(strings.ToLower(v), "upgrade") {
					hasConnection = true
				}
			}
		}
	}
	return hasUpgrade && hasConnection
}

// handleWebSocket opens a raw TCP connection to localhost:<port>, writes
// the upgrade request manually, parses the 101 response, then relays
// binary frames in both directions until either side closes.
func (c *tunnelProxyClient) handleWebSocket(ctx context.Context, stream proxySender, start *daemonv1.ProxyFrame, body io.ReadCloser, port uint32) {
	tunnelID := start.GetTunnelId()
	requestID := start.GetRequestId()

	dialer := net.Dialer{Timeout: tunnelProxyDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		c.sendEndErr(stream, tunnelID, requestID, "ws dial: "+err.Error())
		return
	}
	defer conn.Close()

	// Write the upgrade request in HTTP/1.1 wire format. We skip the
	// synthetic port header and rewrite Host to the local target so the
	// dev server accepts the handshake even if upstream had a public host.
	var reqBuf bytes.Buffer
	fmt.Fprintf(&reqBuf, "%s %s HTTP/1.1\r\n", start.GetMethod(), start.GetPath())
	fmt.Fprintf(&reqBuf, "Host: 127.0.0.1:%d\r\n", port)
	for _, h := range start.GetRequestHeaders() {
		name := http.CanonicalHeaderKey(h.GetName())
		if name == tunnelTargetPortHeader || name == "Host" {
			continue
		}
		for _, v := range h.GetValue() {
			fmt.Fprintf(&reqBuf, "%s: %s\r\n", h.GetName(), v)
		}
	}
	reqBuf.WriteString("\r\n")
	if _, err := conn.Write(reqBuf.Bytes()); err != nil {
		c.sendEndErr(stream, tunnelID, requestID, "ws send handshake: "+err.Error())
		return
	}

	bufReader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(bufReader, nil)
	if err != nil {
		c.sendEndErr(stream, tunnelID, requestID, "ws read handshake: "+err.Error())
		return
	}
	if err := stream.Send(&daemonv1.ProxyFrame{
		TunnelId:        tunnelID,
		RequestId:       requestID,
		Kind:            daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_START,
		StatusCode:      uint32(resp.StatusCode),
		ResponseHeaders: responseHeadersToProto(resp.Header),
	}); err != nil {
		return
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		// Upstream refused the upgrade. Drain body into RESPONSE_BODY
		// frames so the browser can see the error body, then end.
		c.pumpReader(stream, tunnelID, requestID, resp.Body)
		_ = resp.Body.Close()
		_ = stream.Send(&daemonv1.ProxyFrame{
			TunnelId:  tunnelID,
			RequestId: requestID,
			Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_END,
		})
		return
	}

	// Post-handshake: bidirectional raw byte relay.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		// client → server: server-side REQUEST_BODY frames land in `body`
		// via the requestBodyMux; relay them straight into the TCP conn.
		buf := make([]byte, tunnelProxyBodyChunk)
		for {
			n, err := body.Read(buf)
			if n > 0 {
				if _, writeErr := conn.Write(buf[:n]); writeErr != nil {
					return
				}
			}
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		// server → client: read from the already-buffered reader (it may
		// hold bytes consumed by ReadResponse that belong to the ws body).
		buf := make([]byte, tunnelProxyBodyChunk)
		for {
			n, err := bufReader.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				if sendErr := stream.Send(&daemonv1.ProxyFrame{
					TunnelId:  tunnelID,
					RequestId: requestID,
					Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_BODY,
					BodyChunk: chunk,
				}); sendErr != nil {
					return
				}
			}
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				return
			}
		}
	}()
	wg.Wait()
	_ = stream.Send(&daemonv1.ProxyFrame{
		TunnelId:  tunnelID,
		RequestId: requestID,
		Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_END,
	})
	_ = slog.Default() // keep slog import live if we later add logging here
}

// pumpReader drains an io.Reader into RESPONSE_BODY frames. Used for the
// non-upgrade branch of handleWebSocket where the upstream refused the
// handshake and returned a normal error response instead.
func (c *tunnelProxyClient) pumpReader(stream proxySender, tunnelID, requestID string, r io.Reader) {
	buf := make([]byte, tunnelProxyBodyChunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if sendErr := stream.Send(&daemonv1.ProxyFrame{
				TunnelId:  tunnelID,
				RequestId: requestID,
				Kind:      daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_BODY,
				BodyChunk: chunk,
			}); sendErr != nil {
				return
			}
		}
		if errors.Is(err, io.EOF) || err != nil {
			return
		}
	}
}

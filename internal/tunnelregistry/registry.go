// Package tunnelregistry tracks live ProxyExchange streams per daemon and
// routes inbound /preview/<token>/ HTTP requests over those streams.
//
// The registry is the single shared state between the HTTP reverse-proxy
// handler (on the public server mux) and the gRPC bidi ProxyExchange
// handler (on the daemon control service). It is deliberately isolated in
// its own package so both the server and daemonrpc layers can import it
// without creating a dependency cycle.
package tunnelregistry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
)

// Errors returned by the registry. HTTP callers translate these into
// specific status codes (see the preview handler in internal/server).
var (
	ErrNoDaemon     = errors.New("no active daemon stream for this computer")
	ErrStreamClosed = errors.New("proxy stream closed mid-request")
	ErrRequestGone  = errors.New("request responder no longer waiting")
)

// Stream is the writer half the gRPC handler owns. The registry never
// reads from the underlying bidi stream; the daemon handler does that in
// a goroutine and calls Dispatch on us for each inbound frame.
type Stream interface {
	Send(frame *daemonv1.ProxyFrame) error
}

// Responder is the HTTP side of one in-flight request. The reverse-proxy
// handler registers a responder before sending REQUEST_START; the gRPC
// handler pushes RESPONSE_* frames into it; the handler Waits on it for
// the next frame to write out.
type Responder struct {
	frames chan *daemonv1.ProxyFrame
	done   chan struct{}
	once   sync.Once
}

// NewResponder returns a responder with a buffered channel sized for
// typical small-to-medium responses. Back-pressure on large streams is
// applied by the daemon not sending faster than the consumer drains.
func NewResponder() *Responder {
	return &Responder{
		frames: make(chan *daemonv1.ProxyFrame, 32),
		done:   make(chan struct{}),
	}
}

// Push delivers a daemon→server frame to the HTTP handler. Returns false
// if the responder has already been closed (client disconnected).
func (r *Responder) Push(frame *daemonv1.ProxyFrame) bool {
	select {
	case <-r.done:
		return false
	case r.frames <- frame:
		return true
	}
}

// Next blocks until the next frame arrives or ctx is done. Returns
// (nil, nil) when the responder has been closed cleanly.
func (r *Responder) Next(ctx context.Context) (*daemonv1.ProxyFrame, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return nil, nil
	case frame, ok := <-r.frames:
		if !ok {
			return nil, nil
		}
		return frame, nil
	}
}

// Close marks the responder as finished; subsequent Push calls drop
// frames rather than blocking on an abandoned channel.
func (r *Responder) Close() {
	r.once.Do(func() { close(r.done) })
}

// Registry is the hub. Safe for concurrent use.
type Registry struct {
	mu         sync.Mutex
	streams    map[string]Stream               // computerID → stream
	responders map[string]map[string]*Responder // computerID → requestID → responder
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{
		streams:    make(map[string]Stream),
		responders: make(map[string]map[string]*Responder),
	}
}

// Attach binds a stream to a computer. If a prior stream exists for the
// same computer, the caller is responsible for closing that stream first
// — Attach overwrites without signalling.
func (rg *Registry) Attach(computerID string, stream Stream) {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	rg.streams[computerID] = stream
}

// Detach removes the stream for a computer and closes every pending
// responder bound to it, unblocking their Next() callers with a nil
// frame. Call when the gRPC handler returns.
func (rg *Registry) Detach(computerID string, stream Stream) {
	rg.mu.Lock()
	// Only clear if the current stream matches — prevents a fresh reconnect
	// from being torn down by the previous handler's defer.
	if rg.streams[computerID] == stream {
		delete(rg.streams, computerID)
	}
	responders := rg.responders[computerID]
	delete(rg.responders, computerID)
	rg.mu.Unlock()
	for _, responder := range responders {
		responder.Close()
	}
}

// Register allocates a request ID, stores the responder, and returns
// both so the HTTP handler can correlate inbound daemon frames. Returns
// ErrNoDaemon when the computer has no attached stream.
func (rg *Registry) Register(computerID string, responder *Responder) (string, Stream, error) {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	stream, ok := rg.streams[computerID]
	if !ok {
		return "", nil, ErrNoDaemon
	}
	bucket, ok := rg.responders[computerID]
	if !ok {
		bucket = make(map[string]*Responder)
		rg.responders[computerID] = bucket
	}
	requestID := newRequestID()
	bucket[requestID] = responder
	return requestID, stream, nil
}

// Unregister removes a responder when the HTTP handler is done with it.
// Safe to call multiple times.
func (rg *Registry) Unregister(computerID, requestID string) {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	bucket, ok := rg.responders[computerID]
	if !ok {
		return
	}
	delete(bucket, requestID)
	if len(bucket) == 0 {
		delete(rg.responders, computerID)
	}
}

// Dispatch routes a daemon→server frame to the right responder. Returns
// ErrRequestGone when the responder was already unregistered — the gRPC
// handler typically logs-and-continues on that error rather than failing
// the whole stream.
func (rg *Registry) Dispatch(computerID string, frame *daemonv1.ProxyFrame) error {
	requestID := frame.GetRequestId()
	rg.mu.Lock()
	bucket := rg.responders[computerID]
	var responder *Responder
	if bucket != nil {
		responder = bucket[requestID]
	}
	rg.mu.Unlock()
	if responder == nil {
		return ErrRequestGone
	}
	if !responder.Push(frame) {
		return ErrRequestGone
	}
	return nil
}

// HasDaemon reports whether any stream is attached for the computer.
// Useful for preflight 503 responses without allocating a responder.
func (rg *Registry) HasDaemon(computerID string) bool {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	_, ok := rg.streams[computerID]
	return ok
}

// newRequestID returns a 16-byte hex string (128 bits). Collision-safe
// for the lifetime of a single daemon stream.
func newRequestID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

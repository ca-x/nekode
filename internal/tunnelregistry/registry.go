// Package tunnelregistry tracks live ProxyExchange streams per daemon and
// routes inbound /preview/<token>/ HTTP requests over those streams.
//
// The registry is the single shared state between the HTTP reverse-proxy
// handler (on the public server mux) and the connect-rpc bidi ProxyExchange
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

// Stream is the writer half the connect handler owns. The registry never
// reads from the underlying bidi stream; the daemon handler does that in
// a goroutine and calls Dispatch on us for each inbound frame.
type Stream interface {
	Send(frame *daemonv1.ProxyFrame) error
}

// lockedStream serializes Send calls on a single underlying RPC stream.
// The stream should have one sender at a time, so the
// registry wraps every attached stream in this guard before handing it
// out to HTTP responders (which run one per in-flight request and each
// call Send independently for REQUEST_START / REQUEST_BODY / REQUEST_END).
type lockedStream struct {
	mu     sync.Mutex
	inner  Stream
	closed bool
}

func newLockedStream(inner Stream) *lockedStream {
	return &lockedStream{inner: inner}
}

// Send serializes writes. Returns an error matching io.ErrClosedPipe
// semantics when the stream has already been detached, so handlers can
// distinguish "stream gone" from "transient send failure".
func (ls *lockedStream) Send(frame *daemonv1.ProxyFrame) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.closed {
		return ErrStreamClosed
	}
	return ls.inner.Send(frame)
}

// markClosed flips the sentinel so further Send calls short-circuit
// instead of touching a stream the handler has already returned from.
func (ls *lockedStream) markClosed() {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.closed = true
}

// Responder is the HTTP side of one in-flight request. The reverse-proxy
// handler registers a responder before sending REQUEST_START; the RPC
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
	streams    map[string]*lockedStream // computerID → serialized stream
	responders map[string]map[string]*streamOwnedResponder
}

// streamOwnedResponder ties a Responder to the specific stream it was
// registered against. On reconnect-race we can then close only the
// responders that actually belong to the stale stream, leaving any
// responders bound to the fresh stream alive.
type streamOwnedResponder struct {
	stream    *lockedStream
	responder *Responder
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{
		streams:    make(map[string]*lockedStream),
		responders: make(map[string]map[string]*streamOwnedResponder),
	}
}

// Attach binds a stream to a computer. If a prior stream exists for the
// same computer, it is marked closed — subsequent sends on the old
// stream's handle will return ErrStreamClosed instead of silently racing.
func (rg *Registry) Attach(computerID string, stream Stream) Stream {
	wrapped := newLockedStream(stream)
	rg.mu.Lock()
	if existing, ok := rg.streams[computerID]; ok {
		existing.markClosed()
	}
	rg.streams[computerID] = wrapped
	rg.mu.Unlock()
	return wrapped
}

// Detach removes the stream for a computer and closes every pending
// responder that was registered against this specific stream handle.
// Responders belonging to a newer stream (if the daemon reconnected
// faster than the old handler's defer) are left untouched so their
// in-flight requests can still complete.
func (rg *Registry) Detach(computerID string, stream Stream) {
	wrapped, ok := stream.(*lockedStream)
	if !ok {
		// Defensive: callers must pass the handle Attach returned.
		return
	}
	wrapped.markClosed()
	rg.mu.Lock()
	if rg.streams[computerID] == wrapped {
		delete(rg.streams, computerID)
	}
	bucket := rg.responders[computerID]
	var stale []*Responder
	if bucket != nil {
		for id, owned := range bucket {
			if owned.stream == wrapped {
				stale = append(stale, owned.responder)
				delete(bucket, id)
			}
		}
		if len(bucket) == 0 {
			delete(rg.responders, computerID)
		}
	}
	rg.mu.Unlock()
	for _, responder := range stale {
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
		bucket = make(map[string]*streamOwnedResponder)
		rg.responders[computerID] = bucket
	}
	requestID := newRequestID()
	bucket[requestID] = &streamOwnedResponder{stream: stream, responder: responder}
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
// ErrRequestGone when the responder was already unregistered — the RPC
// handler typically logs-and-continues on that error rather than failing
// the whole stream.
func (rg *Registry) Dispatch(computerID string, frame *daemonv1.ProxyFrame) error {
	requestID := frame.GetRequestId()
	rg.mu.Lock()
	bucket := rg.responders[computerID]
	var owned *streamOwnedResponder
	if bucket != nil {
		owned = bucket[requestID]
	}
	rg.mu.Unlock()
	if owned == nil {
		return ErrRequestGone
	}
	if !owned.responder.Push(frame) {
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

package tunnelregistry

import (
	"context"
	"testing"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
)

type fakeStream struct {
	frames []*daemonv1.ProxyFrame
}

func (f *fakeStream) Send(frame *daemonv1.ProxyFrame) error {
	f.frames = append(f.frames, frame)
	return nil
}

func TestRegisterReturnsErrNoDaemon(t *testing.T) {
	reg := New()
	_, _, err := reg.Register("computer-1", NewResponder())
	if err != ErrNoDaemon {
		t.Fatalf("expected ErrNoDaemon, got %v", err)
	}
}

func TestAttachRegisterDispatch(t *testing.T) {
	reg := New()
	stream := &fakeStream{}
	reg.Attach("computer-1", stream)
	responder := NewResponder()
	id, got, err := reg.Register("computer-1", responder)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got != stream {
		t.Fatalf("wrong stream returned")
	}
	if id == "" {
		t.Fatalf("expected non-empty request id")
	}
	frame := &daemonv1.ProxyFrame{RequestId: id, Kind: daemonv1.ProxyFrameKind_PROXY_FRAME_KIND_RESPONSE_END}
	if err := reg.Dispatch("computer-1", frame); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got1, err := responder.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got1 != frame {
		t.Fatalf("wrong frame delivered")
	}
}

func TestDispatchErrRequestGone(t *testing.T) {
	reg := New()
	reg.Attach("computer-1", &fakeStream{})
	err := reg.Dispatch("computer-1", &daemonv1.ProxyFrame{RequestId: "unknown"})
	if err != ErrRequestGone {
		t.Fatalf("expected ErrRequestGone, got %v", err)
	}
}

func TestDetachClosesResponders(t *testing.T) {
	reg := New()
	stream := &fakeStream{}
	reg.Attach("computer-1", stream)
	responder := NewResponder()
	_, _, err := reg.Register("computer-1", responder)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = responder.Next(ctx)
		close(done)
	}()
	reg.Detach("computer-1", stream)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Detach should unblock pending responders")
	}
	if reg.HasDaemon("computer-1") {
		t.Fatalf("HasDaemon should be false after Detach")
	}
}

func TestDetachIgnoresStaleStream(t *testing.T) {
	reg := New()
	first := &fakeStream{}
	second := &fakeStream{}
	reg.Attach("computer-1", first)
	reg.Attach("computer-1", second)
	// Simulate the first handler's defer firing after reconnect.
	reg.Detach("computer-1", first)
	if !reg.HasDaemon("computer-1") {
		t.Fatalf("second stream should still be attached")
	}
}

package imadapter

import (
	"testing"
	"time"

	"github.com/ca-x/nekode/internal/iminbound"
)

func TestDedupeCacheMarkSeen(t *testing.T) {
	now := time.Unix(100, 0)
	cache := DedupeCache{TTL: time.Minute, Now: func() time.Time { return now }}
	msg := iminbound.Message{EndpointID: "ep", ExternalMessageID: "m1"}
	if cache.MarkSeen(msg) {
		t.Fatal("first MarkSeen() = true, want false")
	}
	if !cache.MarkSeen(msg) {
		t.Fatal("second MarkSeen() = false, want true")
	}
	now = now.Add(2 * time.Minute)
	if cache.MarkSeen(msg) {
		t.Fatal("expired MarkSeen() = true, want false")
	}
}

func TestRenderStream(t *testing.T) {
	if got := RenderStream(StreamState{}, 0); got != "Thinking... ▍" {
		t.Fatalf("empty RenderStream() = %q", got)
	}
	got := RenderStream(StreamState{Text: "hello", ActiveTool: "bash: go test"}, 0)
	if got != "hello\n\nbash: go test ▍" {
		t.Fatalf("tool RenderStream() = %q", got)
	}
	got = RenderStream(StreamState{Text: "done", StartedUnix: 10, UpdatedUnix: 12, Done: true}, 0)
	if got != "done\n\nResponse time: 2.0s" {
		t.Fatalf("done RenderStream() = %q", got)
	}
	got = RenderStream(StreamState{Text: "abcdefghijklmnopqrstuvwxyz"}, 10)
	if got != "abcdefg..." {
		t.Fatalf("truncated RenderStream() = %q", got)
	}
}

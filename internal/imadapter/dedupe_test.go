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

func TestDedupeCacheIgnoresMessagesWithoutStableKey(t *testing.T) {
	cache := DedupeCache{}
	for _, msg := range []iminbound.Message{
		{},
		{EndpointID: "ep"},
		{ExternalMessageID: "m1"},
	} {
		if cache.MarkSeen(msg) {
			t.Fatalf("MarkSeen(%+v) = true, want false for messages without dedupe key", msg)
		}
		if cache.MarkSeen(msg) {
			t.Fatalf("second MarkSeen(%+v) = true, want false for messages without dedupe key", msg)
		}
	}
}

func TestDedupeCacheForget(t *testing.T) {
	cache := DedupeCache{}
	msg := iminbound.Message{EndpointID: "ep", ExternalMessageID: "m1"}
	if cache.MarkSeen(msg) {
		t.Fatal("first MarkSeen() = true, want false")
	}
	cache.Forget(msg)
	if cache.MarkSeen(msg) {
		t.Fatal("MarkSeen() after Forget = true, want false")
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

func TestRenderStreamTruncatesOnUTF8Boundary(t *testing.T) {
	got := RenderStream(StreamState{Text: "你好世界"}, len("你好世")+2)
	if got != "你好..." {
		t.Fatalf("UTF-8 truncated RenderStream() = %q", got)
	}
	if got := RenderStream(StreamState{Text: "abcdef"}, 3); got != "abc" {
		t.Fatalf("small max RenderStream() = %q, want raw byte cut", got)
	}
}

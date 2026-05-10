package server

import (
	"testing"
	"time"
)

func TestTunnelRateLimiterAllowsBurst(t *testing.T) {
	l := newTunnelRateLimiter()
	for i := 0; i < int(l.capacity); i++ {
		if !l.Allow("tun-1") {
			t.Fatalf("expected burst request %d to be allowed", i)
		}
	}
	if l.Allow("tun-1") {
		t.Fatalf("expected request after burst to be rejected")
	}
}

func TestTunnelRateLimiterRefills(t *testing.T) {
	l := newTunnelRateLimiter()
	for i := 0; i < int(l.capacity); i++ {
		l.Allow("tun-1")
	}
	// Simulate the passage of time by reaching into the bucket directly.
	// This avoids a wall-clock sleep which would make the test flaky on
	// busy CI machines.
	l.mu.Lock()
	l.buckets["tun-1"].lastSeen = time.Now().Add(-2 * time.Second)
	l.mu.Unlock()
	if !l.Allow("tun-1") {
		t.Fatalf("expected refilled bucket to allow a request")
	}
}

func TestTunnelRateLimiterIsolatesTunnels(t *testing.T) {
	l := newTunnelRateLimiter()
	for i := 0; i < int(l.capacity); i++ {
		l.Allow("tun-1")
	}
	if !l.Allow("tun-2") {
		t.Fatalf("exhausting one tunnel should not affect another")
	}
}

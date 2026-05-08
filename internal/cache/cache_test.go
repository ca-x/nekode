package cache

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBadgerCacheRoundTripAndTTL(t *testing.T) {
	ctx := context.Background()
	c, err := Open(ctx, Options{Driver: DriverBadger, BadgerDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	if err := c.Set(ctx, "k", []byte("value"), time.Second); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got) != "value" {
		t.Fatalf("Get() = %q, want value", got)
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := c.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(deleted) error = %v, want %v", err, ErrNotFound)
	}
}

func TestProjectionKeyIncludesConsistencyScope(t *testing.T) {
	key := ProjectionKey("server:1", 7, "cache-v2", "agent-status", "agent/1")
	for _, want := range []string{"nekode", "projection", "p7"} {
		if !strings.Contains(key, want) {
			t.Fatalf("ProjectionKey() = %q, want part %q", key, want)
		}
	}
	if strings.Contains(key, "server:1") || strings.Contains(key, "agent/1") {
		t.Fatalf("ProjectionKey() = %q, want escaped dynamic parts", key)
	}
}

func TestRedisDriverRequiresAddr(t *testing.T) {
	_, err := Open(context.Background(), Options{Driver: DriverRedis})
	if err == nil {
		t.Fatal("Open(redis without addr) error = nil, want error")
	}
}

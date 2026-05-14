package imadapter

import (
	"context"
	"testing"
	"time"
)

func TestOutgoingRateLimiterDisabled(t *testing.T) {
	limiter := NewOutgoingRateLimiter(OutgoingRateLimit{}, nil)
	start := time.Now()
	for range 20 {
		if err := limiter.Wait(context.Background(), ProviderTelegram); err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("disabled limiter waited %v, want immediate", elapsed)
	}
}

func TestOutgoingRateLimiterBurstThenThrottle(t *testing.T) {
	limiter := NewOutgoingRateLimiter(OutgoingRateLimit{MaxPerSecond: 20, Burst: 1}, nil)
	if err := limiter.Wait(context.Background(), ProviderTelegram); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}
	before := time.Now()
	if err := limiter.Wait(context.Background(), ProviderTelegram); err != nil {
		t.Fatalf("second Wait() error = %v", err)
	}
	if waited := time.Since(before); waited < 25*time.Millisecond {
		t.Fatalf("second Wait() waited %v, want throttled", waited)
	}
}

func TestOutgoingRateLimiterPerProviderOverride(t *testing.T) {
	limiter := NewOutgoingRateLimiter(
		OutgoingRateLimit{MaxPerSecond: 1000, Burst: 100},
		map[string]OutgoingRateLimit{ProviderWeixin: {MaxPerSecond: 20, Burst: 1}},
	)
	start := time.Now()
	for range 10 {
		if err := limiter.Wait(context.Background(), ProviderTelegram); err != nil {
			t.Fatalf("telegram Wait() error = %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("default provider waited %v, want within burst", elapsed)
	}
	if err := limiter.Wait(context.Background(), ProviderWeixin); err != nil {
		t.Fatalf("first weixin Wait() error = %v", err)
	}
	before := time.Now()
	if err := limiter.Wait(context.Background(), ProviderWeixin); err != nil {
		t.Fatalf("second weixin Wait() error = %v", err)
	}
	if waited := time.Since(before); waited < 25*time.Millisecond {
		t.Fatalf("weixin override waited %v, want throttled", waited)
	}
}

func TestOutgoingRateLimiterContextCancellation(t *testing.T) {
	limiter := NewOutgoingRateLimiter(OutgoingRateLimit{MaxPerSecond: 1, Burst: 1}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	if err := limiter.Wait(ctx, ProviderQQ); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}
	cancel()
	if err := limiter.Wait(ctx, ProviderQQ); err == nil {
		t.Fatal("Wait() error = nil, want context cancellation")
	}
}

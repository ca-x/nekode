package imadapter

import (
	"context"
	"math"
	"sync"
	"time"
)

type OutgoingRateWaiter interface {
	Wait(context.Context, string) error
}

type OutgoingRateLimit struct {
	MaxPerSecond float64
	Burst        int
}

type OutgoingRateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*outgoingBucket
	defaults  OutgoingRateLimit
	overrides map[string]OutgoingRateLimit
	now       func() time.Time
}

type outgoingBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
}

func NewOutgoingRateLimiter(defaults OutgoingRateLimit, overrides map[string]OutgoingRateLimit) *OutgoingRateLimiter {
	if overrides == nil {
		overrides = map[string]OutgoingRateLimit{}
	}
	return &OutgoingRateLimiter{
		buckets:   map[string]*outgoingBucket{},
		defaults:  defaults,
		overrides: overrides,
	}
}

func DefaultOutgoingRateLimiter() *OutgoingRateLimiter {
	defaultOutgoingRateLimiterOnce.Do(func() {
		defaultOutgoingRateLimiter = NewOutgoingRateLimiter(OutgoingRateLimit{MaxPerSecond: 10, Burst: 10}, map[string]OutgoingRateLimit{
			ProviderTelegram:   {MaxPerSecond: 20, Burst: 10},
			ProviderFeishu:     {MaxPerSecond: 8, Burst: 4},
			ProviderQQ:         {MaxPerSecond: 5, Burst: 3},
			ProviderWeixin:     {MaxPerSecond: 5, Burst: 3},
			ProviderWeCom:      {MaxPerSecond: 3, Burst: 2},
			ProviderServerChan: {MaxPerSecond: 3, Burst: 2},
		})
	})
	return defaultOutgoingRateLimiter
}

var (
	defaultOutgoingRateLimiterOnce sync.Once
	defaultOutgoingRateLimiter     *OutgoingRateLimiter
)

func (l *OutgoingRateLimiter) Wait(ctx context.Context, provider string) error {
	if l == nil {
		return nil
	}
	provider = CanonicalProvider(provider)
	cfg := l.config(provider)
	if cfg.MaxPerSecond <= 0 {
		return nil
	}
	for {
		l.mu.Lock()
		bucket := l.bucket(provider, cfg)
		bucket.refill(l.clock())
		if bucket.tokens >= 1 {
			bucket.tokens--
			l.mu.Unlock()
			return nil
		}
		wait := time.Duration(((1 - bucket.tokens) / bucket.refillRate) * float64(time.Second))
		l.mu.Unlock()

		if wait <= 0 {
			wait = time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *OutgoingRateLimiter) config(provider string) OutgoingRateLimit {
	if cfg, ok := l.overrides[provider]; ok {
		return cfg
	}
	return l.defaults
}

func (l *OutgoingRateLimiter) bucket(provider string, cfg OutgoingRateLimit) *outgoingBucket {
	if bucket, ok := l.buckets[provider]; ok {
		return bucket
	}
	burst := cfg.Burst
	if burst <= 0 {
		burst = int(math.Ceil(cfg.MaxPerSecond))
	}
	if burst <= 0 {
		burst = 1
	}
	bucket := &outgoingBucket{
		tokens:     float64(burst),
		maxTokens:  float64(burst),
		refillRate: cfg.MaxPerSecond,
		lastRefill: l.clock(),
	}
	l.buckets[provider] = bucket
	return bucket
}

func (l *OutgoingRateLimiter) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func (b *outgoingBucket) refill(now time.Time) {
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now
}

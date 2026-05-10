package server

import (
	"sync"
	"time"
)

// tunnelRateLimiter caps inbound /preview/<token>/ request rate per
// tunnel to protect the daemon stream from being buried under traffic.
// Simple token-bucket: each tunnel gets `capacity` tokens refilling at
// `refillPerSec` per second. Tunnels not seen recently are GC'd so
// idle records don't leak memory.
type tunnelRateLimiter struct {
	capacity     float64
	refillPerSec float64
	idleCutoff   time.Duration

	mu      sync.Mutex
	buckets map[string]*tunnelBucket
}

type tunnelBucket struct {
	tokens   float64
	lastSeen time.Time
}

// newTunnelRateLimiter returns a limiter sized for typical preview
// workloads: 20 req/s sustained with a 40-request burst.
func newTunnelRateLimiter() *tunnelRateLimiter {
	return &tunnelRateLimiter{
		capacity:     40,
		refillPerSec: 20,
		idleCutoff:   5 * time.Minute,
		buckets:      make(map[string]*tunnelBucket),
	}
}

// Allow returns true if the request for tunnelID can proceed. It
// refills the bucket based on wall-clock elapsed time, spends one
// token, and opportunistically sweeps idle buckets.
func (l *tunnelRateLimiter) Allow(tunnelID string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, ok := l.buckets[tunnelID]
	if !ok {
		bucket = &tunnelBucket{tokens: l.capacity, lastSeen: now}
		l.buckets[tunnelID] = bucket
	}
	elapsed := now.Sub(bucket.lastSeen).Seconds()
	if elapsed > 0 {
		bucket.tokens += elapsed * l.refillPerSec
		if bucket.tokens > l.capacity {
			bucket.tokens = l.capacity
		}
	}
	bucket.lastSeen = now
	// Amortized sweep: every ~256th call we purge idle entries.
	if len(l.buckets) > 16 {
		cutoff := now.Add(-l.idleCutoff)
		for id, b := range l.buckets {
			if id != tunnelID && b.lastSeen.Before(cutoff) {
				delete(l.buckets, id)
			}
		}
	}
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

package imadapter

import (
	"sync"
	"time"

	"github.com/ca-x/nekode/internal/iminbound"
)

const DefaultDedupeTTL = time.Minute

type DedupeCache struct {
	TTL time.Duration
	Now func() time.Time

	mu   sync.Mutex
	seen map[string]time.Time
}

func (c *DedupeCache) MarkSeen(message iminbound.Message) bool {
	key := message.DedupeKey()
	if key == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = map[string]time.Time{}
	}
	now := c.now()
	ttl := c.TTL
	if ttl <= 0 {
		ttl = DefaultDedupeTTL
	}
	for existing, firstSeen := range c.seen {
		if now.Sub(firstSeen) > ttl {
			delete(c.seen, existing)
		}
	}
	if _, ok := c.seen[key]; ok {
		return true
	}
	c.seen[key] = now
	return false
}

func (c *DedupeCache) Forget(message iminbound.Message) {
	key := message.DedupeKey()
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.seen, key)
}

func (c *DedupeCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

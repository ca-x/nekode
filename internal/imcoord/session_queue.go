package imcoord

import (
	"context"
	"sync"
	"time"
)

type sessionQueue struct {
	mu          sync.Mutex
	sessions    map[string]*sessionSlot
	idleTimeout time.Duration
}

type sessionSlot struct {
	parent *sessionQueue
	key    string
	queue  chan queuedRequest

	mu           sync.Mutex
	activeCancel context.CancelFunc
	refs         int
}

type queuedRequest struct {
	ctx     context.Context
	fn      func(context.Context) (Result, error)
	resultC chan queueResult
}

type queueResult struct {
	result Result
	err    error
}

func newSessionQueue() *sessionQueue {
	return newSessionQueueWithIdleTimeout(5 * time.Minute)
}

func newSessionQueueWithIdleTimeout(idleTimeout time.Duration) *sessionQueue {
	return &sessionQueue{
		sessions:    make(map[string]*sessionSlot),
		idleTimeout: idleTimeout,
	}
}

func (q *sessionQueue) enqueue(ctx context.Context, sessionKey string, fn func(context.Context) (Result, error)) (Result, error) {
	slot := q.getOrCreate(sessionKey)
	resultC := make(chan queueResult, 1)
	req := queuedRequest{ctx: ctx, fn: fn, resultC: resultC}
	select {
	case slot.queue <- req:
		q.release(slot)
	case <-ctx.Done():
		q.release(slot)
		return Result{}, ctx.Err()
	}
	select {
	case res := <-resultC:
		return res.result, res.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func (q *sessionQueue) abort(sessionKey string) bool {
	q.mu.Lock()
	slot, ok := q.sessions[sessionKey]
	q.mu.Unlock()
	if !ok {
		return false
	}
	slot.mu.Lock()
	cancel := slot.activeCancel
	slot.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (q *sessionQueue) getOrCreate(sessionKey string) *sessionSlot {
	q.mu.Lock()
	defer q.mu.Unlock()
	if slot, ok := q.sessions[sessionKey]; ok {
		slot.refs++
		return slot
	}
	slot := &sessionSlot{
		parent: q,
		key:    sessionKey,
		queue:  make(chan queuedRequest, 64),
		refs:   1,
	}
	q.sessions[sessionKey] = slot
	go slot.run()
	return slot
}

func (q *sessionQueue) release(slot *sessionSlot) {
	q.mu.Lock()
	defer q.mu.Unlock()
	current, ok := q.sessions[slot.key]
	if !ok || current != slot || slot.refs == 0 {
		return
	}
	slot.refs--
}

func (q *sessionQueue) tryDeleteIdle(slot *sessionSlot) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	current, ok := q.sessions[slot.key]
	if !ok || current != slot || len(slot.queue) != 0 || slot.refs != 0 {
		return false
	}
	slot.mu.Lock()
	active := slot.activeCancel != nil
	slot.mu.Unlock()
	if active {
		return false
	}
	delete(q.sessions, slot.key)
	return true
}

func (s *sessionSlot) run() {
	for {
		timer := time.NewTimer(s.parent.idleTimeout)
		select {
		case req := <-s.queue:
			if !timer.Stop() {
				<-timer.C
			}
			if req.ctx.Err() != nil {
				req.resultC <- queueResult{err: req.ctx.Err()}
				continue
			}
			ctx, cancel := context.WithCancel(req.ctx)
			s.mu.Lock()
			s.activeCancel = cancel
			s.mu.Unlock()
			result, err := req.fn(ctx)
			cancel()
			s.mu.Lock()
			s.activeCancel = nil
			s.mu.Unlock()
			req.resultC <- queueResult{result: result, err: err}
		case <-timer.C:
			if s.parent.tryDeleteIdle(s) {
				return
			}
		}
	}
}

package runtime

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"redpanda/protocol/methods"
)

const defaultSubAgentPoolSize = 2
const maxSubAgentPoolSize = 8

var errSubAgentPoolClosed = errors.New("subagent process pool is closed")

type subAgentProcessFactory func(context.Context, methods.ReplyParams, string) (processSubAgent, error)

type healthyProcessSubAgent interface {
	Ping(context.Context) error
}

type subAgentPoolGrant struct {
	child  processSubAgent
	create bool
	err    error
}

type subAgentPoolWaiter struct {
	grant      chan subAgentPoolGrant
	enqueuedAt time.Time
}

type subAgentProcessPool struct {
	mu      sync.Mutex
	limit   int
	active  int
	idle    []processSubAgent
	waiters []*subAgentPoolWaiter
	closed  bool

	totalWaits int64
	lastWait   time.Duration
	maxWait    time.Duration
	factory    subAgentProcessFactory
}

type subAgentPoolStatus struct {
	Limit      int   `json:"limit"`
	Active     int   `json:"active"`
	Idle       int   `json:"idle"`
	InUse      int   `json:"in_use"`
	Queued     int   `json:"queued"`
	Max        int   `json:"max"`
	TotalWaits int64 `json:"total_waits"`
	LastWaitMS int64 `json:"last_wait_ms"`
	MaxWaitMS  int64 `json:"max_wait_ms"`
}

func newSubAgentProcessPool(limit int, factory subAgentProcessFactory) *subAgentProcessPool {
	if limit <= 0 {
		limit = defaultSubAgentPoolSize
	}
	if limit > maxSubAgentPoolSize {
		limit = maxSubAgentPoolSize
	}
	return &subAgentProcessPool{
		limit:   limit,
		factory: factory,
	}
}

func subAgentPoolSizeFromEnv() int {
	value, err := strconv.Atoi(os.Getenv("RED_PANDA_SUBAGENT_POOL_SIZE"))
	if err != nil || value <= 0 {
		return defaultSubAgentPoolSize
	}
	if value > maxSubAgentPoolSize {
		return maxSubAgentPoolSize
	}
	return value
}

func (p *subAgentProcessPool) Acquire(ctx context.Context, params methods.ReplyParams, subAgentID string) (processSubAgent, func(bool), error) {
	return p.AcquireWithStart(ctx, params, subAgentID, nil)
}

func (p *subAgentProcessPool) AcquireWithStart(
	ctx context.Context,
	params methods.ReplyParams,
	subAgentID string,
	onStarting func(),
) (processSubAgent, func(bool), error) {
	waiter := &subAgentPoolWaiter{
		grant:      make(chan subAgentPoolGrant, 1),
		enqueuedAt: time.Now(),
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, nil, errSubAgentPoolClosed
	}
	p.waiters = append(p.waiters, waiter)
	p.dispatchLocked()
	p.mu.Unlock()

	select {
	case grant := <-waiter.grant:
		return p.acceptGrant(ctx, params, subAgentID, grant, onStarting)
	case <-ctx.Done():
		p.mu.Lock()
		removed := p.removeWaiterLocked(waiter)
		if removed {
			p.dispatchLocked()
			p.mu.Unlock()
			return nil, nil, ctx.Err()
		}
		p.mu.Unlock()

		// The waiter was granted concurrently with cancellation. Return the
		// reserved resource before reporting cancellation so capacity cannot leak.
		grant := <-waiter.grant
		p.discardGrant(grant)
		return nil, nil, ctx.Err()
	}
}

func (p *subAgentProcessPool) acceptGrant(
	ctx context.Context,
	params methods.ReplyParams,
	subAgentID string,
	grant subAgentPoolGrant,
	onStarting func(),
) (processSubAgent, func(bool), error) {
	if grant.err != nil {
		return nil, nil, grant.err
	}
	if onStarting != nil {
		onStarting()
	}
	if !grant.create {
		if health, ok := grant.child.(healthyProcessSubAgent); ok {
			healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := health.Ping(healthCtx)
			cancel()
			if err != nil {
				_ = grant.child.Close(context.Background())
				p.mu.Lock()
				p.active--
				canReplace := !p.closed && p.active < p.limit
				if canReplace {
					p.active++
				}
				p.dispatchLocked()
				p.mu.Unlock()
				if !canReplace {
					return p.AcquireWithStart(ctx, params, subAgentID, nil)
				}
				child, createErr := p.factory(ctx, params, subAgentID)
				if createErr != nil {
					p.mu.Lock()
					p.active--
					p.dispatchLocked()
					p.mu.Unlock()
					return nil, nil, createErr
				}
				return child, p.releaseFunc(child), nil
			}
		}
		return grant.child, p.releaseFunc(grant.child), nil
	}

	child, err := p.factory(ctx, params, subAgentID)
	if err != nil {
		p.mu.Lock()
		p.active--
		p.dispatchLocked()
		p.mu.Unlock()
		return nil, nil, err
	}
	return child, p.releaseFunc(child), nil
}

func (p *subAgentProcessPool) discardGrant(grant subAgentPoolGrant) {
	if grant.err != nil {
		return
	}
	if grant.child != nil {
		p.releaseFunc(grant.child)(true)
		return
	}
	if grant.create {
		p.mu.Lock()
		p.active--
		p.dispatchLocked()
		p.mu.Unlock()
	}
}

func (p *subAgentProcessPool) dispatchLocked() {
	if p.closed {
		for len(p.waiters) > 0 {
			waiter := p.waiters[0]
			p.waiters = p.waiters[1:]
			waiter.grant <- subAgentPoolGrant{err: errSubAgentPoolClosed}
		}
		return
	}
	for len(p.waiters) > 0 {
		waiter := p.waiters[0]
		var grant subAgentPoolGrant
		switch {
		case len(p.idle) > 0 && p.active <= p.limit:
			last := len(p.idle) - 1
			grant.child = p.idle[last]
			p.idle = p.idle[:last]
		case p.active < p.limit:
			p.active++
			grant.create = true
		default:
			return
		}
		p.waiters = p.waiters[1:]
		wait := time.Since(waiter.enqueuedAt)
		p.totalWaits++
		p.lastWait = wait
		if wait > p.maxWait {
			p.maxWait = wait
		}
		waiter.grant <- grant
	}
}

func (p *subAgentProcessPool) removeWaiterLocked(target *subAgentPoolWaiter) bool {
	for index, waiter := range p.waiters {
		if waiter != target {
			continue
		}
		copy(p.waiters[index:], p.waiters[index+1:])
		p.waiters[len(p.waiters)-1] = nil
		p.waiters = p.waiters[:len(p.waiters)-1]
		return true
	}
	return false
}

func (p *subAgentProcessPool) releaseFunc(child processSubAgent) func(bool) {
	var once sync.Once
	return func(reusable bool) {
		once.Do(func() {
			if child == nil {
				return
			}
			closeChild := false
			p.mu.Lock()
			if reusable && !p.closed && p.active <= p.limit {
				p.idle = append(p.idle, child)
			} else {
				p.active--
				closeChild = true
			}
			p.dispatchLocked()
			p.mu.Unlock()
			if closeChild {
				go child.Close(context.Background())
			}
		})
	}
}

// Status returns a snapshot of pool capacity and occupancy.
func (p *subAgentProcessPool) Status() subAgentPoolStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statusLocked()
}

func (p *subAgentProcessPool) statusLocked() subAgentPoolStatus {
	idle := len(p.idle)
	return subAgentPoolStatus{
		Limit:      p.limit,
		Active:     p.active,
		Idle:       idle,
		InUse:      p.active - idle,
		Queued:     len(p.waiters),
		Max:        maxSubAgentPoolSize,
		TotalWaits: p.totalWaits,
		LastWaitMS: p.lastWait.Milliseconds(),
		MaxWaitMS:  p.maxWait.Milliseconds(),
	}
}

// SetLimit updates the pool capacity. Shrinking closes idle workers until the
// active count satisfies the new limit; in-use workers finish naturally.
func (p *subAgentProcessPool) SetLimit(limit int) subAgentPoolStatus {
	if limit <= 0 {
		limit = defaultSubAgentPoolSize
	}
	if limit > maxSubAgentPoolSize {
		limit = maxSubAgentPoolSize
	}
	var overflow []processSubAgent
	p.mu.Lock()
	p.limit = limit
	for len(p.idle) > 0 && (p.active > limit || len(p.idle) > limit) {
		last := len(p.idle) - 1
		overflow = append(overflow, p.idle[last])
		p.idle = p.idle[:last]
		p.active--
	}
	p.dispatchLocked()
	status := p.statusLocked()
	p.mu.Unlock()
	for _, child := range overflow {
		_ = child.Close(context.Background())
	}
	return status
}

// Reset closes all idle workers so queued acquires create fresh processes.
// In-use workers are left alone until they complete.
func (p *subAgentProcessPool) Reset(ctx context.Context) subAgentPoolStatus {
	p.mu.Lock()
	idle := p.idle
	p.idle = nil
	p.active -= len(idle)
	p.dispatchLocked()
	status := p.statusLocked()
	p.mu.Unlock()
	for _, child := range idle {
		_ = child.Close(ctx)
	}
	return status
}

func (p *subAgentProcessPool) Close(ctx context.Context) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	idle := p.idle
	p.idle = nil
	p.active -= len(idle)
	p.dispatchLocked()
	p.mu.Unlock()
	for _, child := range idle {
		_ = child.Close(ctx)
	}
}

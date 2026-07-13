package subagent

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	"redpanda/protocol/methods"
)

const defaultSubAgentPoolSize = 2
const MaxPoolSize = 8

type subAgentProcessFactory func(context.Context, methods.ReplyParams, string) (Process, error)

type ProcessPool struct {
	mu      sync.Mutex
	limit   int
	active  int
	idle    []Process
	factory subAgentProcessFactory
}

type subAgentPoolStatus struct {
	Limit  int `json:"limit"`
	Active int `json:"active"`
	Idle   int `json:"idle"`
	InUse  int `json:"in_use"`
	Max    int `json:"max"`
}

func NewProcessPool(limit int, factory subAgentProcessFactory) *ProcessPool {
	if limit <= 0 {
		limit = defaultSubAgentPoolSize
	}
	if limit > MaxPoolSize {
		limit = MaxPoolSize
	}
	return &ProcessPool{
		limit:   limit,
		factory: factory,
	}
}

func PoolSizeFromEnv() int {
	value, err := strconv.Atoi(os.Getenv("RED_PANDA_SUBAGENT_POOL_SIZE"))
	if err != nil || value <= 0 {
		return defaultSubAgentPoolSize
	}
	if value > MaxPoolSize {
		return MaxPoolSize
	}
	return value
}

func (p *ProcessPool) Acquire(ctx context.Context, params methods.ReplyParams, subAgentID string) (Process, func(bool), error) {
	for {
		p.mu.Lock()
		if last := len(p.idle) - 1; last >= 0 {
			child := p.idle[last]
			p.idle = p.idle[:last]
			p.mu.Unlock()
			return child, p.releaseFunc(child), nil
		}
		if p.active < p.limit {
			p.active++
			p.mu.Unlock()
			child, err := p.factory(ctx, params, subAgentID)
			if err != nil {
				p.mu.Lock()
				p.active--
				p.mu.Unlock()
				return nil, nil, err
			}
			return child, p.releaseFunc(child), nil
		}
		p.mu.Unlock()

		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (p *ProcessPool) releaseFunc(child Process) func(bool) {
	return func(reusable bool) {
		if child == nil {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if reusable && len(p.idle) < p.limit {
			p.idle = append(p.idle, child)
			return
		}
		p.active--
		go child.Close(context.Background())
	}
}

// Status returns a snapshot of pool capacity and occupancy.
func (p *ProcessPool) Status() subAgentPoolStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	idle := len(p.idle)
	return subAgentPoolStatus{
		Limit:  p.limit,
		Active: p.active,
		Idle:   idle,
		InUse:  p.active - idle,
		Max:    MaxPoolSize,
	}
}

// SetLimit updates the pool capacity. Shrinking only affects future acquires
// and how many idle workers may be retained; excess idle workers are closed.
func (p *ProcessPool) SetLimit(limit int) subAgentPoolStatus {
	if limit <= 0 {
		limit = defaultSubAgentPoolSize
	}
	if limit > MaxPoolSize {
		limit = MaxPoolSize
	}
	var overflow []Process
	p.mu.Lock()
	p.limit = limit
	for len(p.idle) > limit {
		last := len(p.idle) - 1
		overflow = append(overflow, p.idle[last])
		p.idle = p.idle[:last]
		p.active--
	}
	status := subAgentPoolStatus{
		Limit:  p.limit,
		Active: p.active,
		Idle:   len(p.idle),
		InUse:  p.active - len(p.idle),
		Max:    MaxPoolSize,
	}
	p.mu.Unlock()
	for _, child := range overflow {
		_ = child.Close(context.Background())
	}
	return status
}

// Reset closes all idle workers so the next acquire creates fresh processes.
// In-use workers are left alone until they complete.
func (p *ProcessPool) Reset(ctx context.Context) subAgentPoolStatus {
	p.mu.Lock()
	idle := p.idle
	p.idle = nil
	p.active -= len(idle)
	status := subAgentPoolStatus{
		Limit:  p.limit,
		Active: p.active,
		Idle:   0,
		InUse:  p.active,
		Max:    MaxPoolSize,
	}
	p.mu.Unlock()
	for _, child := range idle {
		_ = child.Close(ctx)
	}
	return status
}

func (p *ProcessPool) Close(ctx context.Context) {
	p.mu.Lock()
	idle := p.idle
	p.idle = nil
	p.active -= len(idle)
	p.mu.Unlock()
	for _, child := range idle {
		_ = child.Close(ctx)
	}
}

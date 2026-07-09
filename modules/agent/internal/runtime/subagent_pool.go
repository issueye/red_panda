package runtime

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	"redpanda/protocol/methods"
)

const defaultSubAgentPoolSize = 2
const maxSubAgentPoolSize = 8

type subAgentProcessFactory func(context.Context, methods.ReplyParams, string) (processSubAgent, error)

type subAgentProcessPool struct {
	mu      sync.Mutex
	limit   int
	active  int
	idle    []processSubAgent
	factory subAgentProcessFactory
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

func (p *subAgentProcessPool) releaseFunc(child processSubAgent) func(bool) {
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

func (p *subAgentProcessPool) Close(ctx context.Context) {
	p.mu.Lock()
	idle := p.idle
	p.idle = nil
	p.active -= len(idle)
	p.mu.Unlock()
	for _, child := range idle {
		_ = child.Close(ctx)
	}
}

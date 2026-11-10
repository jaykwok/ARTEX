package mcphttp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Autumn-27/artex/config"
)

var ErrCircuitOpen = errors.New("MCP server temporarily unavailable (circuit open); switch method or record the failure")

// Shared across sessions: a per-client semaphore would not limit multiple workers.
var breakers sync.Map

type circuit struct {
	sem                 chan struct{}
	mu                  sync.Mutex
	failures, threshold int
	cooldown            time.Duration
	until               time.Time
	probe               bool
	generation          uint64
}

func serverCircuit(server, endpoint string, headers map[string]string) *circuit {
	r := config.RuntimeOptions()
	b, _ := json.Marshal(headers)
	key := fmt.Sprintf("%s\x00%s\x00%x", server, endpoint, sha256.Sum256(b))
	c := &circuit{sem: make(chan struct{}, r.MCPConcurrency), threshold: r.MCPFailureThreshold, cooldown: time.Duration(r.MCPCooldownSeconds) * time.Second}
	v, _ := breakers.LoadOrStore(key, c)
	return v.(*circuit)
}

func (c *circuit) enter(ctx context.Context) (func(error), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	open := !c.until.IsZero() && (time.Now().Before(c.until) || c.probe)
	c.mu.Unlock()
	if open {
		return nil, ErrCircuitOpen
	}
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	if !c.until.IsZero() && (time.Now().Before(c.until) || c.probe) {
		c.mu.Unlock()
		<-c.sem
		return nil, ErrCircuitOpen
	}
	isProbe := !c.until.IsZero()
	if isProbe {
		c.probe = true
	}
	gen := c.generation
	c.mu.Unlock()
	return func(err error) {
		defer func() { <-c.sem }()
		c.mu.Lock()
		defer c.mu.Unlock()
		if gen != c.generation {
			return
		}
		if isProbe {
			c.probe = false
		}
		// Caller cancellation and JSON-RPC application errors are not server outages.
		var rpc *rpcError
		if errors.Is(err, context.Canceled) || errors.As(err, &rpc) {
			return
		}
		if err == nil {
			c.failures = 0
			c.until = time.Time{}
			return
		}
		c.failures++
		if isProbe || c.failures >= c.threshold {
			c.until = time.Now().Add(c.cooldown)
			c.generation++
		}
	}, nil
}

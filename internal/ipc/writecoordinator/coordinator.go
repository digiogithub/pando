// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package writecoordinator serialises all SQLite write operations through a
// single goroutine, eliminating concurrent write contention on the primary
// instance and providing a natural chokepoint for metrics and backpressure.
package writecoordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc/changepub"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/logging"
)

const defaultQueueSize = 256

// WriteJob is a single write request queued for serialised execution.
type WriteJob struct {
	req     dbproxy.WriteRequest
	resultC chan WriteResult
}

// WriteResult is the outcome of a serialised write operation.
type WriteResult struct {
	Result json.RawMessage
	Error  error
}

// CoordinatorMetrics holds a consistent snapshot of coordinator statistics.
type CoordinatorMetrics struct {
	Accepted   uint64
	Completed  uint64
	Failed     uint64
	QueueDepth int
	MaxQueue   int
}

// Coordinator serialises all write operations through a single background
// goroutine, satisfying the dbproxy.WriteSubmitter interface.
type Coordinator struct {
	q      db.Querier
	jobs   chan WriteJob
	done   chan struct{}
	cancel context.CancelFunc

	// publisher receives a best-effort notification after every successful write.
	// nil means no publishing (equivalent to NoopPublisher).
	publisher changepub.Publisher

	// Atomic counters allow metrics reads without acquiring mu.
	accepted  atomic.Uint64
	completed atomic.Uint64
	failed    atomic.Uint64

	mu         sync.Mutex
	queueDepth int
	maxQueue   int
}

// New creates a Coordinator and starts the serialisation goroutine.
// queueSize specifies the job-channel buffer; values <= 0 default to 256.
func New(ctx context.Context, q db.Querier, queueSize int) *Coordinator {
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	runCtx, cancel := context.WithCancel(ctx)
	c := &Coordinator{
		q:        q,
		jobs:     make(chan WriteJob, queueSize),
		done:     make(chan struct{}),
		cancel:   cancel,
		maxQueue: queueSize,
	}
	go c.run(runCtx)
	return c
}

// SetPublisher attaches a change-event publisher to the coordinator.
// Must be called before the first Submit; not safe for concurrent use.
func (c *Coordinator) SetPublisher(pub changepub.Publisher) {
	c.publisher = pub
}

// Submit enqueues a write job and blocks until the result is available or
// the caller's context is cancelled. It satisfies dbproxy.WriteSubmitter.
func (c *Coordinator) Submit(ctx context.Context, req dbproxy.WriteRequest) (json.RawMessage, error) {
	job := WriteJob{
		req:     req,
		resultC: make(chan WriteResult, 1),
	}

	c.mu.Lock()
	c.queueDepth++
	c.mu.Unlock()

	// Enqueue or bail out if the coordinator is shutting down / context cancelled.
	select {
	case <-ctx.Done():
		c.mu.Lock()
		c.queueDepth--
		c.mu.Unlock()
		return nil, fmt.Errorf("writecoordinator: submit cancelled: %w", ctx.Err())
	case <-c.done:
		c.mu.Lock()
		c.queueDepth--
		c.mu.Unlock()
		return nil, fmt.Errorf("writecoordinator: coordinator is shut down")
	case c.jobs <- job:
		c.accepted.Add(1)
	}

	// Wait for the result.
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("writecoordinator: result wait cancelled: %w", ctx.Err())
	case <-c.done:
		return nil, fmt.Errorf("writecoordinator: coordinator shut down while waiting for result")
	case res := <-job.resultC:
		return res.Result, res.Error
	}
}

// Shutdown stops the coordinator gracefully. In-flight jobs that have already
// been dequeued will still complete; pending jobs that are still in the channel
// will have their callers unblocked via the done signal.
func (c *Coordinator) Shutdown() {
	c.cancel()
	<-c.done
}

// Metrics returns a consistent snapshot of coordinator statistics.
func (c *Coordinator) Metrics() CoordinatorMetrics {
	c.mu.Lock()
	depth := c.queueDepth
	maxQ := c.maxQueue
	c.mu.Unlock()
	return CoordinatorMetrics{
		Accepted:   c.accepted.Load(),
		Completed:  c.completed.Load(),
		Failed:     c.failed.Load(),
		QueueDepth: depth,
		MaxQueue:   maxQ,
	}
}

// publishChange fires a best-effort write-change event for the given method.
// A publish failure is logged as a warning but never propagated to the caller.
func (c *Coordinator) publishChange(ctx context.Context, method string) {
	if c.publisher == nil {
		return
	}
	topic := changepub.MethodToTopic(method)
	if topic == "" {
		return
	}
	if err := c.publisher.Publish(ctx, topic, map[string]string{"method": method}); err != nil {
		logging.Warn("writecoordinator: failed to publish change event", "topic", topic, "error", err)
	}
}

// run is the single serialisation goroutine. It drains c.jobs until the
// context is cancelled, then closes c.done to signal Shutdown and unblock
// any pending Submit callers.
func (c *Coordinator) run(ctx context.Context) {
	defer close(c.done)
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-c.jobs:
			if !ok {
				return
			}
			c.mu.Lock()
			c.queueDepth--
			c.mu.Unlock()

			result, err := dbproxy.DispatchWrite(ctx, c.q, job.req)
			if err != nil {
				c.failed.Add(1)
			} else {
				c.completed.Add(1)
				c.publishChange(ctx, job.req.Method)
			}
			job.resultC <- WriteResult{Result: result, Error: err}
		}
	}
}

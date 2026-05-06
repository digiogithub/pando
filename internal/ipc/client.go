// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-zeromq/zmq4"
)

// dealerConn is a cached DEALER socket for a specific RPC endpoint.
type dealerConn struct {
	mu     sync.Mutex
	dealer zmq4.Socket
	addr   string
}

// Client connects to a primary's Bus using SUB and DEALER sockets.
// A single persistent DEALER socket is reused per endpoint to avoid the
// overhead of creating/destroying zmq4 goroutines on every RPC call.
type Client struct {
	ctx  context.Context
	opts Options

	// connMu protects the dealers map.
	connMu  sync.Mutex
	dealers map[string]*dealerConn
}

// NewClient creates a new Client for subscribing to events and making RPC calls.
func NewClient(ctx context.Context) (*Client, error) {
	return &Client{
		ctx:     ctx,
		opts:    DefaultOptions,
		dealers: make(map[string]*dealerConn),
	}, nil
}

// getOrCreateDealer returns the cached DEALER for routerEndpoint, creating it
// on first access. The returned conn's mu is NOT held; callers must lock it
// themselves around the send/recv pair.
func (c *Client) getOrCreateDealer(routerEndpoint string) (*dealerConn, error) {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if dc, ok := c.dealers[routerEndpoint]; ok {
		return dc, nil
	}

	dealer := zmq4.NewDealer(c.ctx,
		zmq4.WithID(zmq4.SocketIdentity(fmt.Sprintf("pando-client-%d", time.Now().UnixNano()))),
		zmq4.WithDialerTimeout(c.opts.DialTimeout),
		zmq4.WithDialerMaxRetries(3),
	)

	if err := dealer.Dial(routerEndpoint); err != nil {
		_ = dealer.Close()
		return nil, fmt.Errorf("%w: dial ROUTER endpoint %s: %v", ErrConnectionFailed, routerEndpoint, err)
	}

	dc := &dealerConn{dealer: dealer, addr: routerEndpoint}
	c.dealers[routerEndpoint] = dc
	return dc, nil
}

// SubscribeTo connects to a PUB endpoint and subscribes to the given topics.
// Returns a channel that receives Envelope messages until the context is cancelled
// or the socket is closed.
func (c *Client) SubscribeTo(pubEndpoint string, topics ...string) (<-chan Envelope, error) {
	sub := zmq4.NewSub(c.ctx,
		zmq4.WithDialerTimeout(c.opts.DialTimeout),
		zmq4.WithDialerMaxRetries(3),
	)

	if err := sub.Dial(pubEndpoint); err != nil {
		_ = sub.Close()
		return nil, fmt.Errorf("%w: dial PUB endpoint %s: %v", ErrConnectionFailed, pubEndpoint, err)
	}

	// Subscribe to all requested topics.
	for _, topic := range topics {
		if err := sub.SetOption(zmq4.OptionSubscribe, topic); err != nil {
			_ = sub.Close()
			return nil, fmt.Errorf("ipc: subscribe to topic %q: %w", topic, err)
		}
	}

	// If no topics specified, subscribe to everything.
	if len(topics) == 0 {
		if err := sub.SetOption(zmq4.OptionSubscribe, ""); err != nil {
			_ = sub.Close()
			return nil, fmt.Errorf("ipc: subscribe to all topics: %w", err)
		}
	}

	ch := make(chan Envelope, 64)

	go func() {
		defer sub.Close()
		defer close(ch)

		for {
			msg, err := sub.Recv()
			if err != nil {
				select {
				case <-c.ctx.Done():
				default:
					// log silently; subscriber will notice the closed channel
				}
				return
			}

			if len(msg.Frames) == 0 {
				continue
			}

			// Frame layout: topic + 0x00 + json_envelope
			_, envJSON, ok := splitTopicPayload(msg.Frames[0])
			if !ok {
				continue
			}

			var env Envelope
			if err := json.Unmarshal(envJSON, &env); err != nil {
				continue
			}

			select {
			case <-c.ctx.Done():
				return
			case ch <- env:
			}
		}
	}()

	return ch, nil
}

// Call sends a JSON-RPC request to a ROUTER endpoint and waits for the response.
// A persistent DEALER socket is reused per endpoint to avoid the overhead of
// creating/closing zmq4 goroutines on every call.
func (c *Client) Call(ctx context.Context, routerEndpoint, method string, params any) (json.RawMessage, error) {
	dc, err := c.getOrCreateDealer(routerEndpoint)
	if err != nil {
		return nil, err
	}

	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("ipc: marshal params: %w", err)
	}

	reqID := fmt.Sprintf("req-%d", time.Now().UnixNano())
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  rawParams,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ipc: marshal request: %w", err)
	}

	// Serialize access to the shared DEALER socket (send + matching recv must
	// be atomic from this socket's perspective).
	dc.mu.Lock()
	defer dc.mu.Unlock()

	// DEALER sends: [empty][data] — zmq4 prepends the identity automatically.
	if err := dc.dealer.Send(zmq4.NewMsgFrom([]byte{}, reqBytes)); err != nil {
		// Socket may have become stale; evict it so the next call recreates it.
		c.evictDealer(routerEndpoint)
		return nil, fmt.Errorf("ipc: send RPC request: %w", err)
	}

	// Wait for response with timeout.
	callCtx, cancel := context.WithTimeout(ctx, c.opts.CallTimeout)
	defer cancel()

	type result struct {
		msg zmq4.Msg
		err error
	}
	done := make(chan result, 1)
	go func() {
		msg, err := dc.dealer.Recv()
		done <- result{msg, err}
	}()

	select {
	case <-callCtx.Done():
		// Evict the stale socket so subsequent calls start fresh.
		c.evictDealer(routerEndpoint)
		return nil, fmt.Errorf("%w: waiting for response to method %q", ErrTimeout, method)
	case r := <-done:
		if r.err != nil {
			c.evictDealer(routerEndpoint)
			return nil, fmt.Errorf("ipc: recv RPC response: %w", r.err)
		}

		// DEALER receives: [empty][data] or just [data]
		frames := r.msg.Frames
		data := frames[len(frames)-1]

		var resp rpcResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("ipc: unmarshal RPC response: %w", err)
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("ipc: RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		return resp.Result, nil
	}
}

// evictDealer removes and closes the cached DEALER for the given endpoint.
// Must NOT be called while dc.mu is held (deadlock risk).
func (c *Client) evictDealer(routerEndpoint string) {
	c.connMu.Lock()
	dc, ok := c.dealers[routerEndpoint]
	if ok {
		delete(c.dealers, routerEndpoint)
	}
	c.connMu.Unlock()

	if ok {
		_ = dc.dealer.Close()
	}
}

// Close closes any resources held by the client, including cached dealer sockets.
func (c *Client) Close() error {
	c.connMu.Lock()
	dealers := c.dealers
	c.dealers = make(map[string]*dealerConn)
	c.connMu.Unlock()

	for _, dc := range dealers {
		_ = dc.dealer.Close()
	}
	return nil
}

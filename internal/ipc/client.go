// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-zeromq/zmq4"
)

// Client connects to a primary's Bus using SUB and DEALER sockets.
type Client struct {
	ctx  context.Context
	opts Options
}

// NewClient creates a new Client for subscribing to events and making RPC calls.
func NewClient(ctx context.Context) (*Client, error) {
	return &Client{
		ctx:  ctx,
		opts: DefaultOptions,
	}, nil
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
// A fresh DEALER socket is used per call.
func (c *Client) Call(ctx context.Context, routerEndpoint, method string, params any) (json.RawMessage, error) {
	dealer := zmq4.NewDealer(ctx,
		zmq4.WithID(zmq4.SocketIdentity(fmt.Sprintf("client-%d", time.Now().UnixNano()))),
		zmq4.WithDialerTimeout(c.opts.DialTimeout),
		zmq4.WithDialerMaxRetries(3),
	)
	defer dealer.Close()

	if err := dealer.Dial(routerEndpoint); err != nil {
		return nil, fmt.Errorf("%w: dial ROUTER endpoint %s: %v", ErrConnectionFailed, routerEndpoint, err)
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

	// DEALER sends: [empty][data] — zmq4 prepends the identity automatically.
	// We send the request data directly; the DEALER socket handles framing.
	if err := dealer.Send(zmq4.NewMsgFrom([]byte{}, reqBytes)); err != nil {
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
		msg, err := dealer.Recv()
		done <- result{msg, err}
	}()

	select {
	case <-callCtx.Done():
		return nil, fmt.Errorf("%w: waiting for response to method %q", ErrTimeout, method)
	case r := <-done:
		if r.err != nil {
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

// Close closes any resources held by the client.
// Currently the Client is stateless between calls; this is a no-op.
func (c *Client) Close() error {
	return nil
}

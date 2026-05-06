// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/go-zeromq/zmq4"
)

// HandlerFunc is a JSON-RPC method handler.
type HandlerFunc func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)

// rpcRequest is a JSON-RPC 2.0 request envelope.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response envelope.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Bus is the server-side ZMQ transport backbone. Only the primary instance creates a Bus.
// It binds a PUB socket for event broadcasting and a ROUTER socket for JSON-RPC requests.
type Bus struct {
	// PubAddr is the address the PUB socket is bound to (e.g. "tcp://127.0.0.1:40000").
	PubAddr string
	// RPCAddr is the address the ROUTER socket is bound to (e.g. "tcp://127.0.0.1:40001").
	RPCAddr string

	instanceID string

	pubSock    zmq4.Socket
	routerSock zmq4.Socket

	mu       sync.RWMutex
	handlers map[string]HandlerFunc

	cancel context.CancelFunc
}

// NewBus creates a Bus for the given instanceID.
func NewBus(instanceID string) *Bus {
	return &Bus{
		instanceID: instanceID,
		handlers:   make(map[string]HandlerFunc),
	}
}

// Start binds the PUB and ROUTER sockets on the given ports and starts a background
// goroutine to handle incoming ROUTER messages.
func (b *Bus) Start(ctx context.Context, pubPort, rpcPort int) error {
	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel

	b.PubAddr = fmt.Sprintf("tcp://127.0.0.1:%d", pubPort)
	b.RPCAddr = fmt.Sprintf("tcp://127.0.0.1:%d", rpcPort)

	b.pubSock = zmq4.NewPub(ctx)
	if err := b.pubSock.Listen(b.PubAddr); err != nil {
		cancel()
		return fmt.Errorf("ipc: bind PUB socket on %s: %w", b.PubAddr, err)
	}

	b.routerSock = zmq4.NewRouter(ctx, zmq4.WithID(zmq4.SocketIdentity(b.instanceID)))
	if err := b.routerSock.Listen(b.RPCAddr); err != nil {
		_ = b.pubSock.Close()
		cancel()
		return fmt.Errorf("ipc: bind ROUTER socket on %s: %w", b.RPCAddr, err)
	}

	go b.serveRPC(ctx)

	return nil
}

// Shutdown closes both sockets gracefully.
func (b *Bus) Shutdown() error {
	if b.cancel != nil {
		b.cancel()
	}
	var errs []error
	if b.pubSock != nil {
		if err := b.pubSock.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close PUB: %w", err))
		}
	}
	if b.routerSock != nil {
		if err := b.routerSock.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close ROUTER: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("ipc: shutdown errors: %v", errs)
	}
	return nil
}

// Publish sends an Envelope on the PUB socket.
// The ZMQ message frame layout is: [topic_bytes + 0x00 + json_envelope_bytes]
// so subscribers can filter by topic prefix.
func (b *Bus) Publish(topic string, payload any) error {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ipc: marshal payload: %w", err)
	}

	env := Envelope{
		InstanceID: b.instanceID,
		Topic:      topic,
		Timestamp:  time.Now().UTC(),
		Payload:    rawPayload,
	}

	envBytes, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("ipc: marshal envelope: %w", err)
	}

	// Single frame: topic + null separator + JSON envelope.
	frame := append([]byte(topic), 0x00)
	frame = append(frame, envBytes...)

	if err := b.pubSock.Send(zmq4.NewMsg(frame)); err != nil {
		return fmt.Errorf("ipc: send on PUB socket: %w", err)
	}
	return nil
}

// RegisterMethod registers a JSON-RPC handler for the given method name.
func (b *Bus) RegisterMethod(method string, handler HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[method] = handler
}

// serveRPC runs in a goroutine receiving ROUTER frames and dispatching JSON-RPC calls.
// ROUTER recv layout: [identity][empty?][json_request_bytes]
// Transient errors are logged and retried with exponential back-off so that a
// single bad frame or a brief socket hiccup does not kill the RPC handler.
func (b *Bus) serveRPC(ctx context.Context) {
	const (
		backoffMin = 5 * time.Millisecond
		backoffMax = 500 * time.Millisecond
	)
	backoff := backoffMin

	for {
		msg, err := b.routerSock.Recv()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("ipc: ROUTER recv error (retrying in %s): %v", backoff, err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				// Exponential back-off, capped at backoffMax.
				backoff *= 2
				if backoff > backoffMax {
					backoff = backoffMax
				}
				continue
			}
		}
		// Reset back-off on successful receive.
		backoff = backoffMin
		go b.handleRPC(ctx, msg)
	}
}

func (b *Bus) handleRPC(ctx context.Context, msg zmq4.Msg) {
	frames := msg.Frames
	if len(frames) < 2 {
		log.Printf("ipc: ROUTER received malformed message with %d frames", len(frames))
		return
	}

	// Identity is always the first frame; data is the last frame.
	// Depending on the DEALER client there may or may not be an empty frame in between.
	identity := frames[0]
	data := frames[len(frames)-1]

	var req rpcRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("ipc: failed to unmarshal RPC request: %v", err)
		b.sendRPCError(identity, "", -32700, "parse error")
		return
	}

	b.mu.RLock()
	handler, ok := b.handlers[req.Method]
	b.mu.RUnlock()

	var resp rpcResponse
	resp.JSONRPC = "2.0"
	resp.ID = req.ID

	if !ok {
		resp.Error = &rpcError{Code: -32601, Message: ErrMethodNotFound.Error()}
	} else {
		result, err := handler(ctx, req.Method, req.Params)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = result
		}
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		log.Printf("ipc: failed to marshal RPC response: %v", err)
		return
	}

	// Reply format: [identity][empty][response_bytes]
	reply := zmq4.NewMsgFrom(identity, []byte{}, respBytes)
	if err := b.routerSock.Send(reply); err != nil {
		log.Printf("ipc: failed to send RPC response: %v", err)
	}
}

func (b *Bus) sendRPCError(identity []byte, id string, code int, message string) {
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	}
	respBytes, _ := json.Marshal(resp)
	reply := zmq4.NewMsgFrom(identity, []byte{}, respBytes)
	_ = b.routerSock.Send(reply)
}

// splitTopicPayload splits a PUB frame at the first null byte, returning the topic and JSON envelope.
func splitTopicPayload(frame []byte) (topic string, envJSON []byte, ok bool) {
	idx := bytes.IndexByte(frame, 0x00)
	if idx < 0 {
		return "", nil, false
	}
	return string(frame[:idx]), frame[idx+1:], true
}

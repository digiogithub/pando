// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package writecoordinator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
)

// The coordinator's handover refusals cross the IPC boundary as plain text; a
// forwarding secondary must classify them as "retry against the next
// primary", or writes issued during a handover fail instead of waiting.
func TestHandoverRefusalsClassifyAsUnavailable(t *testing.T) {
	ctx := context.Background()
	c := New(ctx, nil, 4)
	drainCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := c.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
	_, err := c.Submit(ctx, dbproxy.WriteRequest{Method: "CreateSession"})
	if !errors.Is(err, ErrDraining) {
		t.Fatalf("Submit while draining: %v", err)
	}
	asText := errors.New("ipc: RPC error -32000: " + err.Error())
	if werr := dbproxy.ClassifyError("CreateSession", asText); werr.Code != dbproxy.ErrCodeUnavailable {
		t.Fatalf("draining refusal classified as %s, want %s", werr.Code, dbproxy.ErrCodeUnavailable)
	}

	c2 := New(ctx, nil, 4)
	c2.Shutdown()
	_, err = c2.Submit(ctx, dbproxy.WriteRequest{Method: "CreateSession"})
	if err == nil {
		t.Fatal("Submit after Shutdown must fail")
	}
	if werr := dbproxy.ClassifyError("CreateSession", errors.New("ipc: RPC error -32000: "+err.Error())); werr.Code != dbproxy.ErrCodeUnavailable {
		t.Fatalf("shut-down refusal %q classified as %s, want %s", err, werr.Code, dbproxy.ErrCodeUnavailable)
	}
}

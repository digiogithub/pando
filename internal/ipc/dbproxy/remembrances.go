// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package dbproxy

import (
	"context"
	"encoding/json"
)

// RemembrancesDispatcher defines the contract for dispatching RAG (KB, Events, Code)
// write operations on the primary instance. It decouples the dbproxy infrastructure
// package from the rag business-logic packages, avoiding circular imports.
//
// The primary instance registers an implementation via RegisterRemembrancesDispatcher.
// When dispatchWrite receives a method name not handled by db.Querier, it delegates
// to this dispatcher (if registered) so that KB, Events and Code indexing writes
// forwarded from secondary instances are correctly persisted.
type RemembrancesDispatcher interface {
	DispatchRemembrancesWrite(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
}

// remembrancesDispatcher is the package-level registry for the primary dispatcher.
var remembrancesDispatcher RemembrancesDispatcher

// RegisterRemembrancesDispatcher registers the RAG write dispatcher on the primary.
// Must be called before any secondary instance attempts to forward remembrances writes.
// Calling it more than once replaces the previous registration.
func RegisterRemembrancesDispatcher(d RemembrancesDispatcher) {
	remembrancesDispatcher = d
}

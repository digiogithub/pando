// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package dbproxy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
)

// BusRegistrar is the minimal interface needed to register dbproxy RPC methods.
type BusRegistrar interface {
	RegisterMethod(method string, handler ipc.HandlerFunc)
}

// WriteSubmitter serialises and executes a write request, returning the JSON result.
// Implemented by writecoordinator.Coordinator to avoid circular imports.
type WriteSubmitter interface {
	Submit(ctx context.Context, req WriteRequest) (json.RawMessage, error)
}

// RegisterHandlers registers the db.write JSON-RPC handler on the given bus.
// Only the primary instance should call this.
func RegisterHandlers(bus BusRegistrar, q db.Querier) {
	bus.RegisterMethod(MethodDBWrite, func(ctx context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
		var req WriteRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("dbproxy handler: unmarshal WriteRequest: %w", err)
		}
		return dispatchWrite(ctx, q, req)
	})
}

// RegisterHandlersWithCoordinator registers the db.write handler using a WriteSubmitter
// for serialisation. Both RegisterHandlers and this function can coexist; the coordinator
// path is opt-in and replaces the direct-dispatch path on the primary.
func RegisterHandlersWithCoordinator(bus BusRegistrar, s WriteSubmitter) {
	bus.RegisterMethod(MethodDBWrite, func(ctx context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
		var req WriteRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("dbproxy handler: unmarshal WriteRequest: %w", err)
		}
		return s.Submit(ctx, req)
	})
}

// DispatchWrite is the exported entry point for dispatching a write request.
// The writecoordinator calls this from its serialisation loop.
func DispatchWrite(ctx context.Context, q db.Querier, req WriteRequest) (json.RawMessage, error) {
	return dispatchWrite(ctx, q, req)
}

// dispatchWrite routes a WriteRequest to the appropriate db.Querier method and
// returns the JSON-serialised result (or nil for void methods).
func dispatchWrite(ctx context.Context, q db.Querier, req WriteRequest) (json.RawMessage, error) {
	switch req.Method {
	// ---- Session writes ----
	case "CreateSession":
		var p db.CreateSessionParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		r, err := q.CreateSession(ctx, p)
		if err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return marshalResult(r, nil)

	case "UpdateSession":
		var p db.UpdateSessionParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		r, err := q.UpdateSession(ctx, p)
		if err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return marshalResult(r, nil)

	case "DeleteSession":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.DeleteSession(ctx, id); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	case "DeleteSessionMessages":
		var sessionID string
		if err := json.Unmarshal(req.Params, &sessionID); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.DeleteSessionMessages(ctx, sessionID); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	// ---- Message writes ----
	case "CreateMessage":
		var p db.CreateMessageParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		r, err := q.CreateMessage(ctx, p)
		if err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return marshalResult(r, nil)

	case "UpdateMessage":
		var p db.UpdateMessageParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.UpdateMessage(ctx, p); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	case "DeleteMessage":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.DeleteMessage(ctx, id); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	// ---- File writes ----
	case "CreateFile":
		var p db.CreateFileParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		r, err := q.CreateFile(ctx, p)
		if err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return marshalResult(r, nil)

	case "UpdateFile":
		var p db.UpdateFileParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		r, err := q.UpdateFile(ctx, p)
		if err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return marshalResult(r, nil)

	case "DeleteFile":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.DeleteFile(ctx, id); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	case "DeleteSessionFiles":
		var sessionID string
		if err := json.Unmarshal(req.Params, &sessionID); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.DeleteSessionFiles(ctx, sessionID); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	// ---- Self-improvement writes ----
	case "InsertPromptTemplate":
		var p db.InsertPromptTemplateParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		r, err := q.InsertPromptTemplate(ctx, p)
		if err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return marshalResult(r, nil)

	case "InsertSessionScore":
		var p db.InsertSessionScoreParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		r, err := q.InsertSessionScore(ctx, p)
		if err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return marshalResult(r, nil)

	case "InsertSkill":
		var p db.InsertSkillParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		r, err := q.InsertSkill(ctx, p)
		if err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return marshalResult(r, nil)

	case "DeactivateLowestSkill":
		if err := q.DeactivateLowestSkill(ctx); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	case "IncrementSkillUsage":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.IncrementSkillUsage(ctx, id); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	// ---- Project writes ----
	case "CreateProject":
		var p db.CreateProjectParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		r, err := q.CreateProject(ctx, p)
		if err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return marshalResult(r, nil)

	case "UpdateProjectStatus":
		var p db.UpdateProjectStatusParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.UpdateProjectStatus(ctx, p); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	case "UpdateProjectLastOpened":
		var p db.UpdateProjectLastOpenedParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.UpdateProjectLastOpened(ctx, p); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	case "MarkProjectInitialized":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.MarkProjectInitialized(ctx, id); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	case "DeleteProject":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, invalidParamsErr(req.Method, err)
		}
		if err := q.DeleteProject(ctx, id); err != nil {
			return nil, mapToWriteError(req.Method, err)
		}
		return nil, nil

	default:
		return nil, &WriteError{
			Code:    ErrCodeMethodNotFound,
			Method:  req.Method,
			Message: fmt.Sprintf("unknown write method %q", req.Method),
		}
	}
}

func marshalResult(v any, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("dbproxy handler: marshal result: %w", err)
	}
	return b, nil
}

// invalidParamsErr wraps a JSON unmarshal failure as a typed ErrCodeInvalidParams error.
func invalidParamsErr(method string, err error) *WriteError {
	return &WriteError{Code: ErrCodeInvalidParams, Method: method, Message: err.Error()}
}

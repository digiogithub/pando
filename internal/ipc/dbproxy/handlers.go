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

// RegisterHandlers registers the db.write JSON-RPC handler on the given bus.
// Only the primary instance should call this.
func RegisterHandlers(bus *ipc.Bus, q db.Querier) {
	bus.RegisterMethod(MethodDBWrite, func(ctx context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
		var req WriteRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("dbproxy handler: unmarshal WriteRequest: %w", err)
		}
		return dispatchWrite(ctx, q, req)
	})
}

// dispatchWrite routes a WriteRequest to the appropriate db.Querier method and
// returns the JSON-serialised result (or nil for void methods).
func dispatchWrite(ctx context.Context, q db.Querier, req WriteRequest) (json.RawMessage, error) {
	switch req.Method {
	// ---- Session writes ----
	case "CreateSession":
		var p db.CreateSessionParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		r, err := q.CreateSession(ctx, p)
		return marshalResult(r, err)

	case "UpdateSession":
		var p db.UpdateSessionParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		r, err := q.UpdateSession(ctx, p)
		return marshalResult(r, err)

	case "DeleteSession":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.DeleteSession(ctx, id)

	case "DeleteSessionMessages":
		var sessionID string
		if err := json.Unmarshal(req.Params, &sessionID); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.DeleteSessionMessages(ctx, sessionID)

	// ---- Message writes ----
	case "CreateMessage":
		var p db.CreateMessageParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		r, err := q.CreateMessage(ctx, p)
		return marshalResult(r, err)

	case "UpdateMessage":
		var p db.UpdateMessageParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.UpdateMessage(ctx, p)

	case "DeleteMessage":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.DeleteMessage(ctx, id)

	// ---- File writes ----
	case "CreateFile":
		var p db.CreateFileParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		r, err := q.CreateFile(ctx, p)
		return marshalResult(r, err)

	case "UpdateFile":
		var p db.UpdateFileParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		r, err := q.UpdateFile(ctx, p)
		return marshalResult(r, err)

	case "DeleteFile":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.DeleteFile(ctx, id)

	case "DeleteSessionFiles":
		var sessionID string
		if err := json.Unmarshal(req.Params, &sessionID); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.DeleteSessionFiles(ctx, sessionID)

	// ---- Self-improvement writes ----
	case "InsertPromptTemplate":
		var p db.InsertPromptTemplateParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		r, err := q.InsertPromptTemplate(ctx, p)
		return marshalResult(r, err)

	case "InsertSessionScore":
		var p db.InsertSessionScoreParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		r, err := q.InsertSessionScore(ctx, p)
		return marshalResult(r, err)

	case "InsertSkill":
		var p db.InsertSkillParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		r, err := q.InsertSkill(ctx, p)
		return marshalResult(r, err)

	case "DeactivateLowestSkill":
		return nil, q.DeactivateLowestSkill(ctx)

	case "IncrementSkillUsage":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.IncrementSkillUsage(ctx, id)

	// ---- Project writes ----
	case "CreateProject":
		var p db.CreateProjectParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		r, err := q.CreateProject(ctx, p)
		return marshalResult(r, err)

	case "UpdateProjectStatus":
		var p db.UpdateProjectStatusParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.UpdateProjectStatus(ctx, p)

	case "UpdateProjectLastOpened":
		var p db.UpdateProjectLastOpenedParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.UpdateProjectLastOpened(ctx, p)

	case "MarkProjectInitialized":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.MarkProjectInitialized(ctx, id)

	case "DeleteProject":
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, unmarshalErr(req.Method, err)
		}
		return nil, q.DeleteProject(ctx, id)

	default:
		return nil, fmt.Errorf("dbproxy handler: unknown write method %q", req.Method)
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

func unmarshalErr(method string, err error) error {
	return fmt.Errorf("dbproxy handler: unmarshal params for %s: %w", method, err)
}

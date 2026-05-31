package tools

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/internal/luaengine"
)

type luaTool struct {
	manager *luaengine.FilterManager
	def     luaengine.LuaToolDefinition
}

func NewLuaTools(manager *luaengine.FilterManager) []BaseTool {
	if manager == nil {
		return nil
	}
	defs := manager.LuaTools()
	result := make([]BaseTool, 0, len(defs))
	for _, def := range defs {
		result = append(result, &luaTool{manager: manager, def: def})
	}
	return result
}

func (t *luaTool) Info() ToolInfo {
	return ToolInfo{
		Name:        t.def.Name,
		Description: t.def.Description,
		Parameters:  t.def.Parameters,
		Required:    t.def.Required,
	}
}

func (t *luaTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var arguments map[string]interface{}
	if err := DecodeToolInput(call.Input, &arguments); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	data := map[string]interface{}{
		"tool_name":  t.def.Name,
		"arguments":  arguments,
		"session_id": "",
		"message_id": "",
		"call_id":    call.ID,
	}
	if sessionID, messageID := GetContextValues(ctx); sessionID != "" {
		data["session_id"] = sessionID
		data["message_id"] = messageID
	}
	result, err := t.manager.ExecuteLuaTool(ctx, t.def.Name, data)
	if err != nil {
		return ToolResponse{}, err
	}
	if result == nil {
		return NewTextErrorResponse(fmt.Sprintf("lua tool %s returned no result", t.def.Name)), nil
	}
	if output, ok := result.Data["content"].(string); ok {
		return NewTextResponse(output), nil
	}
	if output, ok := result.Data["output"].(string); ok {
		return NewTextResponse(output), nil
	}
	if errMsg, ok := result.Data["error"].(string); ok && errMsg != "" {
		return NewTextErrorResponse(errMsg), nil
	}
	return NewTextErrorResponse(fmt.Sprintf("lua tool %s must return a table with content, output, or error", t.def.Name)), nil
}
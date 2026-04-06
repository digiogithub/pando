package observability

// GenAI semantic convention attribute keys (OpenTelemetry GenAI Semconv v1.27)
const (
    AttrGenAISystem              = "gen_ai.system"
    AttrGenAIOperationName       = "gen_ai.operation.name"
    AttrGenAIRequestModel        = "gen_ai.request.model"
    AttrGenAIRequestMaxTokens    = "gen_ai.request.max_tokens"
    AttrGenAIResponseModel       = "gen_ai.response.model"
    AttrGenAIResponseFinishReasons = "gen_ai.response.finish_reasons"
    AttrGenAIUsageInputTokens    = "gen_ai.usage.input_tokens"
    AttrGenAIUsageOutputTokens   = "gen_ai.usage.output_tokens"
    AttrGenAICacheReadTokens     = "gen_ai.usage.cache_read_input_tokens"
    AttrGenAICacheCreationTokens = "gen_ai.usage.cache_creation_input_tokens"
    AttrGenAIToolCallName        = "gen_ai.tool.name"
    AttrGenAIToolCallID          = "gen_ai.tool.call.id"
    AttrGenAIMessageCount        = "gen_ai.request.message_count"
    AttrGenAIToolCount           = "gen_ai.request.tool_count"
)

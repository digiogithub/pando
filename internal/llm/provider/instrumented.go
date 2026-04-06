package provider

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/observability"
)

// instrumentedProvider wraps a Provider adding OpenTelemetry tracing.
type instrumentedProvider struct {
	inner  Provider
	tracer trace.Tracer
}

// NewInstrumentedProvider wraps p with OpenTelemetry tracing if observability is enabled.
// If disabled, returns p unchanged (zero overhead).
func NewInstrumentedProvider(p Provider) Provider {
	if !observability.IsEnabled() {
		return p
	}
	return &instrumentedProvider{
		inner:  p,
		tracer: observability.Tracer(),
	}
}

func (ip *instrumentedProvider) Model() models.Model {
	return ip.inner.Model()
}

func (ip *instrumentedProvider) SendMessages(ctx context.Context, msgs []message.Message, ts []tools.BaseTool) (*ProviderResponse, error) {
	model := ip.inner.Model()
	spanName := "chat " + model.APIModel

	ctx, span := ip.tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String(observability.AttrGenAISystem, string(model.Provider)),
			attribute.String(observability.AttrGenAIOperationName, "chat"),
			attribute.String(observability.AttrGenAIRequestModel, model.APIModel),
			attribute.Int(observability.AttrGenAIMessageCount, len(msgs)),
			attribute.Int(observability.AttrGenAIToolCount, len(ts)),
		),
	)
	defer span.End()

	resp, err := ip.inner.SendMessages(ctx, msgs, ts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.Int64(observability.AttrGenAIUsageInputTokens, resp.Usage.InputTokens),
		attribute.Int64(observability.AttrGenAIUsageOutputTokens, resp.Usage.OutputTokens),
		attribute.String(observability.AttrGenAIResponseFinishReasons, string(resp.FinishReason)),
	)
	if resp.Usage.CacheReadTokens > 0 {
		span.SetAttributes(attribute.Int64(observability.AttrGenAICacheReadTokens, resp.Usage.CacheReadTokens))
	}
	if resp.Usage.CacheCreationTokens > 0 {
		span.SetAttributes(attribute.Int64(observability.AttrGenAICacheCreationTokens, resp.Usage.CacheCreationTokens))
	}
	if len(resp.ToolCalls) > 0 {
		for _, tc := range resp.ToolCalls {
			span.AddEvent("gen_ai.tool.call", trace.WithAttributes(
				attribute.String(observability.AttrGenAIToolCallName, tc.Name),
				attribute.String(observability.AttrGenAIToolCallID, tc.ID),
			))
		}
	}

	return resp, nil
}

func (ip *instrumentedProvider) StreamResponse(ctx context.Context, msgs []message.Message, ts []tools.BaseTool) <-chan ProviderEvent {
	model := ip.inner.Model()
	spanName := "chat " + model.APIModel

	ctx, span := ip.tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String(observability.AttrGenAISystem, string(model.Provider)),
			attribute.String(observability.AttrGenAIOperationName, "chat"),
			attribute.String(observability.AttrGenAIRequestModel, model.APIModel),
			attribute.Int(observability.AttrGenAIMessageCount, len(msgs)),
			attribute.Int(observability.AttrGenAIToolCount, len(ts)),
		),
	)

	innerCh := ip.inner.StreamResponse(ctx, msgs, ts)
	outCh := make(chan ProviderEvent)

	go func() {
		defer span.End()
		defer close(outCh)
		for event := range innerCh {
			if event.Type == EventToolUseStart && event.ToolCall != nil {
				span.AddEvent("gen_ai.tool.call", trace.WithAttributes(
					attribute.String(observability.AttrGenAIToolCallName, event.ToolCall.Name),
					attribute.String(observability.AttrGenAIToolCallID, event.ToolCall.ID),
				))
			}
			if event.Type == EventComplete && event.Response != nil {
				resp := event.Response
				span.SetAttributes(
					attribute.Int64(observability.AttrGenAIUsageInputTokens, resp.Usage.InputTokens),
					attribute.Int64(observability.AttrGenAIUsageOutputTokens, resp.Usage.OutputTokens),
					attribute.String(observability.AttrGenAIResponseFinishReasons, string(resp.FinishReason)),
				)
				if resp.Usage.CacheReadTokens > 0 {
					span.SetAttributes(attribute.Int64(observability.AttrGenAICacheReadTokens, resp.Usage.CacheReadTokens))
				}
				if resp.Usage.CacheCreationTokens > 0 {
					span.SetAttributes(attribute.Int64(observability.AttrGenAICacheCreationTokens, resp.Usage.CacheCreationTokens))
				}
			}
			if event.Type == EventError && event.Error != nil {
				span.RecordError(event.Error)
				span.SetStatus(codes.Error, event.Error.Error())
			}
			outCh <- event
		}
	}()

	return outCh
}

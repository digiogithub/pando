package observability

import (
    "context"
    "crypto/tls"
    "fmt"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
    "go.opentelemetry.io/otel/trace"
    "go.opentelemetry.io/otel/trace/noop"

    "github.com/digiogithub/pando/internal/config"
)

var (
    enabled bool
    tracer  trace.Tracer
)

// IsEnabled returns true if OpenLit observability is active
func IsEnabled() bool { return enabled }

// Tracer returns the configured tracer (noop if disabled)
func Tracer() trace.Tracer { return tracer }

// Init initializes the OTLP TracerProvider pointing to OpenLit.
// Returns a shutdown function that must be called on app exit.
func Init(cfg config.OpenLitConfig, appVersion string) (func(context.Context) error, error) {
    tracer = noop.NewTracerProvider().Tracer("pando")

    if !cfg.Enabled {
        return func(ctx context.Context) error { return nil }, nil
    }

    opts := []otlptracehttp.Option{
        otlptracehttp.WithEndpointURL(cfg.Endpoint + "/v1/traces"),
    }

    if cfg.Insecure {
        opts = append(opts, otlptracehttp.WithTLSClientConfig(&tls.Config{InsecureSkipVerify: true}))
    }

    if len(cfg.CustomHeaders) > 0 {
        opts = append(opts, otlptracehttp.WithHeaders(cfg.CustomHeaders))
    }

    exporter, err := otlptracehttp.New(context.Background(), opts...)
    if err != nil {
        return nil, fmt.Errorf("openlit: create OTLP exporter: %w", err)
    }

    res, err := resource.New(context.Background(),
        resource.WithAttributes(
            semconv.ServiceName(cfg.ServiceName),
            semconv.ServiceVersion(appVersion),
        ),
    )
    if err != nil {
        res = resource.Default()
    }

    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithResource(res),
    )

    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{},
        propagation.Baggage{},
    ))

    enabled = true
    tracer = tp.Tracer("github.com/digiogithub/pando")

    return tp.Shutdown, nil
}

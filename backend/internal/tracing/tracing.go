package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Init sets up the OpenTelemetry SDK and connects to Jaeger via OTLP gRPC.
// Returns a shutdown function — call it in main() via defer to flush
// any pending spans before the process exits.
//
// serviceName distinguishes the two binaries in Jaeger UI:
//   "pairly-backend"  — WS server
//   "pairly-executor" — executor
//
// Usage in main():
//   shutdown, err := tracing.Init("pairly-backend")
//   if err != nil { log.Fatal(err) }
//   defer shutdown()
func Init(serviceName, endpoint string) (func(), error) {
	ctx := context.Background()

	// connect to Jaeger OTLP gRPC receiver
	// jaeger:4317 is the internal Docker hostname — never localhost
	conn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: connect to jaeger: %w", err)
	}

	// create the OTLP exporter — sends spans to Jaeger
	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("tracing: create exporter: %w", err)
	}

	// resource describes this service to Jaeger
	// service.name is what appears in the Jaeger UI service dropdown
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: create resource: %w", err)
	}

	// trace provider — the core of the OTel SDK
	// BatchSpanProcessor sends spans in batches for efficiency
	// rather than one HTTP call per span
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// register as the global tracer provider so otel.Tracer() works anywhere
	otel.SetTracerProvider(tp)

	// set the global propagator — this is what injects/extracts trace IDs
	// across HTTP headers and gRPC metadata (the distributed part)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// return a shutdown function that flushes pending spans
	// and closes the connection cleanly
	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			// log but don't fatal — shutdown errors are non-critical
			fmt.Printf("tracing: shutdown error: %v\n", err)
		}
	}

	return shutdown, nil
}
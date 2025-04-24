// tracing.go - OpenTelemetry instrumentation for album-service

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	// Global tracer, available for creating spans anywhere in the application
	tracer trace.Tracer
)

// setupTracing initializes OpenTelemetry
func setupTracing() (func(context.Context) error, error) {
	ctx := context.Background()

	// Get OTLP endpoint address from environment variable
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "jaeger:4317" // Default to local Jaeger GRPC endpoint
	}

	// Create OTLP exporter
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	
	conn, err := grpc.DialContext(
		ctx,
		otlpEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	
	if err != nil {
		log.Printf("Failed to create gRPC connection to collector: %v", err)
		return nil, err
	}

	// Set up OTLP exporter
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		log.Printf("Failed to create trace exporter: %v", err)
		return nil, err
	}

	// Service information - used to differentiate traces from different services
	serviceResource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("album-service"),
		semconv.ServiceVersion("1.0.0"),
		attribute.String("environment", os.Getenv("ENVIRONMENT")),
	)

	// Create tracer provider
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(serviceResource),
	)
	otel.SetTracerProvider(tracerProvider)

	// Set up W3C propagator for passing context between services
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create global tracer instance
	tracer = otel.Tracer("album-service")

	// Return cleanup function
	cleanup := func(ctx context.Context) error {
		// Set timeout to ensure all pending trace data is sent
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := tracerProvider.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
			return err
		}
		return nil
	}

	return cleanup, nil
}

// ExtractTraceInfoFromKafkaMessage extracts trace information from a Kafka message
func ExtractTraceInfoFromKafkaMessage(ctx context.Context, headers []kafka.Header) context.Context {
	// Create carrier to store header information
	carrier := propagation.MapCarrier{}
	
	// Extract trace information from Kafka message headers
	for _, header := range headers {
		carrier.Set(string(header.Key), string(header.Value))
	}
	
	// Use the global propagator to extract trace context
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// InjectTraceInfoToKafkaMessage injects trace information into a Kafka message
func InjectTraceInfoToKafkaMessage(ctx context.Context) []kafka.Header {
	// Create carrier to store headers to be injected
	carrier := propagation.MapCarrier{}
	
	// Inject current trace context into the carrier
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	
	// Convert carrier information to Kafka message headers
	var headers []kafka.Header
	for k, v := range carrier {
		headers = append(headers, kafka.Header{
			Key:   k,
			Value: []byte(v),
		})
	}
	
	return headers
}

// wrapHandlerWithTracing wraps Gin handlers to add more detailed tracing information for each handler
func wrapHandlerWithTracing(handler gin.HandlerFunc, spanName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get parent span (added by the otelgin middleware)
		ctx := c.Request.Context()
		ctx, span := tracer.Start(ctx, spanName)
		defer span.End()

		// Add request information as span attributes
		span.SetAttributes(
			attribute.String("http.method", c.Request.Method),
			attribute.String("http.route", c.FullPath()),
		)

		// Add span to context
		c.Request = c.Request.WithContext(ctx)

		// Capture potential panics
		defer func() {
			if err := recover(); err != nil {
				span.SetStatus(codes.Error, fmt.Sprintf("panic: %v", err))
				panic(err) // Re-panic so Gin's recovery middleware can handle it
			}
		}()

		// Call the original handler
		handler(c)

		// Record response status
		span.SetAttributes(attribute.Int("http.status_code", c.Writer.Status()))
		
		// If status code indicates an error, set span status to Error
		if c.Writer.Status() >= 400 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", c.Writer.Status()))
		} else {
			span.SetStatus(codes.Ok, "")
		}
	}
} 
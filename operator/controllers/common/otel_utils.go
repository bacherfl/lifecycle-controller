package common

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	logger       = ctrl.Log.WithName("otel-utils")
	gitCommit    string
	buildTime    string
	buildVersion string
	otelInitOnce sync.Once
)

type otelConfig struct {
	tracerProvider *trace.TracerProvider
}

// do not export this type to make it accessible only via the GetInstance method (i.e Singleton)
var otelInstance *otelConfig

func GetOtelInstance() *otelConfig {
	// initialize once
	otelInitOnce.Do(func() {
		otelInstance = &otelConfig{}
	})

	return otelInstance
}

func (o *otelConfig) InitOtelCollector(otelCollectorUrl string) error {
	tpOptions, err := GetOTelTracerProviderOptions(otelCollectorUrl)
	if err != nil {
		return err
	}

	o.tracerProvider = trace.NewTracerProvider(tpOptions...)
	otel.SetTracerProvider(o.tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return nil
}

func (o *otelConfig) ShutDown() {
	if err := o.tracerProvider.Shutdown(context.Background()); err != nil {
		os.Exit(1)
	}
}

func GetOTelTracerProviderOptions(oTelCollectorUrl string) ([]trace.TracerProviderOption, error) {
	var tracerProviderOptions []trace.TracerProviderOption

	stdOutExp, err := newStdOutExporter()
	if err != nil {
		return nil, fmt.Errorf("could not create stdout OTel exporter: %w", err)
	}
	tracerProviderOptions = append(tracerProviderOptions, trace.WithBatcher(stdOutExp))

	if oTelCollectorUrl != "" {
		// try to set OTel exporter for Jaeger
		otelExporter, err := newOTelExporter(oTelCollectorUrl)
		if err != nil {
			// log the error, but do not break if Jaeger exporter cannot be created
			logger.Error(err, "Could not set up OTel exporter")
		} else if otelExporter != nil {
			tracerProviderOptions = append(tracerProviderOptions, trace.WithBatcher(otelExporter))
		}
	}
	tracerProviderOptions = append(tracerProviderOptions, trace.WithResource(newResource()))

	return tracerProviderOptions, nil
}

func newStdOutExporter() (trace.SpanExporter, error) {
	return stdouttrace.New(
		// Use human readable output.
		stdouttrace.WithPrettyPrint(),
		// Do not print timestamps for the demo.
		stdouttrace.WithoutTimestamps(),
	)
}

func newOTelExporter(oTelCollectorUrl string) (trace.SpanExporter, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), 3*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, oTelCollectorUrl, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector at %s: %w", oTelCollectorUrl, err)
	}
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}
	return traceExporter, nil
}

func newResource() *resource.Resource {
	r := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.TelemetrySDKLanguageGo,
		semconv.ServiceNameKey.String("keptn-lifecycle-operator"),
		semconv.ServiceVersionKey.String(buildVersion+"-"+gitCommit+"-"+buildTime),
	)
	return r
}

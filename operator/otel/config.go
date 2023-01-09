package otel

import (
	"context"
	"fmt"
	"github.com/benbjohnson/clock"
	"github.com/go-logr/logr"
	"github.com/open-feature/go-sdk/pkg/openfeature"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"strings"
	"sync"
	"time"
)

type otelExporterConfigurator struct {
	tp               *trace.TracerProvider
	otelCollectorURL string
	ticker           *clock.Ticker
	logger           logr.Logger
}

var instance *otelExporterConfigurator
var once sync.Once

func StartOtelExporterConfigurator(ctx context.Context) {
	once.Do(func() {
		instance = &otelExporterConfigurator{
			logger: zap.New(),
			ticker: clock.New().Ticker(10 * time.Second),
		}
		instance.Start(ctx)
	})
}

func (oc *otelExporterConfigurator) ShutDown() error {
	return oc.tp.Shutdown(context.Background())
}

func (oc *otelExporterConfigurator) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				if err := oc.ShutDown(); err != nil {
					oc.logger.Error(err, "Error during otel shutdown")
				}
				return
			case <-oc.ticker.C:
				if err := oc.setup(); err != nil {
					oc.logger.Error(err, "Error during otel setup")
				}
			}
		}
	}()
}

func (oc *otelExporterConfigurator) setup() error {
	tpOptions, err := oc.getOTelTracerProviderOptions()
	if err != nil {
		return err
	}

	oc.tp = trace.NewTracerProvider(tpOptions...)

	otel.SetTracerProvider(oc.tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return nil
}

func (oc *otelExporterConfigurator) getOTelTracerProviderOptions() ([]trace.TracerProviderOption, error) {
	tracerProviderOptions := []trace.TracerProviderOption{}

	stdOutExp, err := newStdOutExporter()
	if err != nil {
		return nil, fmt.Errorf("could not create stdout OTel exporter: %w", err)
	}
	tracerProviderOptions = append(tracerProviderOptions, trace.WithBatcher(stdOutExp))

	client := openfeature.NewClient("klt")

	maxRetries := 3
	var otelCollectorURL string
	for i := 0; i < maxRetries; i++ {
		otelCollectorURL, err = client.StringValue(context.TODO(), "otel-collector-url", "", openfeature.EvaluationContext{})
		if err == nil {
			break
		}

		if strings.Contains(err.Error(), string(openfeature.ProviderNotReadyCode)) {
			<-time.After(2 * time.Second)
			continue
		}
		break
	}

	if err != nil {
		oc.logger.Error(err, "Could not get otel-collector-url from OpenFeature")
	}

	if otelCollectorURL != "" && otelCollectorURL != oc.otelCollectorURL {
		oc.otelCollectorURL = otelCollectorURL
		oc.logger.Info("Got new otel-collector-url", "otel-collector-url", otelCollectorURL, "previous", oc.otelCollectorURL)
		// try to set OTel exporter for Jaeger
		otelExporter, err := newOTelExporter(otelCollectorURL)
		if err != nil {
			// log the error, but do not break if Jaeger exporter cannot be created
			oc.logger.Error(err, "Could not set up OTel exporter")
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

func newOTelExporter(collectorURL string) (trace.SpanExporter, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), 3*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, collectorURL, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector at %s: %w", collectorURL, err)
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
	)
	return r
}

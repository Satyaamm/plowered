// Package obs is the observability layer: OpenTelemetry meter provider
// bridged to a Prometheus registry, named instruments for the domain, and
// the `/metrics` HTTP handler. One Metrics value is constructed at boot
// and shared across the API process.
package obs

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Metrics is the bag of named instruments we record from across the
// codebase. Construct once via NewMetrics; share by reference.
type Metrics struct {
	// Provider holds the OTel meter provider so callers can shut it down
	// during graceful exit.
	Provider *sdkmetric.MeterProvider
	// Registry is the Prometheus registry that backs /metrics.
	Registry *prometheus.Registry

	HTTPRequestDuration metric.Float64Histogram
	HTTPRequestsTotal   metric.Int64Counter

	PipelineRunsTotal     metric.Int64Counter
	TaskRunsTotal         metric.Int64Counter
	CheckRunsTotal        metric.Int64Counter
	NotificationsTotal    metric.Int64Counter
}

// NewMetrics wires an OTel SDK meter provider to a fresh Prometheus
// registry, registers Go runtime + process collectors, and pre-creates
// every instrument the codebase reads.
func NewMetrics() (*Metrics, error) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	exporter, err := otelprom.New(otelprom.WithRegisterer(reg))
	if err != nil {
		return nil, fmt.Errorf("obs: prometheus exporter: %w", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)
	meter := provider.Meter("github.com/Satyaamm/plowered")

	m := &Metrics{Provider: provider, Registry: reg}

	if m.HTTPRequestDuration, err = meter.Float64Histogram(
		"plowered_http_request_duration_seconds",
		metric.WithDescription("Duration of HTTP requests, by route and status."),
		metric.WithUnit("s"),
	); err != nil {
		return nil, err
	}
	if m.HTTPRequestsTotal, err = meter.Int64Counter(
		"plowered_http_requests_total",
		metric.WithDescription("HTTP requests served, by route and status."),
	); err != nil {
		return nil, err
	}
	if m.PipelineRunsTotal, err = meter.Int64Counter(
		"plowered_pipeline_runs_total",
		metric.WithDescription("Pipeline runs, by terminal status."),
	); err != nil {
		return nil, err
	}
	if m.TaskRunsTotal, err = meter.Int64Counter(
		"plowered_task_runs_total",
		metric.WithDescription("Task runs, by terminal status."),
	); err != nil {
		return nil, err
	}
	if m.CheckRunsTotal, err = meter.Int64Counter(
		"plowered_check_runs_total",
		metric.WithDescription("Quality check runs, by outcome."),
	); err != nil {
		return nil, err
	}
	if m.NotificationsTotal, err = meter.Int64Counter(
		"plowered_notifications_total",
		metric.WithDescription("Notification deliveries, by status."),
	); err != nil {
		return nil, err
	}
	return m, nil
}

// Handler serves the registry over HTTP. Mount on `/metrics`.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

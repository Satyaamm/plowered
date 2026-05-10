package obs

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/Satyaamm/plowered/internal/core/events"
)

// HTTPMiddleware records request duration + count for every request,
// labeled by method, route pattern (the pattern from net/http's
// ServeMux — fixed cardinality), and status class.
//
// This wraps an http.Handler so it composes with the existing chain in
// internal/api/http/middleware.go.
func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := r.Pattern
		if route == "" {
			route = "unknown"
		}
		attrs := metric.WithAttributes(
			attribute.String("method", r.Method),
			attribute.String("route", route),
			attribute.String("status", statusClass(rec.status)),
		)
		m.HTTPRequestDuration.Record(r.Context(), time.Since(start).Seconds(), attrs)
		m.HTTPRequestsTotal.Add(r.Context(), 1, attrs)
	})
}

// statusClass maps HTTP statuses to "1xx".."5xx" buckets so cardinality
// stays bounded.
func statusClass(s int) string {
	switch {
	case s < 200:
		return "1xx"
	case s < 300:
		return "2xx"
	case s < 400:
		return "3xx"
	case s < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush delegates so SSE handlers can flush through the wrapper.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// EventRecorder is an events.Subscriber that turns domain events into
// counter increments. Subscribe it to the bus once at boot.
type EventRecorder struct{ M *Metrics }

func (r EventRecorder) OnEvent(ctx context.Context, e events.Event) {
	if r.M == nil {
		return
	}
	switch e.Type {
	case events.RunSucceeded, events.RunFailed, events.RunCancelled:
		r.M.PipelineRunsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", string(e.Type)),
			attribute.String("tenant_id", e.TenantID),
		))
	case events.TaskSucceeded, events.TaskFailed, events.TaskSkipped:
		r.M.TaskRunsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", string(e.Type)),
			attribute.String("tenant_id", e.TenantID),
		))
	case events.CheckPassed, events.CheckFailed:
		r.M.CheckRunsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("outcome", string(e.Type)),
			attribute.String("tenant_id", e.TenantID),
		))
	case events.NotificationDelivered, events.NotificationFailed:
		r.M.NotificationsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", string(e.Type)),
			attribute.String("tenant_id", e.TenantID),
		))
	}
}


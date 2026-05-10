package obs_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Satyaamm/plowered/internal/core/events"
	"github.com/Satyaamm/plowered/internal/obs"
)

func TestMetricsHandlerExposesGoRuntime(t *testing.T) {
	m, err := obs.NewMetrics()
	if err != nil {
		t.Fatalf("new metrics: %v", err)
	}
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	// Go runtime collector is registered by NewMetrics — sanity check it's
	// flowing through.
	if !strings.Contains(text, "go_goroutines") {
		t.Errorf("expected go_goroutines in /metrics output")
	}
}

func TestEventRecorderIncrementsPipelineCounter(t *testing.T) {
	m, err := obs.NewMetrics()
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewMemoryBus()
	bus.Subscribe(obs.EventRecorder{M: m})

	ctx := context.Background()
	bus.Publish(ctx, events.Event{Type: events.RunSucceeded, TenantID: "t1"})
	bus.Publish(ctx, events.Event{Type: events.RunFailed, TenantID: "t1"})
	bus.Publish(ctx, events.Event{Type: events.CheckPassed, TenantID: "t1"})
	bus.Publish(ctx, events.Event{Type: events.NotificationDelivered, TenantID: "t1"})

	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	resp, _ := http.Get(srv.URL)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	text := string(body)

	for _, want := range []string{
		`plowered_pipeline_runs_total`,
		`plowered_check_runs_total`,
		`plowered_notifications_total`,
		`status="run.succeeded"`,
		`status="run.failed"`,
		`outcome="check.passed"`,
		`status="notification.delivered"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in /metrics output", want)
		}
	}
}

func TestHTTPMiddlewareRecordsDuration(t *testing.T) {
	m, err := obs.NewMetrics()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/things", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	srv := httptest.NewServer(m.HTTPMiddleware(mux))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/things")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	metricsSrv := httptest.NewServer(m.Handler())
	defer metricsSrv.Close()
	mResp, _ := http.Get(metricsSrv.URL)
	body, _ := io.ReadAll(mResp.Body)
	mResp.Body.Close()
	text := string(body)

	if !strings.Contains(text, `plowered_http_requests_total`) {
		t.Error("missing plowered_http_requests_total")
	}
	if !strings.Contains(text, `route="GET /v1/things"`) {
		t.Errorf("missing route label, got:\n%s", text)
	}
	if !strings.Contains(text, `status="4xx"`) {
		t.Errorf("missing 4xx status class for 418 response")
	}
	if !strings.Contains(text, `plowered_http_request_duration_seconds_count`) {
		t.Error("missing duration histogram count series")
	}
}

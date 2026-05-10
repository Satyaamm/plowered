// Package e2e exercises the full request → enqueue → worker → persist
// path end-to-end against the in-memory backend. The test is deliberately
// hermetic — no Postgres or Redis required — so it runs in every CI green
// path without flake risk. A separate live-Postgres test lives in
// internal/scheduler/integration_test.go for the production wiring.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apihttp "github.com/Satyaamm/plowered/internal/api/http"
	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/events"
	"github.com/Satyaamm/plowered/internal/core/notify"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/obs"
	"github.com/Satyaamm/plowered/internal/storage/memory"
	"github.com/Satyaamm/plowered/internal/worker"
)

// noopExecutor satisfies pipeline.Executor for the single SQL task we
// schedule in the smoke test. Returns success immediately.
type noopExecutor struct{}

func (noopExecutor) Type() pipeline.TaskType { return pipeline.TaskTypeSQL }
func (noopExecutor) Execute(_ context.Context, _ pipeline.ExecutionContext) (pipeline.Output, error) {
	return pipeline.Output{Properties: map[string]any{"rows": 1}}, nil
}

// fakeDS satisfies quality.DataSource. The smoke test runs a row_count
// check which expects min=1; we return 5 so it passes.
type fakeDS struct{}

func (fakeDS) QueryScalar(_ context.Context, _ string, _ ...any) (any, error) {
	return int64(5), nil
}
func (fakeDS) QueryTime(_ context.Context, _ string, _ ...any) (time.Time, error) {
	return time.Now(), nil
}

func TestEndToEndSmoke(t *testing.T) {
	// ── 1. Build every store + dependency the way main.go does, just
	//      using the memory adapters. Bus is shared so the metrics
	//      recorder sees runner events.
	pStore := pipeline.NewMemoryStore()
	qStore := quality.NewMemoryStore()
	nStore := notify.NewMemoryStore()
	polStore := policy.NewMemoryRuleStore()
	auditStore := audit.NewMemoryWriter()
	bus := events.NewMemoryBus()

	metrics, err := obs.NewMetrics()
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	bus.Subscribe(obs.EventRecorder{M: metrics})

	registry := pipeline.NewRegistry()
	registry.MustRegister(noopExecutor{})

	runner := &pipeline.Runner{
		Store:    pStore,
		Registry: registry,
		Events:   bus,
		Now:      time.Now,
	}
	scheduler := quality.NewScheduler(quality.NewRunner(), quality.SchedulerConfig{})

	handlers := &worker.Handlers{
		Pipelines: pStore,
		Quality:   qStore,
		Scheduler: scheduler,
		Runner:    runner,
		Resolver: func(_ context.Context, _, _ string) (quality.DataSource, error) {
			return fakeDS{}, nil
		},
		Events: bus,
	}
	enq := worker.NewSyncEnqueuer(handlers)

	// ── 2. Stand up the same HTTP chain main.go builds.
	mux := apihttp.NewMux(apihttp.Deps{
		Catalog:   memory.New(),
		Pipelines: pStore,
		Quality:   qStore,
		Notify:    nStore,
		Policies:  polStore,
		Audit:     auditStore,
		Enqueuer:  enq,
	})
	chain := apihttp.Chain(mux,
		apihttp.RecoveryMW(nil),
		apihttp.RequestIDMW(),
		apihttp.AuthMW(func(_ string) (auth.Principal, error) {
			return auth.Principal{ID: "u1", TenantID: "t1", Roles: []string{"admin"}}, nil
		}),
		apihttp.TenantMW(),
	)
	srv := httptest.NewServer(metrics.HTTPMiddleware(chain))
	defer srv.Close()

	// ── 3. Pipeline trigger flow.
	var pipelineID string
	{
		body, _ := json.Marshal(pipeline.Pipeline{
			Name: "smoke",
			Tasks: []pipeline.Task{
				{ID: "step1", Type: pipeline.TaskTypeSQL, Config: map[string]any{"sql": "select 1"}},
			},
		})
		resp := httpDo(t, srv.URL+"/v1/pipelines", "POST", body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create pipeline: status=%d", resp.StatusCode)
		}
		var got pipeline.Pipeline
		_ = json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		pipelineID = got.ID

		trig := httpDo(t, srv.URL+"/v1/pipelines/"+pipelineID+"/trigger", "POST", nil)
		if trig.StatusCode != http.StatusAccepted {
			t.Fatalf("trigger: status=%d", trig.StatusCode)
		}
		trig.Body.Close()

		// SyncEnqueuer fires a goroutine; poll until the run reaches a
		// terminal state.
		runID := waitForRunTerminal(t, pStore, pipelineID)
		run, _ := pStore.GetRun(context.Background(), runID)
		if run.Status != pipeline.RunSucceeded {
			t.Fatalf("expected run succeeded, got %s", run.Status)
		}
	}

	// ── 4. Quality check trigger flow.
	var checkID string
	{
		body, _ := json.Marshal(quality.Check{
			Name:    "rc",
			Type:    quality.CheckRowCount,
			AssetID: "asset-1",
			Config:  map[string]any{"table": "orders", "min": 1.0},
		})
		resp := httpDo(t, srv.URL+"/v1/checks", "POST", body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create check: status=%d", resp.StatusCode)
		}
		var got quality.Check
		_ = json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		checkID = got.ID

		trig := httpDo(t, srv.URL+"/v1/checks/"+checkID+"/run", "POST", []byte(`{}`))
		if trig.StatusCode != http.StatusAccepted {
			t.Fatalf("trigger check: status=%d", trig.StatusCode)
		}
		trig.Body.Close()

		// Wait for the worker to record a CheckRun.
		deadline := time.Now().Add(2 * time.Second)
		var runs []*quality.CheckRun
		for time.Now().Before(deadline) {
			runs, _ = qStore.ListRuns(context.Background(), "t1", checkID, 10)
			if len(runs) > 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if len(runs) != 1 {
			t.Fatalf("expected 1 check run, got %d", len(runs))
		}
		if runs[0].Outcome != quality.OutcomePass || runs[0].Value != 5 {
			t.Errorf("unexpected check run: %+v", runs[0])
		}
	}

	// ── 5. /metrics scrape — every counter we touched should be present.
	mResp, err := http.Get(srv.URL[:0]) // dummy line for clarity below
	_ = mResp
	_ = err
	m := httptest.NewServer(metrics.Handler())
	defer m.Close()
	resp, err := http.Get(m.URL)
	if err != nil {
		t.Fatalf("metrics scrape: %v", err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	scraped := buf.String()
	for _, want := range []string{
		"plowered_pipeline_runs_total",
		"plowered_check_runs_total",
		"plowered_http_requests_total",
		`status="run.succeeded"`,
	} {
		if !strings.Contains(scraped, want) {
			t.Errorf("expected %q in /metrics output", want)
		}
	}

	// ── 6. Audit feed — a Postgres impl would have rows by now; the
	//      memory writer is empty unless explicitly emitted, but the API
	//      still serves a valid response.
	resp = httpDo(t, srv.URL+"/v1/audit", "GET", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("audit list: status=%d", resp.StatusCode)
	}
	resp.Body.Close()
}

// waitForRunTerminal polls until a run for pipelineID reaches a terminal
// state, returning the run id. Fails the test on timeout.
func waitForRunTerminal(t *testing.T, store *pipeline.MemoryStore, pipelineID string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ := store.ListRuns(context.Background(), "t1", pipelineID, 10)
		for _, r := range runs {
			switch r.Status {
			case pipeline.RunSucceeded, pipeline.RunFailed, pipeline.RunCancelled:
				return r.ID
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no terminal run within deadline")
	return ""
}

func httpDo(t *testing.T, url, method string, body []byte) *http.Response {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("Authorization", "Bearer dev")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	apihttp "github.com/Satyaamm/plowered/internal/api/http"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/notify"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/storage/memory"
	"github.com/Satyaamm/plowered/internal/worker"
)

// newOrchServer builds a server with all v2 stores wired in. The principal
// returned by AuthMW is configurable so tests can flip tenants.
func newOrchServer(t *testing.T, tenant string) (*httptest.Server, apihttp.Deps) {
	t.Helper()
	deps := apihttp.Deps{
		Catalog:   memory.New(),
		Pipelines: pipeline.NewMemoryStore(),
		Quality:   quality.NewMemoryStore(),
		Notify:    notify.NewMemoryStore(),
		Policies:  policy.NewMemoryRuleStore(),
	}
	mw := []apihttp.Middleware{
		apihttp.RecoveryMW(nil),
		apihttp.RequestIDMW(),
		apihttp.AuthMW(func(_ string) (auth.Principal, error) {
			return auth.Principal{ID: "u1", TenantID: tenant, Roles: []string{"admin"}}, nil
		}, "/healthz"),
		apihttp.TenantMW("/healthz"),
	}
	h := apihttp.Chain(apihttp.NewMux(deps), mw...)
	return httptest.NewServer(h), deps
}

func do(t *testing.T, s *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r, _ := http.NewRequest(method, s.URL+path, rd)
	r.Header.Set("Authorization", "Bearer fake")
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestPipelineCreateTriggerAndList(t *testing.T) {
	s, _ := newOrchServer(t, "t1")
	defer s.Close()

	create := pipeline.Pipeline{
		Name: "nightly",
		Tasks: []pipeline.Task{
			{ID: "extract", Type: pipeline.TaskTypeSQL, Config: map[string]any{"sql": "select 1"}},
			{ID: "load", Type: pipeline.TaskTypeSQL, DependsOn: []string{"extract"}, Config: map[string]any{"sql": "select 1"}},
		},
	}
	resp := do(t, s, "POST", "/v1/pipelines", create)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d", resp.StatusCode)
	}
	var p pipeline.Pipeline
	_ = json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()
	if p.ID == "" || p.TenantID != "t1" {
		t.Fatalf("returned pipeline = %+v", p)
	}

	resp2 := do(t, s, "POST", "/v1/pipelines/"+p.ID+"/trigger", nil)
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("trigger status=%d", resp2.StatusCode)
	}
	var run pipeline.Run
	_ = json.NewDecoder(resp2.Body).Decode(&run)
	resp2.Body.Close()
	if run.PipelineID != p.ID || run.Status != pipeline.RunQueued {
		t.Errorf("run = %+v", run)
	}

	resp3 := do(t, s, "GET", "/v1/runs", nil)
	defer resp3.Body.Close()
	var rl struct {
		Runs []pipeline.Run `json:"runs"`
	}
	_ = json.NewDecoder(resp3.Body).Decode(&rl)
	if len(rl.Runs) != 1 {
		t.Errorf("runs = %d", len(rl.Runs))
	}
}

func TestPipelineCycleRejectedAtCreate(t *testing.T) {
	s, _ := newOrchServer(t, "t1")
	defer s.Close()

	bad := pipeline.Pipeline{
		Name: "cycle",
		Tasks: []pipeline.Task{
			{ID: "a", Type: pipeline.TaskTypeSQL, DependsOn: []string{"b"}},
			{ID: "b", Type: pipeline.TaskTypeSQL, DependsOn: []string{"a"}},
		},
	}
	resp := do(t, s, "POST", "/v1/pipelines", bad)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", resp.StatusCode)
	}
}

func TestPipelineCrossTenantIsolation(t *testing.T) {
	// Create as t1, attempt read as t2.
	s1, deps := newOrchServer(t, "t1")
	defer s1.Close()
	resp := do(t, s1, "POST", "/v1/pipelines", pipeline.Pipeline{Name: "p"})
	var p pipeline.Pipeline
	_ = json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()

	// Spin up a fresh server using the *same* stores but a different tenant
	// principal, simulating t2 reaching at the API level.
	mw := []apihttp.Middleware{
		apihttp.RecoveryMW(nil),
		apihttp.RequestIDMW(),
		apihttp.AuthMW(func(_ string) (auth.Principal, error) {
			return auth.Principal{ID: "u2", TenantID: "t2", Roles: []string{"admin"}}, nil
		}),
		apihttp.TenantMW(),
	}
	s2 := httptest.NewServer(apihttp.Chain(apihttp.NewMux(deps), mw...))
	defer s2.Close()

	resp2 := do(t, s2, "GET", "/v1/pipelines/"+p.ID, nil)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("cross-tenant get status=%d, want 404", resp2.StatusCode)
	}
}

func TestCheckCreateAndListRuns(t *testing.T) {
	s, deps := newOrchServer(t, "t1")
	defer s.Close()

	c := quality.Check{
		Name:    "row count",
		Type:    quality.CheckRowCount,
		AssetID: "asset-1",
		Config:  map[string]any{"table": "orders", "min": 1.0},
	}
	resp := do(t, s, "POST", "/v1/checks", c)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d", resp.StatusCode)
	}
	var got quality.Check
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()

	// Persist a fake run directly through the store and verify the API
	// surfaces it.
	_, _ = deps.Quality.RecordRun(context.Background(), &quality.CheckRun{
		TenantID: "t1", CheckID: got.ID, AssetID: got.AssetID,
		Outcome: quality.OutcomePass, Value: 5,
	})

	resp2 := do(t, s, "GET", "/v1/checks/"+got.ID+"/runs", nil)
	defer resp2.Body.Close()
	var rl struct {
		Runs []quality.CheckRun `json:"runs"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&rl)
	if len(rl.Runs) != 1 {
		t.Errorf("runs = %d", len(rl.Runs))
	}
}

func TestCheckRequiresName(t *testing.T) {
	s, _ := newOrchServer(t, "t1")
	defer s.Close()
	resp := do(t, s, "POST", "/v1/checks", quality.Check{Type: quality.CheckRowCount})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", resp.StatusCode)
	}
}

func TestNotifyChannelAndRule(t *testing.T) {
	s, _ := newOrchServer(t, "t1")
	defer s.Close()

	resp := do(t, s, "POST", "/v1/notifications/channels", notify.ChannelConfig{
		Kind: "log", Name: "audit-log",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("channel status=%d", resp.StatusCode)
	}
	var ch notify.ChannelConfig
	_ = json.NewDecoder(resp.Body).Decode(&ch)
	resp.Body.Close()

	resp2 := do(t, s, "POST", "/v1/notifications/rules", notify.Rule{
		ChannelID: ch.ID, Enabled: true,
	})
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("rule status=%d", resp2.StatusCode)
	}
	resp2.Body.Close()

	resp3 := do(t, s, "GET", "/v1/notifications/rules", nil)
	defer resp3.Body.Close()
	var rl struct {
		Rules []notify.Rule `json:"rules"`
	}
	_ = json.NewDecoder(resp3.Body).Decode(&rl)
	if len(rl.Rules) != 1 {
		t.Errorf("rules = %d", len(rl.Rules))
	}
}

func TestPolicyCreateAndList(t *testing.T) {
	s, _ := newOrchServer(t, "t1")
	defer s.Close()

	resp := do(t, s, "POST", "/v1/policies", policy.Rule{
		Effect: policy.EffectDeny,
		Verbs:  []policy.Verb{policy.VerbDelete},
		Conditions: []policy.Condition{
			{Type: policy.CondResourceTag, Value: "class:pii"},
		},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	resp2 := do(t, s, "GET", "/v1/policies", nil)
	defer resp2.Body.Close()
	var rl struct {
		Rules []policy.Rule `json:"rules"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&rl)
	if len(rl.Rules) != 1 || rl.Rules[0].Effect != policy.EffectDeny {
		t.Errorf("rules = %+v", rl.Rules)
	}
}

func TestPolicyRejectsEmptyEffect(t *testing.T) {
	s, _ := newOrchServer(t, "t1")
	defer s.Close()
	resp := do(t, s, "POST", "/v1/policies", policy.Rule{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", resp.StatusCode)
	}
}

// countingEnqueuer counts EnqueuePipelineRun + EnqueueQualityRun calls so
// tests can assert the trigger paths actually dispatched.
type countingEnqueuer struct{ p, q atomic.Int32 }

func (c *countingEnqueuer) EnqueuePipelineRun(_ context.Context, _ worker.PipelineRunPayload) error {
	c.p.Add(1)
	return nil
}
func (c *countingEnqueuer) EnqueueQualityRun(_ context.Context, _ worker.QualityRunPayload) error {
	c.q.Add(1)
	return nil
}

// The orchestration tests only care about pipeline + quality dispatch;
// stub the rest of the Enqueuer surface so the double still satisfies
// the interface after it grew.
func (*countingEnqueuer) EnqueueCrawlConnection(context.Context, worker.CrawlConnectionPayload) error {
	return nil
}
func (*countingEnqueuer) EnqueueClassifyConnection(context.Context, worker.ClassifyConnectionPayload) error {
	return nil
}
func (*countingEnqueuer) EnqueueSearchReindex(context.Context, worker.SearchReindexPayload) error {
	return nil
}

func TestTriggerEnqueuesPipelineJob(t *testing.T) {
	cnt := &countingEnqueuer{}
	deps := apihttp.Deps{
		Catalog:   memory.New(),
		Pipelines: pipeline.NewMemoryStore(),
		Quality:   quality.NewMemoryStore(),
		Notify:    notify.NewMemoryStore(),
		Policies:  policy.NewMemoryRuleStore(),
		Enqueuer:  cnt,
	}
	mw := []apihttp.Middleware{
		apihttp.RecoveryMW(nil), apihttp.RequestIDMW(),
		apihttp.AuthMW(func(_ string) (auth.Principal, error) {
			return auth.Principal{ID: "u1", TenantID: "t1", Roles: []string{"admin"}}, nil
		}),
		apihttp.TenantMW(),
	}
	s := httptest.NewServer(apihttp.Chain(apihttp.NewMux(deps), mw...))
	defer s.Close()

	// Create the pipeline so the trigger has something to enqueue.
	resp := do(t, s, "POST", "/v1/pipelines", pipeline.Pipeline{Name: "p"})
	var p pipeline.Pipeline
	_ = json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()

	resp2 := do(t, s, "POST", "/v1/pipelines/"+p.ID+"/trigger", nil)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("trigger status=%d", resp2.StatusCode)
	}
	if cnt.p.Load() != 1 {
		t.Errorf("pipeline enqueue count=%d, want 1", cnt.p.Load())
	}
}

func TestRunCheckEnqueuesQualityJob(t *testing.T) {
	cnt := &countingEnqueuer{}
	deps := apihttp.Deps{
		Catalog:   memory.New(),
		Pipelines: pipeline.NewMemoryStore(),
		Quality:   quality.NewMemoryStore(),
		Notify:    notify.NewMemoryStore(),
		Policies:  policy.NewMemoryRuleStore(),
		Enqueuer:  cnt,
	}
	mw := []apihttp.Middleware{
		apihttp.RecoveryMW(nil), apihttp.RequestIDMW(),
		apihttp.AuthMW(func(_ string) (auth.Principal, error) {
			return auth.Principal{ID: "u1", TenantID: "t1", Roles: []string{"admin"}}, nil
		}),
		apihttp.TenantMW(),
	}
	s := httptest.NewServer(apihttp.Chain(apihttp.NewMux(deps), mw...))
	defer s.Close()

	resp := do(t, s, "POST", "/v1/checks", quality.Check{
		Name: "rc", Type: quality.CheckRowCount, AssetID: "a1",
		Config: map[string]any{"table": "orders"},
	})
	var c quality.Check
	_ = json.NewDecoder(resp.Body).Decode(&c)
	resp.Body.Close()

	resp2 := do(t, s, "POST", "/v1/checks/"+c.ID+"/run", map[string]any{
		"source_id": "src-1", "timeout_sec": 30,
	})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("run status=%d", resp2.StatusCode)
	}
	// Give any goroutines a moment (sync enqueuer would fire here).
	time.Sleep(10 * time.Millisecond)
	if cnt.q.Load() != 1 {
		t.Errorf("quality enqueue count=%d, want 1", cnt.q.Load())
	}
}

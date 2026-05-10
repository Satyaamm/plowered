// Package worker is the async job runner. The HTTP layer enqueues jobs;
// workers consume them off Redis (Asynq) and execute the long-running work
// out-of-band so the request path stays fast.
//
// Two task types in v0:
//
//	pipeline:run  — drives a Pipeline Run to completion via pipeline.Runner.
//	quality:run   — evaluates a Check via quality.Scheduler.
//
// All payloads carry tenant_id explicitly. Workers re-attach it to ctx via
// storage.WithTenant before touching any store.
package worker

import (
	"context"
	"encoding/json"
)

// Task type identifiers — strings are the wire format both Asynq and the
// in-process sync enqueuer use. Treat them as stable.
const (
	TaskPipelineRun        = "pipeline:run"
	TaskQualityRun         = "quality:run"
	TaskCrawlConnection    = "crawler:connection"
	TaskClassifyConnection = "classify:connection"
	TaskSearchReindex      = "search:reindex"
)

// PipelineRunPayload is the body of a TaskPipelineRun job.
type PipelineRunPayload struct {
	TenantID string `json:"tenant_id"`
	RunID    string `json:"run_id"`
}

// QualityRunPayload is the body of a TaskQualityRun job. SourceID names the
// per-tenant datasource the worker should resolve to a real
// quality.DataSource. SamplePercent and Timeout override scheduler defaults.
type QualityRunPayload struct {
	TenantID      string  `json:"tenant_id"`
	CheckID       string  `json:"check_id"`
	SourceID      string  `json:"source_id"`
	SamplePercent float64 `json:"sample_percent,omitempty"`
	TimeoutSec    int     `json:"timeout_sec,omitempty"`
}

// CrawlConnectionPayload is the body of a TaskCrawlConnection job. It
// names the connection to walk; the worker resolves credentials via the
// secrets vault and dispatches to the per-Type adapter Source.
type CrawlConnectionPayload struct {
	TenantID     string `json:"tenant_id"`
	ConnectionID string `json:"connection_id"`
	Actor        string `json:"actor,omitempty"` // user_id of whoever pressed Crawl
}

// ClassifyConnectionPayload drives a sample-based classification run.
// JobID is the visible-to-UI handle; the worker writes progress against
// this row in the jobs table.
type ClassifyConnectionPayload struct {
	TenantID     string `json:"tenant_id"`
	ConnectionID string `json:"connection_id"`
	Actor        string `json:"actor,omitempty"`
	JobID        string `json:"job_id"`
}

// SearchReindexPayload re-embeds every asset in a tenant. Same job-id
// tracking pattern as ClassifyConnectionPayload.
type SearchReindexPayload struct {
	TenantID string `json:"tenant_id"`
	Actor    string `json:"actor,omitempty"`
	JobID    string `json:"job_id"`
}

// Enqueuer is the surface the API layer needs to dispatch async work. The
// concrete impl is either AsynqEnqueuer (Redis-backed, prod), SyncEnqueuer
// (runs inline, useful for embedded mode + tests), or NoopEnqueuer (drops
// jobs silently — only for unit tests that don't care about the worker
// path).
type Enqueuer interface {
	EnqueuePipelineRun(ctx context.Context, payload PipelineRunPayload) error
	EnqueueQualityRun(ctx context.Context, payload QualityRunPayload) error
	EnqueueCrawlConnection(ctx context.Context, payload CrawlConnectionPayload) error
	EnqueueClassifyConnection(ctx context.Context, payload ClassifyConnectionPayload) error
	EnqueueSearchReindex(ctx context.Context, payload SearchReindexPayload) error
}

// NoopEnqueuer drops every job. Useful when a test exercises the HTTP
// surface but isn't asserting on the worker path.
type NoopEnqueuer struct{}

func (NoopEnqueuer) EnqueuePipelineRun(context.Context, PipelineRunPayload) error    { return nil }
func (NoopEnqueuer) EnqueueQualityRun(context.Context, QualityRunPayload) error      { return nil }
func (NoopEnqueuer) EnqueueCrawlConnection(context.Context, CrawlConnectionPayload) error {
	return nil
}
func (NoopEnqueuer) EnqueueClassifyConnection(context.Context, ClassifyConnectionPayload) error {
	return nil
}
func (NoopEnqueuer) EnqueueSearchReindex(context.Context, SearchReindexPayload) error { return nil }

// marshal returns the JSON encoding of v. The error is folded back into the
// caller — Asynq treats it as a non-retryable failure.
func marshal(v any) ([]byte, error) { return json.Marshal(v) }

package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/Satyaamm/plowered/internal/core/classifier"
	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/crawler"
	"github.com/Satyaamm/plowered/internal/core/events"
	"github.com/Satyaamm/plowered/internal/core/jobs"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/core/search"
	"github.com/Satyaamm/plowered/internal/core/secrets"
	"github.com/Satyaamm/plowered/internal/storage"
)

// SourceRegistry pairs each connection.Type with a crawler.Source. Same
// pattern as connection.Registry but for the schema-walker side. The
// worker uses this to dispatch a crawl job by Type.
type SourceRegistry struct {
	sources map[connection.Type]crawler.Source
}

func NewSourceRegistry() *SourceRegistry {
	return &SourceRegistry{sources: map[connection.Type]crawler.Source{}}
}

func (r *SourceRegistry) Register(t connection.Type, s crawler.Source) {
	r.sources[t] = s
}

func (r *SourceRegistry) Source(t connection.Type) (crawler.Source, bool) {
	s, ok := r.sources[t]
	return s, ok
}

// DataSourceResolver returns a quality.DataSource for the given (tenant,
// source) pair. Implementations look up per-tenant connection configs
// (Snowflake, BigQuery, Postgres, …) and construct a typed driver.
//
// For embedded / single-tenant deployments the resolver may always return
// the same source. For multi-tenant SaaS, source IDs key into a vault.
type DataSourceResolver func(ctx context.Context, tenantID, sourceID string) (quality.DataSource, error)

// Handlers bundles the dependencies a worker needs to execute jobs. Both
// the in-process SyncEnqueuer and the Asynq-backed server use this same
// shape so behavior is identical regardless of transport.
type Handlers struct {
	Logger     *slog.Logger
	Pipelines  pipeline.Repo
	Quality    quality.Store
	Scheduler  *quality.Scheduler
	Runner     *pipeline.Runner
	Resolver   DataSourceResolver
	// Crawl wiring — populated when the worker should accept
	// TaskCrawlConnection jobs.
	Catalog        storage.Store
	Connections    connection.Repo
	Vault          secrets.Vault
	SourceRegistry *SourceRegistry
	// Events is optional. When set, the worker publishes CheckPassed /
	// CheckFailed lifecycle events so the metrics recorder + notify
	// dispatcher can react.
	Events events.Bus
	// Jobs is the durable visibility layer for classify + reindex. When
	// set, the corresponding handlers update Start/Progress/Succeed/Fail
	// against this repo so the UI can poll job status.
	Jobs jobs.Repo
	// Classifier orchestrates sample-based classification of a customer
	// connection. Required by HandleClassifyConnection.
	Classifier *classifier.Orchestrator
	// SearchIndexer re-embeds every asset for the tenant. Required by
	// HandleSearchReindex.
	SearchIndexer *search.Indexer
}

func (h *Handlers) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

// HandlePipelineRun resolves the run ID, attaches tenant to ctx, and asks
// the runner to drive it to completion. Errors are returned so the queue
// adapter can apply its retry policy; nil means "done, do not retry."
func (h *Handlers) HandlePipelineRun(ctx context.Context, raw []byte) error {
	var p PipelineRunPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("worker: bad pipeline payload: %w", err)
	}
	if p.TenantID == "" || p.RunID == "" {
		return fmt.Errorf("worker: pipeline payload missing tenant or run id")
	}
	if h.Runner == nil {
		return fmt.Errorf("worker: no pipeline runner configured")
	}
	ctx = storage.WithTenant(ctx, p.TenantID)
	status, err := h.Runner.RunOnce(ctx, p.RunID)
	if err != nil {
		h.logger().ErrorContext(ctx, "pipeline run failed",
			"tenant", p.TenantID, "run", p.RunID, "err", err)
		return err
	}
	h.logger().InfoContext(ctx, "pipeline run finished",
		"tenant", p.TenantID, "run", p.RunID, "status", status)
	return nil
}

// HandleQualityRun loads the check, runs it via the scheduler, and persists
// the resulting CheckRun. Errors evaluating the check do NOT bubble out as
// queue retries — they're recorded as OutcomeError on the CheckRun and the
// job is marked done. Only system-level errors (storage unavailable, etc.)
// return a Go error and trigger Asynq retry.
func (h *Handlers) HandleQualityRun(ctx context.Context, raw []byte) error {
	var p QualityRunPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("worker: bad quality payload: %w", err)
	}
	if p.TenantID == "" || p.CheckID == "" {
		return fmt.Errorf("worker: quality payload missing tenant or check id")
	}
	if h.Scheduler == nil || h.Quality == nil || h.Resolver == nil {
		return fmt.Errorf("worker: quality dependencies not configured")
	}

	ctx = storage.WithTenant(ctx, p.TenantID)

	check, err := h.Quality.GetCheck(ctx, p.CheckID)
	if err != nil {
		return fmt.Errorf("worker: load check %q: %w", p.CheckID, err)
	}

	ds, err := h.Resolver(ctx, p.TenantID, p.SourceID)
	if err != nil {
		// Persist a CheckRun with OutcomeError so operators see the failure
		// in the UI; do not retry.
		_, _ = h.Quality.RecordRun(ctx, &quality.CheckRun{
			TenantID: p.TenantID, CheckID: check.ID, AssetID: check.AssetID,
			Outcome: quality.OutcomeError, Severity: check.Severity,
			Diagnostic: fmt.Sprintf("resolve datasource %q: %v", p.SourceID, err),
		})
		return nil
	}

	opts := quality.RunOptions{Source: p.TenantID + ":" + p.SourceID, SamplePercent: p.SamplePercent}
	if p.TimeoutSec > 0 {
		opts.Timeout = secondsToDuration(p.TimeoutSec)
	}

	run := h.Scheduler.Schedule(ctx, *check, ds, opts)
	if _, err := h.Quality.RecordRun(ctx, run); err != nil {
		// Storage errors should retry — the check ran fine, we just couldn't
		// persist the result.
		return fmt.Errorf("worker: record check_run: %w", err)
	}

	if h.Events != nil {
		eventType := events.CheckPassed
		severity := events.SeverityInfo
		if run.Outcome != quality.OutcomePass {
			eventType = events.CheckFailed
			severity = events.Severity(check.Severity)
			if severity == "" {
				severity = events.SeverityWarning
			}
		}
		h.Events.Publish(ctx, events.Event{
			Type:         eventType,
			Severity:     severity,
			TenantID:     p.TenantID,
			ResourceType: "check_run",
			ResourceID:   run.ID,
			Attributes: map[string]any{
				"check_id":  check.ID,
				"asset_id":  check.AssetID,
				"value":     run.Value,
				"threshold": run.Threshold,
				"outcome":   string(run.Outcome),
			},
			OccurredAt: run.FinishedAt,
		})
	}

	h.logger().InfoContext(ctx, "quality check finished",
		"tenant", p.TenantID, "check", check.ID, "outcome", run.Outcome,
		"value", run.Value, "duration_ms", run.Duration.Milliseconds())
	return nil
}

// HandleCrawlConnection walks a customer datasource and writes assets +
// edges + tags into the catalog. The worker resolves the connection
// + secret, dispatches by Type via SourceRegistry, and persists the
// resulting Tree via the in-memory orchestrator. Errors are returned
// so the queue can retry on transient failures (e.g. the source was
// briefly unreachable).
func (h *Handlers) HandleCrawlConnection(ctx context.Context, raw []byte) error {
	var p CrawlConnectionPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("worker: bad crawl payload: %w", err)
	}
	if p.TenantID == "" || p.ConnectionID == "" {
		return fmt.Errorf("worker: crawl payload missing tenant or connection id")
	}
	if h.Catalog == nil || h.Connections == nil || h.SourceRegistry == nil {
		return fmt.Errorf("worker: crawl dependencies not configured")
	}
	ctx = storage.WithTenant(ctx, p.TenantID)

	conn, err := h.Connections.Get(ctx, p.TenantID, p.ConnectionID)
	if err != nil {
		return fmt.Errorf("worker: load connection %q: %w", p.ConnectionID, err)
	}
	source, ok := h.SourceRegistry.Source(conn.Type)
	if !ok {
		return fmt.Errorf("worker: no crawler for type %q", conn.Type)
	}
	var secret []byte
	if conn.SecretURN != "" && h.Vault != nil {
		b, err := h.Vault.Get(ctx, p.TenantID, conn.SecretURN)
		if err != nil {
			return fmt.Errorf("worker: read vault secret: %w", err)
		}
		secret = b
	}
	tree, err := source.Crawl(ctx, conn.Config, secret)
	if err != nil {
		_ = h.Connections.UpdateHealth(ctx, p.TenantID, conn.ID, connection.HealthUnreachable, time.Now().UTC())
		return fmt.Errorf("worker: crawl: %w", err)
	}

	c := crawler.New(h.Catalog, h.logger())
	res, err := c.Run(ctx, conn.Name, tree, p.Actor)
	if err != nil {
		return fmt.Errorf("worker: project tree: %w", err)
	}
	_ = h.Connections.UpdateHealth(ctx, p.TenantID, conn.ID, connection.HealthHealthy, res.FinishedAt)
	h.logger().InfoContext(ctx, "crawl finished",
		"tenant", p.TenantID, "connection", conn.ID,
		"created", res.CreatedCount, "updated", res.UpdatedCount,
		"tagged", res.TaggedCount,
		"duration_ms", res.FinishedAt.Sub(res.StartedAt).Milliseconds())
	return nil
}

func secondsToDuration(s int) time.Duration {
	return time.Duration(s) * time.Second
}

// HandleClassifyConnection drives a sample-based classification run end-
// to-end and updates the durable job ledger. Errors are recorded on the
// job row (Fail) AND returned so the queue records the failure; we set
// MaxRetry=0 on this task so a failure is final.
func (h *Handlers) HandleClassifyConnection(ctx context.Context, raw []byte) error {
	var p ClassifyConnectionPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("worker: bad classify payload: %w", err)
	}
	if p.TenantID == "" || p.ConnectionID == "" || p.JobID == "" {
		return fmt.Errorf("worker: classify payload missing tenant/connection/job id")
	}
	if h.Classifier == nil {
		return fmt.Errorf("worker: classifier not configured")
	}
	ctx = storage.WithTenant(ctx, p.TenantID)
	if h.Jobs != nil {
		_ = h.Jobs.Start(ctx, p.JobID)
		_ = h.Jobs.Progress(ctx, p.JobID, 5, "sampling tables")
	}
	run, err := h.Classifier.ClassifyConnection(ctx, p.TenantID, p.ConnectionID, p.Actor)
	if err != nil {
		if h.Jobs != nil {
			_ = h.Jobs.Fail(ctx, p.JobID, err.Error())
		}
		h.logger().ErrorContext(ctx, "classify failed",
			"tenant", p.TenantID, "connection", p.ConnectionID, "job", p.JobID, "err", err)
		return err
	}
	if h.Jobs != nil {
		_ = h.Jobs.Succeed(ctx, p.JobID, map[string]any{
			"tables":      run.Tables,
			"columns":     run.Columns,
			"tagged":      run.Tagged,
			"skipped":     run.Skipped,
			"duration_ms": run.DurationMs,
		})
	}
	h.logger().InfoContext(ctx, "classify finished",
		"tenant", p.TenantID, "connection", p.ConnectionID, "job", p.JobID,
		"tables", run.Tables, "tagged", run.Tagged)
	return nil
}

// HandleSearchReindex re-embeds every asset in the tenant. Same job-row
// pattern as HandleClassifyConnection.
func (h *Handlers) HandleSearchReindex(ctx context.Context, raw []byte) error {
	var p SearchReindexPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("worker: bad reindex payload: %w", err)
	}
	if p.TenantID == "" || p.JobID == "" {
		return fmt.Errorf("worker: reindex payload missing tenant/job id")
	}
	if h.SearchIndexer == nil {
		return fmt.Errorf("worker: search indexer not configured")
	}
	ctx = storage.WithTenant(ctx, p.TenantID)
	if h.Jobs != nil {
		_ = h.Jobs.Start(ctx, p.JobID)
		_ = h.Jobs.Progress(ctx, p.JobID, 5, "embedding assets")
	}
	written, err := h.SearchIndexer.IndexAll(ctx, p.TenantID)
	if err != nil {
		if h.Jobs != nil {
			_ = h.Jobs.Fail(ctx, p.JobID, err.Error())
		}
		h.logger().ErrorContext(ctx, "reindex failed",
			"tenant", p.TenantID, "job", p.JobID, "err", err)
		return err
	}
	if h.Jobs != nil {
		_ = h.Jobs.Succeed(ctx, p.JobID, map[string]any{"indexed": written})
	}
	h.logger().InfoContext(ctx, "reindex finished",
		"tenant", p.TenantID, "job", p.JobID, "indexed", written)
	return nil
}

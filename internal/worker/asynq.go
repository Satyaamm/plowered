package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
)

// AsynqConfig captures the knobs the worker binary needs.
type AsynqConfig struct {
	RedisAddr      string         // host:port
	RedisDB        int            // logical DB index
	RedisPassword  string         // optional
	Concurrency    int            // worker goroutines (default 10)
	QueuePriorities map[string]int // queue → priority weight
}

// asynqOpt converts AsynqConfig into the upstream RedisClientOpt.
func (c AsynqConfig) asynqOpt() asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Addr:     c.RedisAddr,
		DB:       c.RedisDB,
		Password: c.RedisPassword,
	}
}

// AsynqEnqueuer is the Redis-backed Enqueuer. One instance is shared across
// the API process; jobs land in named queues so the worker pool can apply
// per-tenant priorities later.
type AsynqEnqueuer struct {
	client *asynq.Client
}

func NewAsynqEnqueuer(cfg AsynqConfig) *AsynqEnqueuer {
	return &AsynqEnqueuer{client: asynq.NewClient(cfg.asynqOpt())}
}

// Close releases the underlying Redis connection.
func (e *AsynqEnqueuer) Close() error { return e.client.Close() }

func (e *AsynqEnqueuer) EnqueuePipelineRun(ctx context.Context, p PipelineRunPayload) error {
	body, err := marshal(p)
	if err != nil {
		return fmt.Errorf("worker: marshal pipeline payload: %w", err)
	}
	task := asynq.NewTask(TaskPipelineRun, body,
		asynq.Queue(queueFor(p.TenantID)),
		asynq.MaxRetry(3),
	)
	_, err = e.client.EnqueueContext(ctx, task)
	return err
}

func (e *AsynqEnqueuer) EnqueueQualityRun(ctx context.Context, p QualityRunPayload) error {
	body, err := marshal(p)
	if err != nil {
		return fmt.Errorf("worker: marshal quality payload: %w", err)
	}
	task := asynq.NewTask(TaskQualityRun, body,
		asynq.Queue(queueFor(p.TenantID)),
		asynq.MaxRetry(2),
	)
	_, err = e.client.EnqueueContext(ctx, task)
	return err
}

func (e *AsynqEnqueuer) EnqueueCrawlConnection(ctx context.Context, p CrawlConnectionPayload) error {
	body, err := marshal(p)
	if err != nil {
		return fmt.Errorf("worker: marshal crawl payload: %w", err)
	}
	task := asynq.NewTask(TaskCrawlConnection, body,
		asynq.Queue(queueFor(p.TenantID)),
		asynq.MaxRetry(2),
	)
	_, err = e.client.EnqueueContext(ctx, task)
	return err
}

func (e *AsynqEnqueuer) EnqueueClassifyConnection(ctx context.Context, p ClassifyConnectionPayload) error {
	body, err := marshal(p)
	if err != nil {
		return fmt.Errorf("worker: marshal classify payload: %w", err)
	}
	// Classification can take minutes on wide warehouses — generous
	// timeout, no retries (a half-classified state is worse than a
	// failed-and-rerun state).
	task := asynq.NewTask(TaskClassifyConnection, body,
		asynq.Queue(queueFor(p.TenantID)),
		asynq.MaxRetry(0),
		asynq.Timeout(20*time.Minute),
	)
	_, err = e.client.EnqueueContext(ctx, task)
	return err
}

func (e *AsynqEnqueuer) EnqueueSearchReindex(ctx context.Context, p SearchReindexPayload) error {
	body, err := marshal(p)
	if err != nil {
		return fmt.Errorf("worker: marshal reindex payload: %w", err)
	}
	task := asynq.NewTask(TaskSearchReindex, body,
		asynq.Queue(queueFor(p.TenantID)),
		asynq.MaxRetry(1),
		asynq.Timeout(15*time.Minute),
	)
	_, err = e.client.EnqueueContext(ctx, task)
	return err
}

// queueFor returns the queue name for a tenant. We currently route all
// jobs to "default" — Asynq workers consume only queues they know
// about, so per-tenant queues require a discovery mechanism we'll add
// later. The per-tenant routing is preserved as a TODO.
func queueFor(tenantID string) string {
	return "default"
}

// NewAsynqServer builds the Asynq server bound to the supplied Handlers.
// The caller owns the lifecycle: call Run to block until shutdown.
func NewAsynqServer(cfg AsynqConfig, h *Handlers, logger *slog.Logger) *asynq.Server {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}
	srv := asynq.NewServer(cfg.asynqOpt(), asynq.Config{
		Concurrency: cfg.Concurrency,
		Queues:      cfg.QueuePriorities,
		Logger:      asynqLogger{logger: logger},
	})
	mux := asynq.NewServeMux()
	mux.HandleFunc(TaskPipelineRun, func(ctx context.Context, t *asynq.Task) error {
		return h.HandlePipelineRun(ctx, t.Payload())
	})
	mux.HandleFunc(TaskQualityRun, func(ctx context.Context, t *asynq.Task) error {
		return h.HandleQualityRun(ctx, t.Payload())
	})
	mux.HandleFunc(TaskCrawlConnection, func(ctx context.Context, t *asynq.Task) error {
		return h.HandleCrawlConnection(ctx, t.Payload())
	})
	mux.HandleFunc(TaskClassifyConnection, func(ctx context.Context, t *asynq.Task) error {
		return h.HandleClassifyConnection(ctx, t.Payload())
	})
	mux.HandleFunc(TaskSearchReindex, func(ctx context.Context, t *asynq.Task) error {
		return h.HandleSearchReindex(ctx, t.Payload())
	})

	go func() {
		if err := srv.Run(mux); err != nil {
			if logger != nil {
				logger.Error("asynq server stopped", "err", err)
			}
		}
	}()
	return srv
}

// asynqLogger adapts slog to Asynq's expected interface.
type asynqLogger struct{ logger *slog.Logger }

func (l asynqLogger) emit(level slog.Level, args ...any) {
	if l.logger == nil {
		return
	}
	l.logger.Log(context.Background(), level, fmt.Sprint(args...))
}
func (l asynqLogger) Debug(args ...any) { l.emit(slog.LevelDebug, args...) }
func (l asynqLogger) Info(args ...any)  { l.emit(slog.LevelInfo, args...) }
func (l asynqLogger) Warn(args ...any)  { l.emit(slog.LevelWarn, args...) }
func (l asynqLogger) Error(args ...any) { l.emit(slog.LevelError, args...) }
func (l asynqLogger) Fatal(args ...any) { l.emit(slog.LevelError, args...) }

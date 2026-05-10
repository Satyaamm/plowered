package worker

import (
	"context"
	"fmt"
	"log/slog"
)

// SyncEnqueuer runs jobs inline on the calling goroutine. Useful for
// embedded mode (single-binary deployments without Redis) and for tests.
//
// Errors from handlers are logged but never propagated past the calling
// HTTP request — they would translate into a 500 on the API side and the
// caller intentionally chose async semantics.
type SyncEnqueuer struct {
	Handlers *Handlers
	Logger   *slog.Logger
}

// NewSyncEnqueuer is convenience for the cmd wiring.
func NewSyncEnqueuer(h *Handlers) *SyncEnqueuer {
	if h == nil {
		panic("worker: nil Handlers")
	}
	return &SyncEnqueuer{Handlers: h}
}

func (s *SyncEnqueuer) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *SyncEnqueuer) EnqueuePipelineRun(ctx context.Context, p PipelineRunPayload) error {
	body, err := marshal(p)
	if err != nil {
		return fmt.Errorf("worker: marshal pipeline payload: %w", err)
	}
	go func(parent context.Context) {
		ctx := context.WithoutCancel(parent)
		if err := s.Handlers.HandlePipelineRun(ctx, body); err != nil {
			s.logger().ErrorContext(ctx, "sync pipeline run failed", "err", err)
		}
	}(ctx)
	return nil
}

func (s *SyncEnqueuer) EnqueueQualityRun(ctx context.Context, p QualityRunPayload) error {
	body, err := marshal(p)
	if err != nil {
		return fmt.Errorf("worker: marshal quality payload: %w", err)
	}
	go func(parent context.Context) {
		ctx := context.WithoutCancel(parent)
		if err := s.Handlers.HandleQualityRun(ctx, body); err != nil {
			s.logger().ErrorContext(ctx, "sync quality run failed", "err", err)
		}
	}(ctx)
	return nil
}

func (s *SyncEnqueuer) EnqueueCrawlConnection(ctx context.Context, p CrawlConnectionPayload) error {
	body, err := marshal(p)
	if err != nil {
		return fmt.Errorf("worker: marshal crawl payload: %w", err)
	}
	go func(parent context.Context) {
		ctx := context.WithoutCancel(parent)
		if err := s.Handlers.HandleCrawlConnection(ctx, body); err != nil {
			s.logger().ErrorContext(ctx, "sync crawl failed", "err", err)
		}
	}(ctx)
	return nil
}

func (s *SyncEnqueuer) EnqueueClassifyConnection(ctx context.Context, p ClassifyConnectionPayload) error {
	body, err := marshal(p)
	if err != nil {
		return fmt.Errorf("worker: marshal classify payload: %w", err)
	}
	go func(parent context.Context) {
		ctx := context.WithoutCancel(parent)
		if err := s.Handlers.HandleClassifyConnection(ctx, body); err != nil {
			s.logger().ErrorContext(ctx, "sync classify failed", "err", err)
		}
	}(ctx)
	return nil
}

func (s *SyncEnqueuer) EnqueueSearchReindex(ctx context.Context, p SearchReindexPayload) error {
	body, err := marshal(p)
	if err != nil {
		return fmt.Errorf("worker: marshal reindex payload: %w", err)
	}
	go func(parent context.Context) {
		ctx := context.WithoutCancel(parent)
		if err := s.Handlers.HandleSearchReindex(ctx, body); err != nil {
			s.logger().ErrorContext(ctx, "sync reindex failed", "err", err)
		}
	}(ctx)
	return nil
}

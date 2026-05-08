package shared

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
)

// Sink is the write surface a connector emits into. Implementations buffer,
// batch, and flush to durable storage.
type Sink interface {
	UpsertAsset(ctx context.Context, a *graph.Asset) error
	UpsertEdge(ctx context.Context, e *graph.Edge) error
	Flush(ctx context.Context) error
	Stats() SinkStats
}

// SinkStats counts what a connector has produced in this run.
type SinkStats struct {
	Assets int64
	Edges  int64
}

// BatchedSink is the default Sink. It buffers writes and flushes either when
// the buffer reaches BatchSize or when Flush is called. Errors short-circuit:
// the first failed write returns and subsequent writes also fail until the
// sink is reset.
type BatchedSink struct {
	store     storage.Store
	BatchSize int

	mu     sync.Mutex
	assets []*graph.Asset
	edges  []*graph.Edge
	stats  SinkStats
	err    error
}

// NewBatchedSink constructs a BatchedSink wrapping store. BatchSize defaults
// to 200 when ≤ 0.
func NewBatchedSink(store storage.Store, batchSize int) *BatchedSink {
	if batchSize <= 0 {
		batchSize = 200
	}
	return &BatchedSink{store: store, BatchSize: batchSize}
}

func (s *BatchedSink) UpsertAsset(ctx context.Context, a *graph.Asset) error {
	if err := s.firstErr(); err != nil {
		return err
	}
	s.mu.Lock()
	s.assets = append(s.assets, a)
	pending := len(s.assets) >= s.BatchSize
	s.mu.Unlock()
	if pending {
		return s.Flush(ctx)
	}
	return nil
}

func (s *BatchedSink) UpsertEdge(ctx context.Context, e *graph.Edge) error {
	if err := s.firstErr(); err != nil {
		return err
	}
	s.mu.Lock()
	s.edges = append(s.edges, e)
	pending := len(s.edges) >= s.BatchSize
	s.mu.Unlock()
	if pending {
		return s.Flush(ctx)
	}
	return nil
}

func (s *BatchedSink) Flush(ctx context.Context) error {
	s.mu.Lock()
	assets, edges := s.assets, s.edges
	s.assets, s.edges = nil, nil
	s.mu.Unlock()

	for _, a := range assets {
		if err := s.upsertAssetOnce(ctx, a); err != nil {
			s.recordErr(err)
			return err
		}
		s.bumpStats(1, 0)
	}
	for _, e := range edges {
		if err := s.upsertEdgeOnce(ctx, e); err != nil {
			s.recordErr(err)
			return err
		}
		s.bumpStats(0, 1)
	}
	return nil
}

// upsertAssetOnce attempts to create the asset; on conflict it falls back to
// looking up by qualified_name and updating in place.
func (s *BatchedSink) upsertAssetOnce(ctx context.Context, a *graph.Asset) error {
	if _, err := s.store.CreateAsset(ctx, a); err == nil {
		return nil
	} else if !errors.Is(err, graph.ErrConflict) {
		return fmt.Errorf("create asset %s: %w", a.QualifiedName, err)
	}
	existing, err := s.store.GetAssetByQualifiedName(ctx, a.QualifiedName)
	if err != nil {
		return fmt.Errorf("lookup asset %s: %w", a.QualifiedName, err)
	}
	a.ID = existing.ID
	if _, err := s.store.UpdateAsset(ctx, a); err != nil {
		return fmt.Errorf("update asset %s: %w", a.QualifiedName, err)
	}
	return nil
}

func (s *BatchedSink) upsertEdgeOnce(ctx context.Context, e *graph.Edge) error {
	_, err := s.store.CreateEdge(ctx, e)
	if err != nil && !errors.Is(err, graph.ErrConflict) {
		return fmt.Errorf("create edge %s→%s: %w", e.SourceID, e.TargetID, err)
	}
	return nil
}

func (s *BatchedSink) Stats() SinkStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

func (s *BatchedSink) firstErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *BatchedSink) recordErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

func (s *BatchedSink) bumpStats(assets, edges int64) {
	s.mu.Lock()
	s.stats.Assets += assets
	s.stats.Edges += edges
	s.mu.Unlock()
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "id-fallback"
	}
	return hex.EncodeToString(b[:])
}

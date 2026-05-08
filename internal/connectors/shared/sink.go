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
	Assets       int64
	Edges        int64
	UnresolvedQN int64 // edges dropped because source/target QN did not resolve
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
		ok, err := s.upsertEdgeOnce(ctx, e)
		if err != nil {
			s.recordErr(err)
			return err
		}
		if ok {
			s.bumpStats(0, 1)
		} else {
			s.bumpUnresolved()
		}
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

// upsertEdgeOnce inserts an edge. Connectors typically don't know asset UUIDs
// at write time — they emit edges with `source_qn`/`target_qn` in
// Properties; this helper resolves those to IDs by qualified-name lookup and
// fills SourceID/TargetID before insert. If either endpoint can't be
// resolved, the edge is dropped and the (caller-visible) UnresolvedQN counter
// is bumped — partial graphs are normal during early crawls.
//
// Returns (true, nil) on insert, (false, nil) on a clean drop, and
// (false, err) on a real failure that should abort the run.
func (s *BatchedSink) upsertEdgeOnce(ctx context.Context, e *graph.Edge) (bool, error) {
	if e.SourceID == "" || e.TargetID == "" {
		if err := s.resolveEdgeByQN(ctx, e); err != nil {
			if errors.Is(err, errEdgeUnresolvable) {
				return false, nil
			}
			return false, err
		}
	}
	_, err := s.store.CreateEdge(ctx, e)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, graph.ErrConflict) {
		return true, nil
	}
	return false, fmt.Errorf("create edge %s→%s: %w", e.SourceID, e.TargetID, err)
}

var errEdgeUnresolvable = errors.New("edge endpoints not yet in graph")

func (s *BatchedSink) resolveEdgeByQN(ctx context.Context, e *graph.Edge) error {
	srcQN := stringFromMap(e.Properties, "source_qn")
	tgtQN := stringFromMap(e.Properties, "target_qn")
	if srcQN == "" || tgtQN == "" {
		return fmt.Errorf("edge missing IDs and source_qn/target_qn properties")
	}
	src, err := s.store.GetAssetByQualifiedName(ctx, srcQN)
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			return errEdgeUnresolvable
		}
		return fmt.Errorf("resolve source %s: %w", srcQN, err)
	}
	tgt, err := s.store.GetAssetByQualifiedName(ctx, tgtQN)
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			return errEdgeUnresolvable
		}
		return fmt.Errorf("resolve target %s: %w", tgtQN, err)
	}
	e.SourceID = src.ID
	e.TargetID = tgt.ID
	return nil
}

func stringFromMap(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	v, _ := m[k].(string)
	return v
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

func (s *BatchedSink) bumpUnresolved() {
	s.mu.Lock()
	s.stats.UnresolvedQN++
	s.mu.Unlock()
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "id-fallback"
	}
	return hex.EncodeToString(b[:])
}

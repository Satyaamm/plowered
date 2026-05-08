package shared_test

import (
	"context"
	"testing"

	"github.com/Satyaamm/plowered/internal/connectors/shared"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/memory"
)

func tenantCtx(t string) context.Context {
	return storage.WithTenant(context.Background(), t)
}

func TestSinkResolvesEdgesByQN(t *testing.T) {
	store := memory.New()
	ctx := tenantCtx("acme")

	src, _ := store.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "wh://prod/raw/orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	})
	tgt, _ := store.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "wh://prod/mart/orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	})

	sink := shared.NewBatchedSink(store, 10)
	if err := sink.UpsertEdge(ctx, &graph.Edge{
		Kind: graph.EdgeLineage,
		Properties: map[string]any{
			"source_qn": src.QualifiedName,
			"target_qn": tgt.QualifiedName,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if sink.Stats().Edges != 1 {
		t.Errorf("edges = %d, want 1", sink.Stats().Edges)
	}
	if sink.Stats().UnresolvedQN != 0 {
		t.Errorf("unresolved = %d, want 0", sink.Stats().UnresolvedQN)
	}
}

func TestSinkDropsUnresolvableEdges(t *testing.T) {
	store := memory.New()
	ctx := tenantCtx("acme")

	src, _ := store.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "wh://prod/raw/orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	})

	sink := shared.NewBatchedSink(store, 10)
	if err := sink.UpsertEdge(ctx, &graph.Edge{
		Kind: graph.EdgeLineage,
		Properties: map[string]any{
			"source_qn": src.QualifiedName,
			"target_qn": "wh://prod/mart/missing",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Flush(ctx); err != nil {
		t.Fatalf("flush should not error on unresolved QN: %v", err)
	}
	if sink.Stats().Edges != 0 {
		t.Errorf("edges = %d, want 0", sink.Stats().Edges)
	}
	if sink.Stats().UnresolvedQN != 1 {
		t.Errorf("unresolved = %d, want 1", sink.Stats().UnresolvedQN)
	}
}

func TestSinkAcceptsExplicitIDsTooo(t *testing.T) {
	store := memory.New()
	ctx := tenantCtx("acme")
	src, _ := store.CreateAsset(ctx, &graph.Asset{QualifiedName: "x", Type: graph.AssetTypeTable, Name: "x"})
	tgt, _ := store.CreateAsset(ctx, &graph.Asset{QualifiedName: "y", Type: graph.AssetTypeTable, Name: "y"})

	sink := shared.NewBatchedSink(store, 10)
	if err := sink.UpsertEdge(ctx, &graph.Edge{
		Kind: graph.EdgeLineage, SourceID: src.ID, TargetID: tgt.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if sink.Stats().Edges != 1 {
		t.Errorf("edges = %d", sink.Stats().Edges)
	}
}

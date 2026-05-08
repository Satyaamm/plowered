package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/memory"
)

func ctxWith(t string) context.Context {
	return storage.WithTenant(context.Background(), t)
}

func TestCreateAndGet(t *testing.T) {
	s := memory.New()
	ctx := ctxWith("acme")

	got, err := s.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "warehouse://prod.public.orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	})
	if err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	if got.ID == "" {
		t.Fatal("expected generated ID")
	}
	if got.TenantID != "acme" {
		t.Errorf("tenant id = %q, want acme", got.TenantID)
	}

	got2, err := s.GetAsset(ctx, got.ID)
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if got2.QualifiedName != got.QualifiedName {
		t.Errorf("qn = %q, want %q", got2.QualifiedName, got.QualifiedName)
	}
}

func TestTenantIsolation(t *testing.T) {
	s := memory.New()
	a, err := s.CreateAsset(ctxWith("acme"), &graph.Asset{
		QualifiedName: "warehouse://prod.public.orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.GetAsset(ctxWith("globex"), a.ID); !errors.Is(err, graph.ErrNotFound) {
		t.Errorf("cross-tenant read leaked or wrong error: %v", err)
	}

	// Same qualified name in a different tenant must be allowed.
	if _, err := s.CreateAsset(ctxWith("globex"), &graph.Asset{
		QualifiedName: "warehouse://prod.public.orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	}); err != nil {
		t.Errorf("collision across tenants should be allowed: %v", err)
	}
}

func TestNoTenantContext(t *testing.T) {
	s := memory.New()
	_, err := s.CreateAsset(context.Background(), &graph.Asset{
		QualifiedName: "x", Type: graph.AssetTypeTable, Name: "x",
	})
	if !errors.Is(err, graph.ErrTenantMissing) {
		t.Errorf("want ErrTenantMissing, got %v", err)
	}
}

func TestQualifiedNameUnique(t *testing.T) {
	s := memory.New()
	ctx := ctxWith("acme")
	_, err := s.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "x", Type: graph.AssetTypeTable, Name: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "x", Type: graph.AssetTypeTable, Name: "x2",
	})
	if !errors.Is(err, graph.ErrConflict) {
		t.Errorf("want ErrConflict on duplicate qualified_name, got %v", err)
	}
}

func TestEdgesAndDelete(t *testing.T) {
	s := memory.New()
	ctx := ctxWith("acme")

	src, _ := s.CreateAsset(ctx, &graph.Asset{QualifiedName: "a", Type: graph.AssetTypeTable, Name: "a"})
	tgt, _ := s.CreateAsset(ctx, &graph.Asset{QualifiedName: "b", Type: graph.AssetTypeTable, Name: "b"})

	if _, err := s.CreateEdge(ctx, &graph.Edge{
		Kind: graph.EdgeLineage, SourceID: src.ID, TargetID: tgt.ID,
	}); err != nil {
		t.Fatal(err)
	}

	out, err := s.Neighbors(ctx, src.ID, storage.NeighborsOptions{
		Kind: graph.EdgeLineage, Outgoing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].TargetID != tgt.ID {
		t.Errorf("Neighbors = %+v", out)
	}

	if err := s.DeleteAsset(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	out, _ = s.Neighbors(ctx, src.ID, storage.NeighborsOptions{Outgoing: true})
	if len(out) != 0 {
		t.Errorf("edges should be cascade-deleted, got %+v", out)
	}
}

func TestList(t *testing.T) {
	s := memory.New()
	ctx := ctxWith("acme")
	for i := 0; i < 5; i++ {
		qn := string(rune('a' + i))
		if _, err := s.CreateAsset(ctx, &graph.Asset{
			QualifiedName: qn, Type: graph.AssetTypeTable, Name: qn,
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := s.ListAssets(ctx, storage.ListAssetsOptions{PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("want 5 assets, got %d", len(got))
	}
}

package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/postgres"
)

// Integration tests run only when PLOWERED_TEST_DATABASE_URL is set.
// Locally: docker compose up -d postgres &&
//   PLOWERED_TEST_DATABASE_URL='postgres://plowered:plowered@localhost:5432/plowered?sslmode=disable' \
//   go test ./internal/storage/postgres/...

func newStore(t *testing.T) (*postgres.Store, func()) {
	t.Helper()
	url := os.Getenv("PLOWERED_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PLOWERED_TEST_DATABASE_URL not set; skipping postgres integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Per-test isolation: delete from a tenant prefix unique to this run.
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM edges  WHERE tenant_id LIKE 'test-%'`)
		_, _ = pool.Exec(ctx, `DELETE FROM assets WHERE tenant_id LIKE 'test-%'`)
		pool.Close()
	}
	return postgres.New(pool), cleanup
}

func ctxWith(t string) context.Context {
	return storage.WithTenant(context.Background(), "test-"+t)
}

func TestPGCreateAndGet(t *testing.T) {
	s, done := newStore(t)
	defer done()

	got, err := s.CreateAsset(ctxWith("acme"), &graph.Asset{
		QualifiedName: "warehouse://prod.public.orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	})
	if err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	if got.ID == "" || got.TenantID != "test-acme" {
		t.Fatalf("got = %+v", got)
	}

	got2, err := s.GetAsset(ctxWith("acme"), got.ID)
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if got2.QualifiedName != got.QualifiedName {
		t.Errorf("qn = %q", got2.QualifiedName)
	}
}

func TestPGTenantIsolation(t *testing.T) {
	s, done := newStore(t)
	defer done()

	a, err := s.CreateAsset(ctxWith("acme"), &graph.Asset{
		QualifiedName: "warehouse://prod.public.orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.GetAsset(ctxWith("globex"), a.ID); !errors.Is(err, graph.ErrNotFound) {
		t.Errorf("cross-tenant read leaked: %v", err)
	}

	if _, err := s.CreateAsset(ctxWith("globex"), &graph.Asset{
		QualifiedName: "warehouse://prod.public.orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	}); err != nil {
		t.Errorf("collision across tenants should be allowed: %v", err)
	}
}

func TestPGNoTenantContext(t *testing.T) {
	s, done := newStore(t)
	defer done()

	_, err := s.CreateAsset(context.Background(), &graph.Asset{
		QualifiedName: "x", Type: graph.AssetTypeTable, Name: "x",
	})
	if !errors.Is(err, graph.ErrTenantMissing) {
		t.Errorf("want ErrTenantMissing, got %v", err)
	}
}

func TestPGUniqueQualifiedName(t *testing.T) {
	s, done := newStore(t)
	defer done()

	ctx := ctxWith("acme")
	if _, err := s.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "warehouse://x", Type: graph.AssetTypeTable, Name: "x",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "warehouse://x", Type: graph.AssetTypeTable, Name: "x2",
	}); !errors.Is(err, graph.ErrConflict) {
		t.Errorf("want ErrConflict, got %v", err)
	}
}

func TestPGEdgeCascade(t *testing.T) {
	s, done := newStore(t)
	defer done()

	ctx := ctxWith("acme")
	src, _ := s.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "warehouse://a", Type: graph.AssetTypeTable, Name: "a",
	})
	tgt, _ := s.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "warehouse://b", Type: graph.AssetTypeTable, Name: "b",
	})
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
	if len(out) != 1 {
		t.Fatalf("want 1 edge, got %d", len(out))
	}

	if err := s.DeleteAsset(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	out, _ = s.Neighbors(ctx, src.ID, storage.NeighborsOptions{Outgoing: true})
	if len(out) != 0 {
		t.Errorf("edges should cascade-delete with the asset, got %d", len(out))
	}
}

func TestPGListPagination(t *testing.T) {
	s, done := newStore(t)
	defer done()

	ctx := ctxWith("page")
	for i := 0; i < 5; i++ {
		qn := "warehouse://" + string(rune('a'+i))
		if _, err := s.CreateAsset(ctx, &graph.Asset{
			QualifiedName: qn, Type: graph.AssetTypeTable, Name: qn,
		}); err != nil {
			t.Fatal(err)
		}
	}

	page1, next, err := s.ListAssets(ctx, storage.ListAssetsOptions{PageSize: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 3 || next == "" {
		t.Fatalf("page1 size=%d next=%q", len(page1), next)
	}

	page2, next2, err := s.ListAssets(ctx, storage.ListAssetsOptions{PageSize: 3, PageToken: next})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 || next2 != "" {
		t.Errorf("page2 size=%d next=%q", len(page2), next2)
	}
}

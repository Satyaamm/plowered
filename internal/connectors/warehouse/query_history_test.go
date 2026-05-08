package warehouse

// White-box tests for query-history → edge translation. Mocks the SQL layer
// by exercising emitFromQuery + normalizeQN directly so the lineage path is
// covered without spinning up a database.

import (
	"context"
	"testing"
	"time"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/memory"

	connectorsShared "github.com/Satyaamm/plowered/internal/connectors/shared"
)

func tenantCtx(t string) context.Context {
	return storage.WithTenant(context.Background(), t)
}

func TestEmitFromQueryProducesLineageEdges(t *testing.T) {
	store := memory.New()
	ctx := tenantCtx("acme")

	// Pre-create the assets the parser will reference so the BatchedSink
	// can write the edges (UpsertEdge needs both endpoints to exist).
	for _, qn := range []string{
		"wh://prod/raw/orders",
		"wh://prod/mart/orders",
	} {
		if _, err := store.CreateAsset(ctx, &graph.Asset{
			QualifiedName: qn, Type: graph.AssetTypeTable, Name: lastSegment(qn),
		}); err != nil {
			t.Fatal(err)
		}
	}

	sink := connectorsShared.NewBatchedSink(store, 10)
	row := HistoryRow{
		QueryText:  "INSERT INTO mart.orders SELECT * FROM raw.orders",
		ExecutedAt: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		UserName:   "etl",
		DurationMS: 120,
	}
	if err := emitFromQuery(ctx, row, "prod", sink); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if err := sink.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := sink.Stats().Edges; got != 1 {
		t.Errorf("edges = %d, want 1", got)
	}
}

func TestEmitFromQueryHandlesUnparseable(t *testing.T) {
	store := memory.New()
	sink := connectorsShared.NewBatchedSink(store, 10)
	row := HistoryRow{QueryText: "SHOW WAREHOUSES;", ExecutedAt: time.Now()}
	if err := emitFromQuery(tenantCtx("acme"), row, "prod", sink); err != nil {
		t.Errorf("non-DML SQL should be silently skipped, got %v", err)
	}
}

func TestNormalizeQNVariants(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"orders", "wh://prod/public/orders"},
		{"mart.orders", "wh://prod/mart/orders"},
		{"prod.mart.orders", "wh://prod/mart/orders"},
		{"wh://prod/mart/orders", "wh://prod/mart/orders"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeQN("prod", c.in); got != c.want {
			t.Errorf("normalizeQN(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSQLHashStable(t *testing.T) {
	a := sqlHash("INSERT INTO x SELECT * FROM y")
	b := sqlHash("INSERT INTO x SELECT * FROM y")
	c := sqlHash("INSERT INTO x SELECT * FROM z")
	if a != b {
		t.Error("hash should be deterministic")
	}
	if a == c {
		t.Error("different SQL should produce different hashes")
	}
	if len(a) != 16 {
		t.Errorf("hash length = %d, want 16", len(a))
	}
}

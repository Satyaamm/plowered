package shared_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Satyaamm/plowered/internal/connectors/shared"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/memory"
)

type fakeConnector struct{}

func (fakeConnector) Info() shared.Info {
	return shared.Info{Name: "fake", Version: "0.0.1"}
}
func (fakeConnector) Validate(_ context.Context, _ shared.Config) error { return nil }
func (fakeConnector) Crawl(_ context.Context, _ shared.Config, _ shared.Sink) error {
	return nil
}

func TestRegistryRoundTrip(t *testing.T) {
	r := shared.NewRegistry()
	if err := r.Register("fake", func() shared.Connector { return fakeConnector{} }); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("fake", func() shared.Connector { return fakeConnector{} }); err == nil {
		t.Error("duplicate register should fail")
	}
	c, err := r.Build("fake")
	if err != nil {
		t.Fatal(err)
	}
	if c.Info().Name != "fake" {
		t.Errorf("info = %+v", c.Info())
	}
	if got := r.Names(); len(got) != 1 || got[0] != "fake" {
		t.Errorf("names = %v", got)
	}
	if _, err := r.Build("missing"); err == nil {
		t.Error("expected unknown error")
	}
}

func TestBatchedSinkUpsertsAndFlushes(t *testing.T) {
	store := memory.New()
	ctx := storage.WithTenant(context.Background(), "t1")

	sink := shared.NewBatchedSink(store, 2)

	for i := 0; i < 3; i++ {
		qn := "warehouse://table" + string(rune('a'+i))
		if err := sink.UpsertAsset(ctx, &graph.Asset{
			QualifiedName: qn, Type: graph.AssetTypeTable, Name: qn,
		}); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}
	if err := sink.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := sink.Stats().Assets; got != 3 {
		t.Errorf("assets = %d, want 3", got)
	}
}

func TestBatchedSinkUpsertExisting(t *testing.T) {
	store := memory.New()
	ctx := storage.WithTenant(context.Background(), "t1")

	if _, err := store.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "warehouse://orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	}); err != nil {
		t.Fatal(err)
	}

	sink := shared.NewBatchedSink(store, 10)
	if err := sink.UpsertAsset(ctx, &graph.Asset{
		QualifiedName: "warehouse://orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
		Description:   "updated",
	}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetAssetByQualifiedName(ctx, "warehouse://orders")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "updated" {
		t.Errorf("description not updated: %q", got.Description)
	}
}

func TestRunLifecycle(t *testing.T) {
	r := shared.NewRun("fake", "instance-1")
	if r.Status != shared.RunRunning {
		t.Errorf("initial status = %s", r.Status)
	}
	r.Succeed(shared.SinkStats{Assets: 5, Edges: 7})
	if r.Status != shared.RunSucceeded || r.AssetsSeen != 5 || r.EdgesSeen != 7 {
		t.Errorf("after Succeed: %+v", r)
	}

	r2 := shared.NewRun("fake", "instance-1")
	r2.Fail(errors.New("boom"))
	if r2.Status != shared.RunFailed || r2.Err != "boom" {
		t.Errorf("after Fail: %+v", r2)
	}
}

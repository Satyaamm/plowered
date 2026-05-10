package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apihttp "github.com/Satyaamm/plowered/internal/api/http"
	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/notify"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/storage/memory"
	"github.com/Satyaamm/plowered/internal/worker"
)

// buildBinServer is a small helper that wires the same Deps the smoke
// test uses, parameterized by which roles the principal carries. Tests
// flip super_admin on/off to exercise the purge gate.
func buildBinServer(t *testing.T, roles []string) (*httptest.Server, *pipeline.MemoryStore, deleted.Repo, legalhold.Repo) {
	t.Helper()
	pStore := pipeline.NewMemoryStore()
	repo := deleted.NewMemoryRepo()
	holds := legalhold.NewMemoryRepo()

	mux := apihttp.NewMux(apihttp.Deps{
		Catalog:    memory.New(),
		Pipelines:  pStore,
		Quality:    quality.NewMemoryStore(),
		Notify:     notify.NewMemoryStore(),
		Policies:   policy.NewMemoryRuleStore(),
		Audit:      audit.NewMemoryWriter(),
		Deleted:    repo,
		LegalHolds: holds,
		Enqueuer:   worker.NoopEnqueuer{},
	})
	chain := apihttp.Chain(mux,
		apihttp.RecoveryMW(nil),
		apihttp.RequestIDMW(),
		apihttp.AuthMW(func(_ string) (auth.Principal, error) {
			return auth.Principal{ID: "u1", TenantID: "t1", Roles: roles}, nil
		}),
		apihttp.TenantMW(),
	)
	return httptest.NewServer(chain), pStore, repo, holds
}

func TestPipelineDeleteCapturesTombstoneAndRestoreRebuildsIt(t *testing.T) {
	srv, pStore, repo, _ := buildBinServer(t, []string{"admin"})
	defer srv.Close()

	// Create a pipeline so we have something to delete.
	body, _ := json.Marshal(pipeline.Pipeline{Name: "demo"})
	resp := httpDo(t, srv.URL+"/v1/pipelines", "POST", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	var p pipeline.Pipeline
	_ = json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()

	// DELETE → row gone from pipelines, tombstone in repo.
	del := httpDo(t, srv.URL+"/v1/pipelines/"+p.ID, "DELETE", nil)
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", del.StatusCode)
	}
	del.Body.Close()

	if _, err := pStore.GetPipeline(context.Background(), p.ID); err == nil {
		t.Errorf("pipeline still present after delete")
	}
	tombs, _ := repo.List(context.Background(), "t1", deleted.ListOptions{ResourceType: "pipeline"})
	if len(tombs) != 1 || tombs[0].ResourceID != p.ID {
		t.Fatalf("expected one tombstone for the pipeline, got %+v", tombs)
	}

	// Restore via the recycle-bin endpoint.
	rest := httpDo(t, srv.URL+"/v1/deleted/"+tombs[0].ID+"/restore", "POST", nil)
	if rest.StatusCode != http.StatusOK {
		buf := make([]byte, 1024)
		n, _ := rest.Body.Read(buf)
		t.Fatalf("restore status=%d body=%s", rest.StatusCode, string(buf[:n]))
	}
	rest.Body.Close()

	got, err := pStore.GetPipeline(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("expected pipeline back after restore: %v", err)
	}
	if got.Name != "demo" {
		t.Errorf("name lost on restore: %q", got.Name)
	}

	// Tombstone is now marked restored.
	updated, _ := repo.Get(context.Background(), "t1", tombs[0].ID)
	if updated.RestoredAt.IsZero() || updated.IsActive() {
		t.Errorf("tombstone restore state: %+v", updated)
	}
}

func TestPurgeRequiresSuperAdmin(t *testing.T) {
	// Step 1: admin creates + deletes a pipeline so a tombstone exists.
	srv, _, repo, _ := buildBinServer(t, []string{"admin"})
	defer srv.Close()

	body, _ := json.Marshal(pipeline.Pipeline{Name: "bin-only"})
	resp := httpDo(t, srv.URL+"/v1/pipelines", "POST", body)
	var p pipeline.Pipeline
	_ = json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()

	httpDo(t, srv.URL+"/v1/pipelines/"+p.ID, "DELETE", nil).Body.Close()

	tombs, _ := repo.List(context.Background(), "t1", deleted.ListOptions{})
	if len(tombs) != 1 {
		t.Fatalf("expected 1 tombstone, got %d", len(tombs))
	}
	tombID := tombs[0].ID

	// Step 2: as plain admin, purge → 403.
	denied := httpDo(t, srv.URL+"/v1/deleted/"+tombID, "DELETE", nil)
	if denied.StatusCode != http.StatusForbidden {
		t.Errorf("admin purge status=%d, want 403", denied.StatusCode)
	}
	denied.Body.Close()

	// Step 3: as super_admin (separate server with the elevated principal),
	// purge succeeds.
	superSrv, _, superRepo, _ := buildBinServer(t, []string{"super_admin"})
	defer superSrv.Close()
	// Capture a separate tombstone in this server's repo so we don't cross
	// state between two memory stores.
	respS := httpDo(t, superSrv.URL+"/v1/pipelines",
		"POST", mustJSON(pipeline.Pipeline{Name: "super-bin"}))
	var sp pipeline.Pipeline
	_ = json.NewDecoder(respS.Body).Decode(&sp)
	respS.Body.Close()
	httpDo(t, superSrv.URL+"/v1/pipelines/"+sp.ID, "DELETE", nil).Body.Close()
	stombs, _ := superRepo.List(context.Background(), "t1", deleted.ListOptions{})
	if len(stombs) != 1 {
		t.Fatalf("super-admin tombstone count = %d", len(stombs))
	}
	purged := httpDo(t, superSrv.URL+"/v1/deleted/"+stombs[0].ID, "DELETE", nil)
	if purged.StatusCode != http.StatusNoContent {
		t.Errorf("super_admin purge status=%d, want 204", purged.StatusCode)
	}
	purged.Body.Close()
	got, _ := superRepo.Get(context.Background(), "t1", stombs[0].ID)
	if got.PurgedAt.IsZero() {
		t.Errorf("expected purged_at to be set, got %+v", got)
	}
}

// TestLegalHoldBlocksDelete asserts that a delete on a resource caught by
// an active legal hold returns 409 with the hold id, and that releasing
// the hold re-enables deletion. Auditors test exactly this: the gate is
// enforced at the runtime layer, not just policy paperwork.
func TestLegalHoldBlocksDelete(t *testing.T) {
	srv, _, _, holds := buildBinServer(t, []string{"admin"})
	defer srv.Close()

	resp := httpDo(t, srv.URL+"/v1/pipelines", "POST",
		mustJSON(pipeline.Pipeline{Name: "held-resource"}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	var p pipeline.Pipeline
	_ = json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()

	// Issue a hold scoped to all pipelines for tenant t1.
	hold, err := holds.Issue(context.Background(), &legalhold.Hold{
		TenantID: "t1",
		Matter:   "Acme v. Plowered #2026-04",
		Scope:    legalhold.Scope{ResourceTypes: []string{"pipeline"}},
		IssuedBy: "u1",
	})
	if err != nil {
		t.Fatalf("issue hold: %v", err)
	}

	// Delete should be blocked with 409.
	del := httpDo(t, srv.URL+"/v1/pipelines/"+p.ID, "DELETE", nil)
	if del.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 while held, got %d", del.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(del.Body).Decode(&body)
	del.Body.Close()
	if body["hold_id"] != hold.ID {
		t.Errorf("response should surface hold_id, got %+v", body)
	}

	// Release and retry — delete now succeeds.
	if err := holds.Release(context.Background(), "t1", hold.ID, "u1", time.Now()); err != nil {
		t.Fatalf("release: %v", err)
	}
	del2 := httpDo(t, srv.URL+"/v1/pipelines/"+p.ID, "DELETE", nil)
	if del2.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204 after release, got %d", del2.StatusCode)
	}
	del2.Body.Close()
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

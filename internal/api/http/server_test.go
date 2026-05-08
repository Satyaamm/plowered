package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apihttp "github.com/Satyaamm/plowered/internal/api/http"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/memory"
)

func newTestServer(t *testing.T) (*httptest.Server, storage.Store) {
	t.Helper()
	store := memory.New()
	mw := []apihttp.Middleware{
		apihttp.RecoveryMW(nil),
		apihttp.RequestIDMW(),
		apihttp.AuthMW(func(token string) (auth.Principal, error) {
			return auth.Principal{ID: "u1", TenantID: "t1", Email: "u@example.com"}, nil
		}, "/healthz"),
		apihttp.TenantMW("/healthz"),
	}
	h := apihttp.Chain(apihttp.Mux(store), mw...)
	return httptest.NewServer(h), store
}

func req(t *testing.T, s *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r, _ := http.NewRequest(method, s.URL+path, rd)
	r.Header.Set("Authorization", "Bearer fake")
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestCreateAndGet(t *testing.T) {
	s, _ := newTestServer(t)
	defer s.Close()

	resp := req(t, s, "POST", "/v1/assets", graph.Asset{
		QualifiedName: "warehouse://orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var created graph.Asset
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	resp2 := req(t, s, "GET", "/v1/assets/"+created.ID, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("get status = %d", resp2.StatusCode)
	}
}

func TestSearchAndLineage(t *testing.T) {
	s, store := newTestServer(t)
	defer s.Close()

	ctx := storage.WithTenant(context.Background(), "t1")
	src, _ := store.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "warehouse://raw.orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	})
	tgt, _ := store.CreateAsset(ctx, &graph.Asset{
		QualifiedName: "warehouse://mart.orders",
		Type:          graph.AssetTypeTable,
		Name:          "orders",
	})
	_, _ = store.CreateEdge(ctx, &graph.Edge{
		Kind: graph.EdgeLineage, SourceID: src.ID, TargetID: tgt.ID,
	})

	resp := req(t, s, "POST", "/v1/assets:search", apihttp.SearchRequest{Query: "orders"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d", resp.StatusCode)
	}
	var sr struct {
		Hits []apihttp.SearchHit `json:"hits"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&sr)
	resp.Body.Close()
	if len(sr.Hits) != 2 {
		t.Errorf("hits = %d", len(sr.Hits))
	}

	resp2 := req(t, s, "GET", "/v1/assets/"+src.ID+"/lineage?direction=downstream", nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("lineage status = %d", resp2.StatusCode)
	}
	var lr apihttp.LineageResponse
	_ = json.NewDecoder(resp2.Body).Decode(&lr)
	resp2.Body.Close()
	if len(lr.Edges) != 1 || lr.Edges[0].Target != tgt.ID {
		t.Errorf("lineage = %+v", lr.Edges)
	}
}

func TestAuthRejectsMissingHeader(t *testing.T) {
	store := memory.New()
	h := apihttp.Chain(apihttp.Mux(store),
		apihttp.AuthMW(func(_ string) (auth.Principal, error) {
			return auth.Principal{ID: "u1", TenantID: "t1"}, nil
		}),
		apihttp.TenantMW(),
	)
	s := httptest.NewServer(h)
	defer s.Close()

	resp, err := http.Get(s.URL + "/v1/assets")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestNotFoundReturns404(t *testing.T) {
	s, _ := newTestServer(t)
	defer s.Close()
	resp := req(t, s, "GET", "/v1/assets/missing", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

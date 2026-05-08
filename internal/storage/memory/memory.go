// Package memory is an in-process Store used by tests and the embedded
// dev-mode binary when SQLite is not desired. It enforces tenant isolation
// the same way real backends do, so tests catch isolation bugs early.
package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
)

type Store struct {
	mu     sync.RWMutex
	assets map[string]*graph.Asset // key: tenantID + ":" + id
	byQN   map[string]string       // key: tenantID + ":" + qualifiedName -> id
	edges  map[string]*graph.Edge
	now    func() time.Time
}

func New() *Store {
	return &Store{
		assets: make(map[string]*graph.Asset),
		byQN:   make(map[string]string),
		edges:  make(map[string]*graph.Edge),
		now:    time.Now,
	}
}

func (s *Store) Ping(_ context.Context) error { return nil }
func (s *Store) Close() error                 { return nil }

func (s *Store) CreateAsset(ctx context.Context, a *graph.Asset) (*graph.Asset, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, graph.ErrInvalidArgument
	}
	cp := *a
	cp.TenantID = tenant
	if cp.ID == "" {
		cp.ID = newID()
	}
	now := s.now().UTC()
	cp.CreatedAt = now
	cp.UpdatedAt = now
	if cp.Trust == "" {
		cp.Trust = graph.TrustUnverified
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	qnk := qnKey(tenant, cp.QualifiedName)
	if _, exists := s.byQN[qnk]; exists {
		return nil, fmt.Errorf("qualified_name %q: %w", cp.QualifiedName, graph.ErrConflict)
	}
	if _, exists := s.assets[assetKey(tenant, cp.ID)]; exists {
		return nil, fmt.Errorf("id %q: %w", cp.ID, graph.ErrConflict)
	}
	s.assets[assetKey(tenant, cp.ID)] = &cp
	s.byQN[qnk] = cp.ID
	out := cp
	return &out, nil
}

func (s *Store) GetAsset(ctx context.Context, id string) (*graph.Asset, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.assets[assetKey(tenant, id)]
	if !ok {
		return nil, graph.ErrNotFound
	}
	out := *a
	return &out, nil
}

func (s *Store) GetAssetByQualifiedName(ctx context.Context, qn string) (*graph.Asset, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byQN[qnKey(tenant, qn)]
	if !ok {
		return nil, graph.ErrNotFound
	}
	a := s.assets[assetKey(tenant, id)]
	out := *a
	return &out, nil
}

func (s *Store) UpdateAsset(ctx context.Context, a *graph.Asset) (*graph.Asset, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if a == nil || a.ID == "" {
		return nil, graph.ErrInvalidArgument
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.assets[assetKey(tenant, a.ID)]
	if !ok {
		return nil, graph.ErrNotFound
	}
	cp := *a
	cp.TenantID = tenant
	cp.CreatedAt = existing.CreatedAt
	cp.UpdatedAt = s.now().UTC()

	if cp.QualifiedName != existing.QualifiedName {
		newKey := qnKey(tenant, cp.QualifiedName)
		if _, taken := s.byQN[newKey]; taken {
			return nil, fmt.Errorf("qualified_name %q: %w", cp.QualifiedName, graph.ErrConflict)
		}
		delete(s.byQN, qnKey(tenant, existing.QualifiedName))
		s.byQN[newKey] = cp.ID
	}
	s.assets[assetKey(tenant, cp.ID)] = &cp
	out := cp
	return &out, nil
}

func (s *Store) DeleteAsset(ctx context.Context, id string) error {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assets[assetKey(tenant, id)]
	if !ok {
		return graph.ErrNotFound
	}
	delete(s.assets, assetKey(tenant, id))
	delete(s.byQN, qnKey(tenant, a.QualifiedName))
	for eid, e := range s.edges {
		if e.TenantID == tenant && (e.SourceID == id || e.TargetID == id) {
			delete(s.edges, eid)
		}
	}
	return nil
}

func (s *Store) ListAssets(ctx context.Context, opts storage.ListAssetsOptions) ([]*graph.Asset, string, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, "", err
	}
	limit := opts.PageSize
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*graph.Asset
	for _, a := range s.assets {
		if a.TenantID != tenant {
			continue
		}
		if opts.Type != graph.AssetTypeUnspecified && a.Type != opts.Type {
			continue
		}
		cp := *a
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QualifiedName < out[j].QualifiedName })

	start := 0
	if opts.PageToken != "" {
		for i, a := range out {
			if a.ID == opts.PageToken {
				start = i + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(out) {
		end = len(out)
	}
	page := out[start:end]
	var next string
	if end < len(out) {
		next = page[len(page)-1].ID
	}
	return page, next, nil
}

func (s *Store) CreateEdge(ctx context.Context, e *graph.Edge) (*graph.Edge, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if e == nil || e.SourceID == "" || e.TargetID == "" || e.Kind == "" {
		return nil, graph.ErrInvalidArgument
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.assets[assetKey(tenant, e.SourceID)]; !ok {
		return nil, fmt.Errorf("source %q: %w", e.SourceID, graph.ErrNotFound)
	}
	if _, ok := s.assets[assetKey(tenant, e.TargetID)]; !ok {
		return nil, fmt.Errorf("target %q: %w", e.TargetID, graph.ErrNotFound)
	}
	cp := *e
	cp.TenantID = tenant
	if cp.ID == "" {
		cp.ID = newID()
	}
	cp.CreatedAt = s.now().UTC()
	s.edges[cp.ID] = &cp
	out := cp
	return &out, nil
}

func (s *Store) DeleteEdge(ctx context.Context, id string) error {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.edges[id]
	if !ok || e.TenantID != tenant {
		return graph.ErrNotFound
	}
	delete(s.edges, id)
	return nil
}

func (s *Store) Neighbors(ctx context.Context, assetID string, opts storage.NeighborsOptions) ([]*graph.Edge, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	limit := opts.Limit
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*graph.Edge
	for _, e := range s.edges {
		if e.TenantID != tenant {
			continue
		}
		if opts.Kind != "" && e.Kind != opts.Kind {
			continue
		}
		match := (opts.Outgoing && e.SourceID == assetID) ||
			(!opts.Outgoing && e.TargetID == assetID)
		if !match {
			continue
		}
		cp := *e
		out = append(out, &cp)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func assetKey(tenant, id string) string { return tenant + ":" + id }
func qnKey(tenant, qn string) string    { return tenant + ":" + strings.ToLower(qn) }

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("memory store: rand failed: %w", err))
	}
	return hex.EncodeToString(b[:])
}

package vectorstore

import (
	"context"
	"math"
	"sort"
	"sync"
)

// memoryStore is an in-process kNN index used for tests + embedded
// dev. Cosine similarity, O(N) scan per query — fine up to a few
// thousand vectors. For real workloads point at pgvector or a hosted
// backend.
type memoryStore struct {
	cfg  *Config
	mu   sync.RWMutex
	rows map[string]Vector
}

func newMemoryStore(cfg *Config) *memoryStore {
	return &memoryStore{cfg: cfg, rows: make(map[string]Vector)}
}

func (m *memoryStore) Name() string { return "memory:" + m.cfg.Name }

func (m *memoryStore) Upsert(_ context.Context, vecs []Vector) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range vecs {
		m.rows[v.ID] = v
	}
	return nil
}

func (m *memoryStore) Search(_ context.Context, opts SearchOpts) ([]SearchHit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	k := opts.K
	if k <= 0 {
		k = 10
	}
	hits := make([]SearchHit, 0, len(m.rows))
	for _, v := range m.rows {
		if !filterMatch(v.Metadata, opts.Filter) {
			continue
		}
		s := cosine(opts.Vector, v.Values)
		hits = append(hits, SearchHit{ID: v.ID, Score: s, Metadata: v.Metadata})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

func (m *memoryStore) Delete(_ context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.rows, id)
	}
	return nil
}

func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		na += ai * ai
		nb += bi * bi
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

func filterMatch(rowMeta, filter map[string]string) bool {
	for k, v := range filter {
		if rowMeta[k] != v {
			return false
		}
	}
	return true
}

package feedback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// MemoryRepo is the in-process Repo for tests + memory-mode dev.
type MemoryRepo struct {
	mu       sync.RWMutex
	items    map[string]*Item
	votes    map[string]map[string]bool // item_id -> voter_id -> true
	comments map[string][]*Comment      // item_id -> appended in order
}

func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{
		items:    make(map[string]*Item),
		votes:    make(map[string]map[string]bool),
		comments: make(map[string][]*Comment),
	}
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "fb-fallback"
	}
	return hex.EncodeToString(b[:])
}

func (r *MemoryRepo) Create(_ context.Context, it *Item) (*Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if it.ID == "" {
		it.ID = newID()
	}
	now := time.Now().UTC()
	if it.CreatedAt.IsZero() {
		it.CreatedAt = now
	}
	it.UpdatedAt = now
	if it.Status == "" {
		it.Status = StatusNew
	}
	if it.Priority == "" {
		it.Priority = PriorityNormal
	}
	cp := *it
	r.items[it.ID] = &cp
	return &cp, nil
}

func (r *MemoryRepo) Get(_ context.Context, id string) (*Item, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	it, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *it
	return &cp, nil
}

func (r *MemoryRepo) List(_ context.Context, opts ListOptions) ([]*Item, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Item, 0, len(r.items))
	for _, it := range r.items {
		if opts.TenantID != "" && it.TenantID != opts.TenantID {
			continue
		}
		if opts.Status != "" && it.Status != opts.Status {
			continue
		}
		if opts.Type != "" && it.Type != opts.Type {
			continue
		}
		if opts.Priority != "" && it.Priority != opts.Priority {
			continue
		}
		if opts.Assignee != "" && it.AssigneeID != opts.Assignee {
			continue
		}
		cp := *it
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryRepo) Update(_ context.Context, it *Item) (*Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.items[it.ID]
	if !ok {
		return nil, ErrNotFound
	}
	existing.Type = it.Type
	existing.Title = it.Title
	existing.Body = it.Body
	existing.Priority = it.Priority
	existing.Status = it.Status
	existing.AssigneeID = it.AssigneeID
	existing.UpdatedAt = time.Now().UTC()
	cp := *existing
	return &cp, nil
}

func (r *MemoryRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return ErrNotFound
	}
	delete(r.items, id)
	delete(r.votes, id)
	delete(r.comments, id)
	return nil
}

func (r *MemoryRepo) Vote(_ context.Context, itemID, voterID string) (int, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	it, ok := r.items[itemID]
	if !ok {
		return 0, false, ErrNotFound
	}
	v, ok := r.votes[itemID]
	if !ok {
		v = make(map[string]bool)
		r.votes[itemID] = v
	}
	if v[voterID] {
		return it.VoteCount, true, nil
	}
	v[voterID] = true
	it.VoteCount++
	it.UpdatedAt = time.Now().UTC()
	return it.VoteCount, false, nil
}

func (r *MemoryRepo) Unvote(_ context.Context, itemID, voterID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	it, ok := r.items[itemID]
	if !ok {
		return 0, ErrNotFound
	}
	v, ok := r.votes[itemID]
	if !ok || !v[voterID] {
		return it.VoteCount, nil
	}
	delete(v, voterID)
	it.VoteCount--
	if it.VoteCount < 0 {
		it.VoteCount = 0
	}
	it.UpdatedAt = time.Now().UTC()
	return it.VoteCount, nil
}

func (r *MemoryRepo) VotedBy(_ context.Context, voterID string, itemIDs []string) (map[string]bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]bool, len(itemIDs))
	for _, id := range itemIDs {
		out[id] = r.votes[id][voterID]
	}
	return out, nil
}

func (r *MemoryRepo) AddComment(_ context.Context, c *Comment) (*Comment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[c.ItemID]; !ok {
		return nil, ErrNotFound
	}
	if c.ID == "" {
		c.ID = newID()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	cp := *c
	r.comments[c.ItemID] = append(r.comments[c.ItemID], &cp)
	r.items[c.ItemID].UpdatedAt = cp.CreatedAt
	return &cp, nil
}

func (r *MemoryRepo) ListComments(_ context.Context, itemID string) ([]*Comment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Comment, 0, len(r.comments[itemID]))
	for _, c := range r.comments[itemID] {
		cp := *c
		out = append(out, &cp)
	}
	return out, nil
}

var _ Repo = (*MemoryRepo)(nil)

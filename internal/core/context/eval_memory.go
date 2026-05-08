package context

import (
	"context"
	"sync"
)

// MemoryEvalSink is an in-process EvalSink for tests and the embedded
// dev mode. Records are kept in append order; concurrent Record calls are
// safe.
type MemoryEvalSink struct {
	mu      sync.Mutex
	records []Eval
}

func NewMemoryEvalSink() *MemoryEvalSink { return &MemoryEvalSink{} }

func (m *MemoryEvalSink) Record(_ context.Context, e *Eval) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, *e)
	return nil
}

// All returns a snapshot of recorded evals in insertion order.
func (m *MemoryEvalSink) All() []Eval {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Eval, len(m.records))
	copy(out, m.records)
	return out
}

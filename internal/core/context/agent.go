// Package context implements Plowered's AI agents that fill gaps in the
// metadata graph. Every agent runs queue-driven (never blocking user
// requests) and writes a record to the EvalSink for every generation so the
// platform can compute approval rates and groundedness over time.
//
// Pipeline per agent invocation:
//
//	build prompt → call provider → parse output → record eval → return proposal
//
// The actual write back to the graph is the caller's responsibility, gated
// behind human review (see SECURITY.md §8).
package context

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/Satyaamm/plowered/internal/core/graph"
)

// Agent is the surface every Context Agent implements. Each agent decides
// whether the LLM is needed (e.g. QualityAgent is algorithmic) and what its
// proposal looks like.
type Agent interface {
	Name() string
	Version() string
}

// Disposition is the human reviewer's decision on a generation.
type Disposition string

const (
	DispositionPending  Disposition = "pending"
	DispositionApproved Disposition = "approved"
	DispositionEdited   Disposition = "edited"
	DispositionRejected Disposition = "rejected"
	DispositionExpired  Disposition = "expired"
)

// Eval is one record of an agent generation. Persisted to the `evals` table
// in storage; held in memory by tests.
type Eval struct {
	ID            string
	TenantID      string
	AssetID       string // optional: for asset-scoped agents
	Agent         string
	AgentVersion  string
	Model         string
	PromptVersion string
	InputHash     string
	Output        string
	Disposition   Disposition
	ReviewerID    string
	Groundedness  float32
	LatencyMS     int64
	TokensIn      int
	TokensOut     int
	CreatedAt     time.Time
}

// EvalSink persists Eval records. Concrete impls go in storage/<backend>;
// tests use the in-memory MemoryEvalSink.
type EvalSink interface {
	Record(ctx context.Context, e *Eval) error
}

// HashInput returns a stable hex digest for an arbitrary set of inputs. Use
// this to fill Eval.InputHash so re-runs of the same input can be deduped or
// compared.
func HashInput(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// AssetSummary summarizes an asset for prompt construction. Handlers build
// this from a Store lookup before calling an agent so the agent layer stays
// storage-agnostic.
type AssetSummary struct {
	Asset           *graph.Asset
	UpstreamSamples []*graph.Asset // up to N immediate upstream assets
	DownstreamCount int
	OwnerCount      int
}

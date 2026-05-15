// Package describer generates auto-description suggestions for catalog
// assets using a tenant's configured chat provider. The user reviews
// the suggestion in the UI before it's saved — Describer never writes
// to assets.description itself. That's a deliberate boundary: the AI
// proposes, the human disposes.
//
// The service composes:
//
//	SchemaContextBuilder  — facts about the asset
//	aiprovider.Resolver   — tenant's chat provider
//	Log                   — audit trail of every suggestion
//
// Failure modes degrade gracefully: a missing primary AI provider is
// surfaced as ErrNoProvider (HTTP 400 from the handler), not 500.
package describer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/aictx"
	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/pkg/llm"
)

// ErrNoProvider is returned when the tenant has not configured a
// primary chat provider. The HTTP layer maps this to a 400 with
// guidance, not a 500.
var ErrNoProvider = aiprovider.ErrNoPrimary

// Suggestion is one auto-generated description, fully audited.
type Suggestion struct {
	AssetID      string    `json:"asset_id"`
	Suggestion   string    `json:"suggestion"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	GeneratedAt  time.Time `json:"generated_at"`
}

// Log persists every suggestion for audit and eval. The Postgres
// implementation writes to ai_descriptions_log.
type Log interface {
	Record(ctx context.Context, s *Suggestion, tenantID, generatedBy string) error
	MarkAccepted(ctx context.Context, tenantID, suggestionID string) error
}

// Service is the only type the HTTP layer should hold.
//
// Why dependencies are interfaces, not concrete: the AI resolver is
// already a tiny interface-ready type; the context builder is one;
// the log is one. The result is that this service can be unit-tested
// with stubbed deps that return canned suggestions — no real API key
// burned in CI.
type Service struct {
	Context  *aictx.Builder
	Resolver *aiprovider.Resolver
	Log      Log
	Logger   *slog.Logger

	// MaxOutputTokens caps generation length. Defaults to 300 which
	// translates to ~2 dense sentences — the prompt asks for brevity
	// but the model needs a hard cap.
	MaxOutputTokens int
}

// Suggest produces one description suggestion for the asset. It does
// NOT modify the asset row — the UI's Save button issues a separate
// PATCH /v1/assets/{id} that copies the suggestion into description_ai.
func (s *Service) Suggest(ctx context.Context, tenantID, assetID, generatedBy string) (*Suggestion, error) {
	if s.Context == nil || s.Resolver == nil || s.Log == nil {
		return nil, errors.New("describer: service not fully configured")
	}
	tctx, err := s.Context.BuildForTable(ctx, tenantID, assetID)
	if err != nil {
		// Fall back to BuildForColumn — caller might have handed us a
		// column asset_id. The Builder's BuildForColumn already
		// returns parent-table context when available.
		tctx, err = s.Context.BuildForColumn(ctx, tenantID, assetID)
		if err != nil {
			return nil, fmt.Errorf("build context: %w", err)
		}
	}
	provider, err := s.Resolver.Primary(ctx, tenantID, aiprovider.CapChat)
	if err != nil {
		return nil, err
	}
	prompt := tctx.Render()
	maxTok := s.MaxOutputTokens
	if maxTok <= 0 {
		maxTok = 300
	}
	resp, err := provider.Generate(ctx, llm.GenerateRequest{
		System:      systemPrompt,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: prompt}},
		MaxTokens:   maxTok,
		Temperature: 0.2, // low — we want consistent, factual descriptions
	})
	if err != nil {
		return nil, fmt.Errorf("llm generate: %w", err)
	}
	out := &Suggestion{
		AssetID:      assetID,
		Suggestion:   strings.TrimSpace(resp.Content),
		Model:        resp.Model,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		GeneratedAt:  time.Now().UTC(),
	}
	if err := s.Log.Record(ctx, out, tenantID, generatedBy); err != nil {
		// Log failure is non-fatal — the suggestion still ships to the
		// caller; we just lose the audit row.
		s.logger().WarnContext(ctx, "describer: log record", "err", err)
	}
	return out, nil
}

// systemPrompt is the constant instruction every suggestion uses.
// Lives as a package-level const so prompt diffs are reviewable in
// git history.
const systemPrompt = `You write concise descriptions for entries in a data catalog.

Rules:
- 1 to 2 sentences, max 240 characters.
- Describe WHAT the table/column represents, not how it was made.
- Use plain English. No marketing language, no "this table stores".
- If the columns suggest a domain (orders, users, events), name it.
- If sample values are present, weave one in only if it clarifies.
- Never invent columns or relationships that aren't in the context.
- Output the description text only — no preamble, no quotes, no markdown.`

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// Package llm is a provider-agnostic abstraction for the language-model and
// embedding providers Plowered consumes. Concrete implementations live in
// sub-packages and are swapped via configuration; the rest of the codebase
// only ever depends on this package.
//
// Design principles:
//
//   - Stateless requests; no provider-managed conversation state.
//   - Structured output is first-class via JSONSchema.
//   - Cost telemetry (InputTokens, OutputTokens) is mandatory in responses
//     so the platform can enforce per-tenant budgets.
//   - Embedding and Generate live behind one interface so swapping providers
//     stays a single configuration change.
package llm

import (
	"context"
	"errors"
)

// Provider is the surface that every LLM backend implements.
type Provider interface {
	// Generate returns a single completion for the given request.
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)

	// Embed returns one vector per input text. Backends that do not support
	// embeddings should return ErrEmbedUnsupported rather than approximate.
	Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error)

	// Name identifies the provider for telemetry. Stable across runs.
	Name() string
}

// Role is "user" or "assistant" for now; system prompts go on
// GenerateRequest.System rather than as a Message so providers that don't
// have a `system` role can map it explicitly.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type GenerateRequest struct {
	Model       string
	System      string
	Messages    []Message
	MaxTokens   int
	Temperature float64

	// JSONSchema, when non-empty, asks the provider for structured JSON
	// output conforming to the supplied JSON Schema. Providers that don't
	// support structured output may emulate via prompt and best-effort
	// validation.
	JSONSchema []byte

	// StopSequences is the optional list of strings at which generation
	// should halt.
	StopSequences []string
}

type GenerateResponse struct {
	Content      string
	Model        string
	InputTokens  int
	OutputTokens int

	// StopReason: "end_turn", "max_tokens", "stop_sequence", "error".
	StopReason string
}

type EmbedRequest struct {
	Model string
	Texts []string
}

type EmbedResponse struct {
	Vectors [][]float32
	Model   string
	Tokens  int
}

// Sentinel errors. Callers should errors.Is / errors.As to handle.
var (
	ErrEmbedUnsupported = errors.New("llm: provider does not support embeddings")
	ErrUnknownModel     = errors.New("llm: unknown model")
	ErrRateLimited      = errors.New("llm: rate limited")
	ErrContextLength    = errors.New("llm: context length exceeded")
	ErrSafetyBlocked    = errors.New("llm: provider blocked output for safety")
)

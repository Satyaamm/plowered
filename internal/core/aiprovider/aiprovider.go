// Package aiprovider is the BYOM (bring-your-own-model) configuration
// layer. A tenant admin registers one or more provider configs — pick a
// kind (Anthropic / OpenAI / DeepSeek / OpenAI-compatible), supply an
// API key + model + optional base URL — and the platform stores the
// config row in Postgres and the API key sealed in the secrets vault.
//
// At runtime the router resolves a config by ID, fetches its secret,
// constructs a fresh llm.Provider via Build, and either runs a real
// request (search reindex, glossary auto-write) or — for the "Test"
// button on the settings page — a credential probe.
//
// Why per-tenant configs instead of a single env-driven provider:
//   - Customers want to use their own API quotas; we don't subsidize
//     their model bill.
//   - A multi-tenant SaaS deployment must keep one tenant's API key
//     out of another tenant's blast radius.
//   - Some tenants want Anthropic for chat + OpenAI for embeddings;
//     each config row picks one kind + one model, so a tenant can stack
//     multiple configs and route per-feature.
package aiprovider

import (
	"context"
	"errors"
	"time"
)

// Kind names a supported provider family. Add a const here when you ship
// a new adapter; the registry in adapters.go maps Kind → factory.
type Kind string

const (
	KindAnthropic Kind = "anthropic"
	KindOpenAI    Kind = "openai"
	KindDeepSeek  Kind = "deepseek"
	// KindCustom is for OpenAI-compatible self-hosted/proxy endpoints
	// (Ollama, LiteLLM, OpenRouter, vLLM, etc.). The adapter reuses the
	// OpenAI wire format and lets BaseURL override the API host.
	KindCustom Kind = "openai-compatible"
)

// AllKinds is the ordered list the settings UI offers in its dropdown.
// Order = "most common first."
var AllKinds = []Kind{KindAnthropic, KindOpenAI, KindDeepSeek, KindCustom}

// Capability bits surface to the UI so users see at a glance whether a
// config can serve chat or embeddings (or both).
type Capability string

const (
	CapChat  Capability = "chat"
	CapEmbed Capability = "embed"
)

// Purpose names a slot a tenant can assign a config to. v0 ships two:
// default-chat for glossary auto-write, asset descriptions, etc.; and
// default-embed for semantic search. A config can be "primary" for one
// or both purposes; the resolver looks up by purpose.
type Purpose string

const (
	PurposeDefaultChat  Purpose = "default_chat"
	PurposeDefaultEmbed Purpose = "default_embed"
)

// Config is one provider entry on a tenant's BYOM list. The API key
// itself is never stored on this row — only the SecretURN that points
// into the vault.
type Config struct {
	ID         string
	TenantID   string
	Kind       Kind
	Name       string  // user-facing nickname, e.g. "Claude Sonnet 4.6"
	Model      string  // provider-specific model id
	BaseURL    string  // optional; required for KindCustom
	SecretURN  string  // vault key for the API key bytes
	IsPrimary  bool    // marked as the tenant default for its capability
	Capability Capability
	CreatedAt  time.Time
	UpdatedAt  time.Time
	LastTestedAt time.Time
	LastTestOK  bool
	LastTestErr string
}

// Redacted is the wire form the API returns. It strips anything
// sensitive (secret URN, full base URL when private) and adds a couple
// status fields the UI surfaces.
type Redacted struct {
	ID           string     `json:"id"`
	Kind         Kind       `json:"kind"`
	Name         string     `json:"name"`
	Model        string     `json:"model"`
	BaseURL      string     `json:"base_url,omitempty"`
	IsPrimary    bool       `json:"is_primary"`
	Capability   Capability `json:"capability"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastTestedAt *time.Time `json:"last_tested_at,omitempty"`
	LastTestOK   bool       `json:"last_test_ok"`
	LastTestErr  string     `json:"last_test_error,omitempty"`
}

func (c *Config) Redact() Redacted {
	r := Redacted{
		ID:          c.ID,
		Kind:        c.Kind,
		Name:        c.Name,
		Model:       c.Model,
		BaseURL:     c.BaseURL,
		IsPrimary:   c.IsPrimary,
		Capability:  c.Capability,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
		LastTestOK:  c.LastTestOK,
		LastTestErr: c.LastTestErr,
	}
	if !c.LastTestedAt.IsZero() {
		t := c.LastTestedAt
		r.LastTestedAt = &t
	}
	return r
}

// Repo is the persistence interface. Postgres impl lives in
// internal/storage/postgres.
type Repo interface {
	Create(ctx context.Context, c *Config) (*Config, error)
	Get(ctx context.Context, tenantID, id string) (*Config, error)
	List(ctx context.Context, tenantID string) ([]*Config, error)
	Update(ctx context.Context, c *Config) (*Config, error)
	Delete(ctx context.Context, tenantID, id string) error
	// MarkTested records the outcome of a credential probe so the UI
	// can render a green/red badge per config without re-testing on
	// every page load.
	MarkTested(ctx context.Context, tenantID, id string, ok bool, errMsg string) error
	// SetPrimary atomically clears IsPrimary on every config in the
	// (tenant, capability) bucket then sets it on the chosen one.
	SetPrimary(ctx context.Context, tenantID, id string) error
}

// ErrNotFound is returned when an id doesn't exist (or belongs to a
// different tenant).
var ErrNotFound = errors.New("aiprovider: not found")

// SecretURNFor builds the vault URN for a config's API key. The shape
// matches the urn:plowered:* convention the rest of the codebase uses.
func SecretURNFor(configID string) string {
	return "urn:plowered:aiprovider:" + configID
}

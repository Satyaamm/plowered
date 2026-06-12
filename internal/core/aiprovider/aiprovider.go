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
	// First-party REST providers — each has its own wire format and adapter.
	KindAnthropic Kind = "anthropic"
	KindOpenAI    Kind = "openai"
	KindGemini    Kind = "gemini"        // Google AI Studio (api key)
	KindAzureOAI  Kind = "azure-openai"  // Azure-hosted OpenAI (deployment + api-version)
	KindCohere    Kind = "cohere"        // chat + embed + rerank
	KindVoyage    Kind = "voyage"        // embed-only
	KindBedrock   Kind = "bedrock"       // AWS Bedrock (IAM/SigV4)
	KindVertex    Kind = "vertex"        // GCP Vertex AI (service account / WIF)

	// OpenAI-compatible providers — reuse the openaiProvider adapter
	// with a per-kind default base URL. Promoting them to named kinds
	// gets us per-provider analytics + a saner UI than "type a URL."
	KindDeepSeek    Kind = "deepseek"
	KindMistral     Kind = "mistral"
	KindGroq        Kind = "groq"
	KindTogether    Kind = "together"
	KindFireworks   Kind = "fireworks"
	KindPerplexity  Kind = "perplexity"
	KindXAI         Kind = "xai"
	KindOllama      Kind = "ollama"

	// KindCustom is the catch-all for any other OpenAI-compatible
	// endpoint (LiteLLM, OpenRouter, vLLM, your own gateway). Requires
	// BaseURL to be set; the adapter reuses the OpenAI wire format.
	KindCustom Kind = "openai-compatible"
)

// AllKinds is the ordered list the settings UI offers in its dropdown.
// Order = "common frontier first, then cloud-platform, then specialised,
// then long-tail OpenAI-compatible, then catch-all."
var AllKinds = []Kind{
	KindAnthropic, KindOpenAI, KindGemini,
	KindAzureOAI, KindBedrock, KindVertex,
	KindCohere, KindVoyage, KindMistral,
	KindGroq, KindTogether, KindFireworks,
	KindPerplexity, KindXAI, KindDeepSeek,
	KindOllama, KindCustom,
}

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
//
// Per-kind notes for the optional fields below:
//
//	KindAzureOAI : BaseURL = "https://<resource>.openai.azure.com",
//	               Deployment = the deployment name in your resource,
//	               APIVersion = e.g. "2024-06-01". Auth header is api-key.
//	KindBedrock  : Region required (e.g. "us-east-1"). Auth is AWS SigV4
//	               via the standard credential chain (env, ~/.aws,
//	               instance profile, IRSA). SecretURN may carry a JSON
//	               blob {access_key_id, secret_access_key, session_token}
//	               for explicit static creds.
//	KindVertex   : Project + Location required (e.g. "us-central1").
//	               SecretURN points at a service-account JSON blob; auth
//	               uses the google.Credentials default chain otherwise.
//	KindOllama   : BaseURL defaults to http://localhost:11434/v1 — set
//	               PLOWERED_ALLOW_PRIVATE_AI_HOSTS=1 to bypass the SSRF
//	               guard for local dev.
type Config struct {
	ID         string
	TenantID   string
	Kind       Kind
	Name       string  // user-facing nickname, e.g. "Claude Sonnet 4.6"
	Model      string  // provider-specific model id
	BaseURL    string  // optional; required for KindCustom + KindAzureOAI + KindOllama
	SecretURN  string  // vault key for the API key bytes (or AWS creds JSON, GCP SA JSON)
	IsPrimary  bool    // marked as the tenant default for its capability
	Capability Capability

	// Azure OpenAI specific.
	Deployment string // resource deployment name
	APIVersion string // e.g. "2024-06-01"

	// AWS Bedrock specific.
	Region string // e.g. "us-east-1"

	// GCP Vertex AI specific.
	Project  string // GCP project ID
	Location string // e.g. "us-central1"

	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastTestedAt time.Time
	LastTestOK   bool
	LastTestErr  string
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
	Deployment   string     `json:"deployment,omitempty"`
	APIVersion   string     `json:"api_version,omitempty"`
	Region       string     `json:"region,omitempty"`
	Project      string     `json:"project,omitempty"`
	Location     string     `json:"location,omitempty"`
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
		Deployment:  c.Deployment,
		APIVersion:  c.APIVersion,
		Region:      c.Region,
		Project:     c.Project,
		Location:    c.Location,
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

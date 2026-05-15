package aiprovider

import (
	"context"
	"errors"
	"fmt"

	"github.com/Satyaamm/plowered/internal/core/secrets"
	"github.com/Satyaamm/plowered/pkg/llm"
)

// ErrNoPrimary is returned when a tenant has not designated a primary
// config for the requested capability. Callers should surface this as a
// friendly "configure an AI provider first" message to the UI rather
// than a 500.
var ErrNoPrimary = errors.New("aiprovider: no primary provider configured for this capability")

// Resolver turns (tenant, purpose) into a ready-to-use llm.Provider.
// It's the single dependency every AI-powered feature should hold: the
// Profile / Describer / Asker services don't know about Repos or
// Vaults — they ask the resolver and get back something that can
// Generate(). This is the AICompletionClient seam the design called
// out, expressed against the existing llm.Provider so we don't add a
// parallel abstraction.
//
// Why not directly inject *Config into services: a config's secret
// can be rotated; building the Provider per-call ensures we always
// pick up the live key. The build is cheap — no network I/O until
// Generate runs.
type Resolver struct {
	Repo  Repo
	Vault secrets.Vault
}

// NewResolver wires the two dependencies. Returns nil if either is
// missing so callers can fail fast.
func NewResolver(repo Repo, vault secrets.Vault) *Resolver {
	if repo == nil || vault == nil {
		return nil
	}
	return &Resolver{Repo: repo, Vault: vault}
}

// Primary picks the tenant's primary config for the given capability
// (chat or embed), reads its API key from the vault, and builds a live
// llm.Provider. Returns ErrNoPrimary if no config is marked primary
// for the capability — this is a user-facing setup error, not a bug.
//
// Capability selection happens here rather than in the call-sites so a
// feature like "auto-describe" doesn't have to know whether to look
// for chat or embed.
func (r *Resolver) Primary(ctx context.Context, tenantID string, cap Capability) (llm.Provider, error) {
	configs, err := r.Repo.List(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	var chosen *Config
	for _, c := range configs {
		if c.Capability != cap {
			continue
		}
		if c.IsPrimary {
			chosen = c
			break
		}
		// Fallback: first matching capability if none are marked primary.
		// Saves the user from a hard error when they only have one
		// config; they probably meant for it to be primary.
		if chosen == nil {
			chosen = c
		}
	}
	if chosen == nil {
		return nil, ErrNoPrimary
	}
	apiKey, err := r.Vault.Get(ctx, tenantID, chosen.SecretURN)
	if err != nil {
		return nil, fmt.Errorf("read api key: %w", err)
	}
	return Build(chosen, apiKey)
}

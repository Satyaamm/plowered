package aiprovider

import (
	"context"
	"errors"

	"github.com/Satyaamm/plowered/pkg/llm"
)

// CloudDriver is the seam between aiprovider and a cloud-SDK-backed
// provider implementation. Bedrock + Vertex live in subpackages under
// internal/adapters/ so the AWS / GCP SDKs can be dropped from a
// build by removing the blank-import in cmd/plowered/main.go (mirrors
// the BigQuery pattern in internal/adapters/bigquery_driver).
type CloudDriver interface {
	Build(cfg *Config, secret []byte) (llm.Provider, error)
	Test(ctx context.Context, cfg *Config, secret []byte) error
}

// ErrDriverNotInstalled is returned when a cloud-backed kind is
// requested but no driver registered. The error message tells the
// operator which named import to wire.
type ErrDriverNotInstalled struct{ Kind Kind }

func (e ErrDriverNotInstalled) Error() string {
	return "aiprovider: " + string(e.Kind) +
		" driver not installed (add the named import in cmd/plowered/main.go)"
}

var (
	activeBedrock CloudDriver
	activeVertex  CloudDriver
)

// SetBedrockDriver registers the AWS Bedrock implementation. Call it
// from an init() in your wiring package.
func SetBedrockDriver(d CloudDriver) { activeBedrock = d }

// SetVertexDriver registers the GCP Vertex AI implementation.
func SetVertexDriver(d CloudDriver) { activeVertex = d }

// newBedrockProvider returns the registered Bedrock driver's provider
// or ErrDriverNotInstalled if no driver was wired.
func newBedrockProvider(cfg *Config, apiKey []byte) (llm.Provider, error) {
	if activeBedrock == nil {
		return nil, ErrDriverNotInstalled{Kind: KindBedrock}
	}
	return activeBedrock.Build(cfg, apiKey)
}

func newVertexProvider(cfg *Config, apiKey []byte) (llm.Provider, error) {
	if activeVertex == nil {
		return nil, ErrDriverNotInstalled{Kind: KindVertex}
	}
	return activeVertex.Build(cfg, apiKey)
}

func testBedrock(ctx context.Context, cfg *Config, apiKey []byte) error {
	if activeBedrock == nil {
		return ErrDriverNotInstalled{Kind: KindBedrock}
	}
	return activeBedrock.Test(ctx, cfg, apiKey)
}

func testVertex(ctx context.Context, cfg *Config, apiKey []byte) error {
	if activeVertex == nil {
		return ErrDriverNotInstalled{Kind: KindVertex}
	}
	return activeVertex.Test(ctx, cfg, apiKey)
}

// _ keeps llm in scope for the seam — drivers below take/return llm
// types and we want a compile failure if the package goes away.
var _ llm.Provider = (llm.Provider)(nil)

// _ silences unused-error warnings in builds where neither driver is
// wired; the typed sentinel still works via errors.Is.
var _ = errors.New

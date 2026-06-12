// Package vectorstore is the destination interface for embedding
// vectors. Today the semantic-search code writes directly to the
// asset_embeddings Postgres table; this package introduces a Store
// interface so tenants can route to an external vector DB (Pinecone,
// Weaviate, Qdrant) or stay on pgvector / asset_embeddings.
//
// Why split this out of aiprovider:
//
//   - LLM providers GENERATE vectors. Vector stores STORE + RETRIEVE
//     them. Treating them as one concept (or one config row) muddles
//     "Cohere can both embed and is a vector DB" — which is false.
//   - Vector store choice is independent of LLM choice. A tenant might
//     embed with Voyage and store in Pinecone; another might embed
//     with OpenAI and store in pgvector locally.
//   - Each store has very different config (Pinecone: api key + env;
//     Weaviate: url + api key + class name; Qdrant: url + api key +
//     collection; pgvector: just a flag — reuses asset_embeddings).
//
// Coverage status:
//   - pgvector + asset_embeddings (default, in-process): scaffolded
//     here; the existing search.Indexer continues to own the writes
//     until the resolver is wired in a follow-up.
//   - Pinecone / Weaviate / Qdrant adapters live in subpackages under
//     internal/adapters/<name>_vectorstore so the SDK deps are opt-in.
package vectorstore

import (
	"context"
	"errors"
	"time"
)

// Kind names a vector-store backend.
type Kind string

const (
	KindPgvector Kind = "pgvector" // pgvector extension on the catalog Postgres
	KindMemory   Kind = "memory"   // in-process; tests + embedded mode
	KindPinecone Kind = "pinecone"
	KindWeaviate Kind = "weaviate"
	KindQdrant   Kind = "qdrant"
)

// AllKinds is the wizard order — most common first.
var AllKinds = []Kind{KindPgvector, KindPinecone, KindWeaviate, KindQdrant, KindMemory}

// Config is one tenant's vector-store entry. Per-kind notes for the
// optional fields below:
//
//	Pinecone : Endpoint = "https://<index>-<project>.svc.<env>.pinecone.io",
//	           IndexName = the index name, ApiKey via SecretURN.
//	Weaviate : Endpoint = base URL, ClassName = your class, ApiKey
//	           optional (anonymous Weaviate accepted but not recommended).
//	Qdrant   : Endpoint = base URL, Collection name, ApiKey via SecretURN.
//	Pgvector : nothing extra — uses the platform's primary Postgres.
type Config struct {
	ID         string
	TenantID   string
	Kind       Kind
	Name       string
	Endpoint   string
	IndexName  string // pinecone
	ClassName  string // weaviate
	Collection string // qdrant
	Dimension  int    // vector size declared at index/collection creation
	SecretURN  string // API key for hosted stores

	IsPrimary bool

	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastTestedAt time.Time
	LastTestOK   bool
	LastTestErr  string
}

// Vector is a single embedding bound to an asset/text reference.
type Vector struct {
	ID       string            // typically the asset id or chunk id
	Values   []float32
	Metadata map[string]string // facets used by the search filter
}

// SearchOpts shapes a kNN query.
type SearchOpts struct {
	K        int
	Vector   []float32
	Filter   map[string]string
	TenantID string
}

// SearchHit is one match.
type SearchHit struct {
	ID       string            `json:"id"`
	Score    float32           `json:"score"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Store is the per-tenant write + search surface. Implementations are
// stateless — they hold a client + config and serialise via the
// underlying SDK.
type Store interface {
	// Name returns "kind:identifier" for log lines + telemetry.
	Name() string
	// Upsert writes (or replaces) the vectors. The Vector.ID is the
	// stable key.
	Upsert(ctx context.Context, vecs []Vector) error
	// Search returns the top-K matches for opts.Vector.
	Search(ctx context.Context, opts SearchOpts) ([]SearchHit, error)
	// Delete removes vectors by id.
	Delete(ctx context.Context, ids []string) error
}

// Driver is the seam for backends whose SDKs live in optional
// sub-packages. Pinecone / Weaviate / Qdrant register themselves on
// init via SetPineconeDriver etc.; the default chain bypasses to
// pgvector / memory which compile in by default.
type Driver interface {
	Build(cfg *Config, secret []byte) (Store, error)
	Test(ctx context.Context, cfg *Config, secret []byte) error
}

// ErrDriverNotInstalled is returned when a hosted-store kind is
// requested but no driver registered.
type ErrDriverNotInstalled struct{ Kind Kind }

func (e ErrDriverNotInstalled) Error() string {
	return "vectorstore: " + string(e.Kind) +
		" driver not installed (add the named import in cmd/plowered/main.go)"
}

var (
	activePinecone Driver
	activeWeaviate Driver
	activeQdrant   Driver
)

func SetPineconeDriver(d Driver) { activePinecone = d }
func SetWeaviateDriver(d Driver) { activeWeaviate = d }
func SetQdrantDriver(d Driver)   { activeQdrant = d }

// Build resolves a Config to a Store. The pgvector + memory backends
// are compiled in by default; everything else routes through the
// driver seam above.
func Build(cfg *Config, secret []byte) (Store, error) {
	if cfg == nil {
		return nil, errors.New("vectorstore: nil config")
	}
	switch cfg.Kind {
	case KindPgvector:
		if activePgvectorStore == nil {
			return nil, errors.New("vectorstore: pgvector backend not registered (call SetPgvectorStore in your wiring package)")
		}
		return NewPgvectorStore(cfg, activePgvectorStore), nil
	case KindMemory:
		return newMemoryStore(cfg), nil
	case KindPinecone:
		if activePinecone == nil {
			return nil, ErrDriverNotInstalled{Kind: KindPinecone}
		}
		return activePinecone.Build(cfg, secret)
	case KindWeaviate:
		if activeWeaviate == nil {
			return nil, ErrDriverNotInstalled{Kind: KindWeaviate}
		}
		return activeWeaviate.Build(cfg, secret)
	case KindQdrant:
		if activeQdrant == nil {
			return nil, ErrDriverNotInstalled{Kind: KindQdrant}
		}
		return activeQdrant.Build(cfg, secret)
	}
	return nil, errors.New("vectorstore: unknown kind " + string(cfg.Kind))
}

// Test runs a credential / connectivity probe.
func Test(ctx context.Context, cfg *Config, secret []byte) error {
	if cfg == nil {
		return errors.New("vectorstore: nil config")
	}
	switch cfg.Kind {
	case KindMemory, KindPgvector:
		return nil // local; nothing to probe
	case KindPinecone:
		if activePinecone == nil {
			return ErrDriverNotInstalled{Kind: KindPinecone}
		}
		return activePinecone.Test(ctx, cfg, secret)
	case KindWeaviate:
		if activeWeaviate == nil {
			return ErrDriverNotInstalled{Kind: KindWeaviate}
		}
		return activeWeaviate.Test(ctx, cfg, secret)
	case KindQdrant:
		if activeQdrant == nil {
			return ErrDriverNotInstalled{Kind: KindQdrant}
		}
		return activeQdrant.Test(ctx, cfg, secret)
	}
	return errors.New("vectorstore: unknown kind " + string(cfg.Kind))
}

// SecretURNFor matches the urn:plowered:vectorstore:<id> shape the
// rest of the codebase uses for vault keys.
func SecretURNFor(configID string) string {
	return "urn:plowered:vectorstore:" + configID
}

// Repo is the persistence surface for tenant config rows. Memory +
// Postgres impls follow the ai_provider_configs shape so the wizard +
// vault patterns stay symmetric.
type Repo interface {
	Create(ctx context.Context, c *Config) (*Config, error)
	Get(ctx context.Context, tenantID, id string) (*Config, error)
	List(ctx context.Context, tenantID string) ([]*Config, error)
	Update(ctx context.Context, c *Config) (*Config, error)
	Delete(ctx context.Context, tenantID, id string) error
	MarkTested(ctx context.Context, tenantID, id string, ok bool, errMsg string) error
	SetPrimary(ctx context.Context, tenantID, id string) error
	// Primary returns the tenant's currently-active config, or
	// ErrNotFound when none is set. Callers fall back to pgvector +
	// the asset_embeddings table when nothing is configured.
	Primary(ctx context.Context, tenantID string) (*Config, error)
	// SetSecretURN writes the vault URN onto a row after Create
	// returns the generated UUID (the URN incorporates the id).
	SetSecretURN(ctx context.Context, tenantID, id, urn string) error
}

// ErrNotFound is returned when an id doesn't exist (or belongs to a
// different tenant).
var ErrNotFound = errs("vectorstore: not found")

type errs string

func (e errs) Error() string { return string(e) }

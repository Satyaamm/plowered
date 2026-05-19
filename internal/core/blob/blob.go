// Package blob is the durable object-storage abstraction. Plowered
// produces artifacts that don't belong in Postgres — migration run
// checkpoints, AI prompt/response transcripts, classify-preview
// reports, crawl tree snapshots. Putting megabytes-of-JSON rows in a
// relational table is the wrong shape: writes are append-only, reads
// are by key, and the size grows linearly with usage. Object storage
// (S3 + friends) is the right home.
//
// Why an interface and not "just call S3":
//
//   - Tests must not hit AWS. In-memory backend handles every test
//     path with no creds + no flakiness.
//   - Local dev shouldn't require credentials. The InMem backend
//     also serves single-process dev when the operator hasn't wired
//     S3 yet.
//   - On-prem deployments will need MinIO (S3-compatible) or a
//     filesystem-backed implementation. The interface stays stable;
//     the backend swaps via config.
//
// Key shape is the caller's responsibility — Plowered convention is
// "<tenant_id>/<feature>/<resource_id>/<filename>" but the store
// doesn't enforce it. Keep paths predictable so manual S3 console
// inspection during incidents is easy.
package blob

import (
	"context"
	"errors"
	"io"
	"time"
)

// ObjectStore is the surface every backend implements. All methods
// accept ctx so deadlines and cancellation propagate to the network.
type ObjectStore interface {
	// Put writes body to key. Returns the object's URI for storage in
	// downstream catalog rows. Existing objects at key are overwritten.
	Put(ctx context.Context, key string, body io.Reader) (string, error)

	// Get returns a reader for the object at key. Caller MUST close it.
	// Returns ErrNotFound when the key doesn't exist.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the object at key. Not-found is not an error —
	// deletion is idempotent.
	Delete(ctx context.Context, key string) error

	// SignedURL returns a time-bounded URL the holder can use to fetch
	// the object directly. ttl=0 falls back to a reasonable default
	// (15 minutes). Backends that have no signing concept (in-memory)
	// return a stable in-process URI plus ErrSigningUnsupported.
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}

var (
	// ErrNotFound is returned by Get when the key has no object.
	// Backends MUST wrap this — callers use errors.Is.
	ErrNotFound = errors.New("blob: not found")

	// ErrSigningUnsupported is returned by backends that can't issue
	// time-bounded URLs (InMem). Handlers should fall back to a
	// proxy-fetch in that case.
	ErrSigningUnsupported = errors.New("blob: signed URLs not supported by this backend")
)

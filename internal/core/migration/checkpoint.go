package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Satyaamm/plowered/internal/core/blob"
)

// Checkpoint is the durable cursor for incremental runs. Persisted to
// object storage so re-runs (after process restart, after a failure)
// resume from the last successfully-flushed batch instead of replaying
// the whole table.
//
// We store the cursor as a string regardless of source type — every
// SQL warehouse can parse "2024-01-15T10:30:00Z" back into its native
// timestamp, "12345" back into an int, etc. Cleaner than carrying a
// type discriminator across the boundary.
type Checkpoint struct {
	LastCursorValue string `json:"last_cursor_value"`
	RowsProcessed   int64  `json:"rows_processed"`
	UpdatedAt       string `json:"updated_at"`
}

// CheckpointStore is the small interface the runner needs from blob
// storage. Backed by blob.ObjectStore in production, by an in-memory
// stub in tests.
type CheckpointStore interface {
	Load(ctx context.Context, planID string) (*Checkpoint, error)
	Save(ctx context.Context, planID string, c *Checkpoint) error
}

// BlobCheckpointStore implements CheckpointStore on top of a generic
// blob.ObjectStore. Key shape:
//
//	migrations/<plan_id>/checkpoint.json
//
// Tenant scoping happens upstream — the plan_id is already
// tenant-scoped because plans table is.
type BlobCheckpointStore struct {
	Bucket blob.ObjectStore
}

// NewBlobCheckpointStore wraps an ObjectStore. Returns nil when the
// store is nil so call-sites can branch on "no checkpointing
// configured" cleanly.
func NewBlobCheckpointStore(store blob.ObjectStore) *BlobCheckpointStore {
	if store == nil {
		return nil
	}
	return &BlobCheckpointStore{Bucket: store}
}

func (s *BlobCheckpointStore) Load(ctx context.Context, planID string) (*Checkpoint, error) {
	if s == nil || s.Bucket == nil {
		return nil, nil // no checkpoint == start from the beginning
	}
	rc, err := s.Bucket.Get(ctx, checkpointKey(planID))
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("checkpoint: get: %w", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: read: %w", err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(body, &cp); err != nil {
		return nil, fmt.Errorf("checkpoint: decode: %w", err)
	}
	return &cp, nil
}

func (s *BlobCheckpointStore) Save(ctx context.Context, planID string, c *Checkpoint) error {
	if s == nil || s.Bucket == nil {
		// Silent no-op: snapshot-only deployments don't need
		// checkpointing. Incremental runs without object storage
		// configured fail loudly at the Service layer, not here.
		return nil
	}
	if c == nil {
		return errors.New("checkpoint: nil")
	}
	body, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("checkpoint: encode: %w", err)
	}
	if _, err := s.Bucket.Put(ctx, checkpointKey(planID), bytes.NewReader(body)); err != nil {
		return fmt.Errorf("checkpoint: put: %w", err)
	}
	return nil
}

func checkpointKey(planID string) string {
	// Defence against caller passing a path with traversal characters.
	// Plan IDs are UUIDs in production but the interface is `string`.
	if strings.ContainsAny(planID, "/\\.") {
		planID = "invalid"
	}
	return "migrations/" + planID + "/checkpoint.json"
}

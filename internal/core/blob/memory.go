package blob

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// InMem is a thread-safe in-process ObjectStore. Used by:
//
//   - Unit tests — no network, no creds, deterministic.
//   - Local dev when the operator hasn't configured S3.
//   - Embedded mode where a single process holds the whole platform.
//
// Data is lost on process restart. The constructor is intentionally
// trivial so call-sites can be terse: blob.NewInMem().
type InMem struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewInMem() *InMem {
	return &InMem{data: map[string][]byte{}}
}

func (m *InMem) Put(ctx context.Context, key string, body io.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	buf, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("inmem: read body: %w", err)
	}
	m.mu.Lock()
	m.data[key] = buf
	m.mu.Unlock()
	return "mem://" + key, nil
}

func (m *InMem) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	buf, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: key=%s", ErrNotFound, key)
	}
	// Copy so callers can't mutate the stored bytes.
	cp := make([]byte, len(buf))
	copy(cp, buf)
	return io.NopCloser(bytes.NewReader(cp)), nil
}

func (m *InMem) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.data, key)
	m.mu.Unlock()
	return nil
}

// SignedURL: in-memory has no real signing surface. We return the
// stable in-process URI alongside ErrSigningUnsupported so the caller
// can fall back to proxy-fetching the body through their own handler.
// This keeps the interface honest about a real backend limitation
// rather than pretending to issue tokens.
func (m *InMem) SignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "mem://" + key, ErrSigningUnsupported
}

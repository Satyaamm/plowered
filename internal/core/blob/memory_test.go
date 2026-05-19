package blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestInMemRoundtrip exercises the basic Put / Get / Delete contract
// and the ErrNotFound + ErrSigningUnsupported sentinels. Boring but
// load-bearing: every backend must pass an equivalent suite.
func TestInMemRoundtrip(t *testing.T) {
	ctx := context.Background()
	s := NewInMem()

	uri, err := s.Put(ctx, "tenant-a/profile/users.json", strings.NewReader(`{"ok":true}`))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if uri == "" {
		t.Errorf("expected non-empty URI, got empty")
	}

	rc, err := s.Get(ctx, "tenant-a/profile/users.json")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, []byte(`{"ok":true}`)) {
		t.Errorf("body mismatch: got %q", got)
	}

	// Get on missing key returns ErrNotFound (wrapped is fine).
	_, err = s.Get(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("missing-key Get: want ErrNotFound, got %v", err)
	}

	// Delete is idempotent — second call on missing key must not error.
	if err := s.Delete(ctx, "missing"); err != nil {
		t.Errorf("idempotent delete: %v", err)
	}

	// SignedURL returns the in-process URI and the explicit sentinel.
	url, err := s.SignedURL(ctx, "tenant-a/profile/users.json", 0)
	if !errors.Is(err, ErrSigningUnsupported) {
		t.Errorf("SignedURL: want ErrSigningUnsupported, got %v", err)
	}
	if url == "" {
		t.Errorf("SignedURL should still return the in-process URI")
	}
}

func TestInMemConcurrent(t *testing.T) {
	ctx := context.Background()
	s := NewInMem()
	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			_, _ = s.Put(ctx, "concurrent", strings.NewReader("x"))
			_, _ = s.Get(ctx, "concurrent")
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// Package secrets is the vault that connections, API keys, and (future)
// API-callable adapters use to keep credentials out of the application
// rows. Connection rows store an opaque `secret_urn`; the actual bytes
// live here, encrypted with a master key the application never logs.
//
// The Vault interface is the only abstraction handlers depend on; the
// AESVault implementation is the production path (AES-256-GCM with the
// master key from PLOWERED_SECRETS_MASTER_KEY). MemoryVault exists for
// tests.
//
// URN format: `secret://<tenant>/<resource_kind>/<resource_id>`. URNs
// are tenant-scoped — every Get is checked against the caller's tenant
// before returning bytes (defence-in-depth on top of the row's FK).
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrNotFound is returned by Get / Delete when the URN is unknown.
var ErrNotFound = errors.New("secrets: not found")

// Vault is the small surface every caller uses.
type Vault interface {
	Put(ctx context.Context, tenantID, urn string, plaintext []byte) error
	Get(ctx context.Context, tenantID, urn string) ([]byte, error)
	Delete(ctx context.Context, tenantID, urn string) error
}

// Sealed is the on-disk shape stored by an AESVault. Implementations
// (Postgres SecretsStore today) write nonce + ciphertext separately; the
// envelope keeps the API symmetric.
type Sealed struct {
	Nonce      []byte
	Ciphertext []byte
	UpdatedAt  time.Time
}

// Storage is what AESVault delegates to for persistence. Implementing it
// against Postgres + KMS later is a one-file change.
type Storage interface {
	PutSealed(ctx context.Context, tenantID, urn string, s Sealed) error
	GetSealed(ctx context.Context, tenantID, urn string) (Sealed, error)
	DeleteSealed(ctx context.Context, tenantID, urn string) error
}

// AESVault is a 256-bit AES-GCM vault. Construct once at startup; the
// AEAD is reused across calls. Thread-safe.
type AESVault struct {
	storage Storage
	aead    cipher.AEAD
}

// NewAESVault parses the base64 master key, prepares the AEAD, and binds
// it to a backing Storage. The master key MUST be exactly 32 bytes after
// decoding.
func NewAESVault(masterKeyB64 string, storage Storage) (*AESVault, error) {
	if storage == nil {
		return nil, errors.New("secrets: storage is required")
	}
	key, err := decodeMasterKey(masterKeyB64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: gcm: %w", err)
	}
	return &AESVault{storage: storage, aead: aead}, nil
}

func (v *AESVault) Put(ctx context.Context, tenantID, urn string, plaintext []byte) error {
	if urn == "" || tenantID == "" {
		return errors.New("secrets: tenant_id and urn required")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("secrets: nonce: %w", err)
	}
	ct := v.aead.Seal(nil, nonce, plaintext, []byte(urn))
	return v.storage.PutSealed(ctx, tenantID, urn, Sealed{
		Nonce: nonce, Ciphertext: ct, UpdatedAt: time.Now().UTC(),
	})
}

func (v *AESVault) Get(ctx context.Context, tenantID, urn string) ([]byte, error) {
	s, err := v.storage.GetSealed(ctx, tenantID, urn)
	if err != nil {
		return nil, err
	}
	pt, err := v.aead.Open(nil, s.Nonce, s.Ciphertext, []byte(urn))
	if err != nil {
		return nil, fmt.Errorf("secrets: open: %w", err)
	}
	return pt, nil
}

func (v *AESVault) Delete(ctx context.Context, tenantID, urn string) error {
	return v.storage.DeleteSealed(ctx, tenantID, urn)
}

// GenerateMasterKey returns a freshly-rolled 32-byte master key,
// base64-encoded for env-var consumption. Used only by the dev-mode
// fallback in main.go and by ops tooling.
func GenerateMasterKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b[:]), nil
}

func decodeMasterKey(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("secrets: PLOWERED_SECRETS_MASTER_KEY is required")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("secrets: master key must be base64: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("secrets: master key must be 32 bytes, got %d", len(b))
	}
	return b, nil
}

// MemoryStorage is an in-process Storage for tests. Do not use in prod —
// secrets vanish on restart.
type MemoryStorage struct {
	mu sync.RWMutex
	m  map[string]map[string]Sealed // tenant_id → urn → sealed
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{m: make(map[string]map[string]Sealed)}
}

func (s *MemoryStorage) PutSealed(_ context.Context, tenantID, urn string, sealed Sealed) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m[tenantID] == nil {
		s.m[tenantID] = map[string]Sealed{}
	}
	s.m[tenantID][urn] = sealed
	return nil
}

func (s *MemoryStorage) GetSealed(_ context.Context, tenantID, urn string) (Sealed, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.m[tenantID][urn]; ok {
		return v, nil
	}
	return Sealed{}, ErrNotFound
}

func (s *MemoryStorage) DeleteSealed(_ context.Context, tenantID, urn string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[tenantID][urn]; !ok {
		return ErrNotFound
	}
	delete(s.m[tenantID], urn)
	return nil
}

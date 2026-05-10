package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters tuned for "interactive login on a small Postgres
// box" — ~75 ms on a typical x86 server. The encoded format follows the
// PHC string convention used by the rest of the industry, so password
// hashes are portable between languages and across hashing libraries.
//
//   $argon2id$v=19$m=65536,t=2,p=1$<salt-b64>$<hash-b64>
//
// If we ever need to migrate parameters (e.g. faster CPUs ship), we can
// detect via the parsed params and rehash on next login.
const (
	argonTime    uint32 = 2
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 1
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// HashPassword returns a PHC-encoded argon2id hash of the password.
// Returns an error only if the system RNG fails (i.e. catastrophic).
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("identity: password cannot be empty")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identity: read salt: %w", err)
	}
	digest := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(digest),
	), nil
}

// VerifyPassword constant-time-compares the supplied password against the
// stored PHC hash. Returns nil on match, ErrInvalidPassword on mismatch,
// or a parse error if the stored hash is malformed (also treated as a
// mismatch by the handler so we don't leak internal state to clients).
func VerifyPassword(password, encoded string) error {
	if password == "" || encoded == "" {
		return ErrInvalidPassword
	}
	parts := strings.Split(encoded, "$")
	// Expected: ["", "argon2id", "v=19", "m=65536,t=2,p=1", "<salt>", "<hash>"]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return ErrInvalidPassword
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return ErrInvalidPassword
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return ErrInvalidPassword
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return ErrInvalidPassword
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return ErrInvalidPassword
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(want, got) == 1 {
		return nil
	}
	return ErrInvalidPassword
}

// NewToken returns a 32-byte random URL-safe token. Used for both
// email-verification and (future) password-reset links.
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

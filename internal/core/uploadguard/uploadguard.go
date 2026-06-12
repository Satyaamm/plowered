// Package uploadguard is the mandatory validation gate for user file
// uploads. The HTTP layer rejects every non-JSON body today
// (BodyGuardMW), so the API has NO binary-upload surface — but the
// moment one lands (avatars, CSV imports, attachments), it MUST run
// the bytes through Check before persisting or parsing them.
//
// What Check defends against:
//
//   - Type lies — the declared Content-Type and the filename extension
//     are advisories from the attacker. We sniff the leading bytes
//     (magic numbers) and trust only those.
//   - Disguised executables — PE ("MZ"), ELF, Mach-O and shell-script
//     shebangs are rejected outright no matter what the file claims
//     to be. This is the cheap, reliable slice of "virus checking":
//     a catalog platform has no reason to ever store a binary
//     executable, so the policy is refusal, not detection.
//   - Corrupted / truncated files — a file whose magic doesn't match
//     any allowed kind is refused, which also catches truncation at
//     byte 0 and content that is simply noise.
//   - Decompression bombs — size is capped BEFORE content inspection,
//     and zip-family kinds are off the default allowlist.
//
// What this is NOT: an antivirus engine. If a deployment must accept
// rich documents (PDF, Office), wire ClamAV or a scanning service
// behind the Scanner interface and chain it after Check; uploadguard
// keeps the structural checks in-process either way.
package uploadguard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// Kind is a content family we know how to verify by magic bytes.
type Kind string

const (
	KindPNG  Kind = "png"
	KindJPEG Kind = "jpeg"
	KindGIF  Kind = "gif"
	KindWebP Kind = "webp"
	KindPDF  Kind = "pdf"
	KindCSV  Kind = "csv"  // verified as printable UTF-8 text, no magic
	KindJSON Kind = "json" // verified by first significant byte + UTF-8
)

// Limits bounds one upload. Callers set MaxBytes per use case (an
// avatar is not a CSV import); AllowedKinds is the closed set the
// endpoint accepts.
type Limits struct {
	MaxBytes     int64
	AllowedKinds []Kind
}

// Scanner is the seam for an external AV engine (ClamAV, cloud
// scanning API). Chain after Check; nil means structural checks only.
type Scanner interface {
	Scan(ctx context.Context, r io.Reader) error
}

var (
	ErrTooLarge    = errors.New("uploadguard: file exceeds size limit")
	ErrExecutable  = errors.New("uploadguard: executable content is never accepted")
	ErrUnknownType = errors.New("uploadguard: content does not match any allowed type")
	ErrEmpty       = errors.New("uploadguard: empty file")
)

// executable magic prefixes — rejected unconditionally.
var executableMagic = [][]byte{
	{'M', 'Z'},                   // Windows PE
	{0x7F, 'E', 'L', 'F'},        // ELF
	{0xFE, 0xED, 0xFA, 0xCE},     // Mach-O 32
	{0xFE, 0xED, 0xFA, 0xCF},     // Mach-O 64
	{0xCF, 0xFA, 0xED, 0xFE},     // Mach-O 64 LE
	{0xCA, 0xFE, 0xBA, 0xBE},     // Mach-O fat / Java class
	{'#', '!'},                   // script shebang
	{'P', 'K', 0x03, 0x04},       // zip family (jar/docx/apk) — off-list by default
}

// Check validates content against limits and returns the detected
// Kind. It reads at most limits.MaxBytes+1 bytes from r; the caller
// re-reads the file for persistence (or uses a TeeReader).
func Check(r io.Reader, limits Limits) (Kind, error) {
	if limits.MaxBytes <= 0 {
		return "", errors.New("uploadguard: MaxBytes must be positive")
	}
	if len(limits.AllowedKinds) == 0 {
		return "", errors.New("uploadguard: AllowedKinds must not be empty")
	}

	// Size gate first — never inspect unbounded input.
	data, err := io.ReadAll(io.LimitReader(r, limits.MaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("uploadguard: read: %w", err)
	}
	if int64(len(data)) > limits.MaxBytes {
		return "", ErrTooLarge
	}
	if len(data) == 0 {
		return "", ErrEmpty
	}

	// Executables: refused before any allowlist logic.
	for _, magic := range executableMagic {
		if bytes.HasPrefix(data, magic) {
			return "", ErrExecutable
		}
	}

	for _, kind := range limits.AllowedKinds {
		if matches(kind, data) {
			return kind, nil
		}
	}
	return "", ErrUnknownType
}

func matches(kind Kind, data []byte) bool {
	switch kind {
	case KindPNG:
		return bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	case KindJPEG:
		return bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF})
	case KindGIF:
		return bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a"))
	case KindWebP:
		return len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
	case KindPDF:
		return bytes.HasPrefix(data, []byte("%PDF-"))
	case KindCSV:
		return isPrintableText(data)
	case KindJSON:
		trimmed := bytes.TrimLeft(data, " \t\r\n")
		if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
			return false
		}
		return utf8.Valid(data)
	default:
		return false
	}
}

// isPrintableText accepts UTF-8 with no NUL or other C0 control bytes
// besides tab/newline/CR — a cheap "this is actually text" check that
// rejects binary blobs renamed to .csv.
func isPrintableText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			return false
		}
	}
	return true
}

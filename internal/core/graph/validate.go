package graph

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxNameLen          = 256
	maxQualifiedNameLen = 1024
	maxDescriptionLen   = 16 * 1024
	maxPropertiesBytes  = 1 << 20 // 1 MiB
	maxTags             = 64
	maxOwners           = 64
)

// ValidateAsset enforces the input rules documented in SECURITY.md §6.
// Returns nil on success, or an error wrapping ErrInvalidArgument.
func ValidateAsset(a *Asset) error {
	if a == nil {
		return fmt.Errorf("asset is nil: %w", ErrInvalidArgument)
	}
	if !utf8.ValidString(a.QualifiedName) {
		return fmt.Errorf("qualified_name not utf-8: %w", ErrInvalidArgument)
	}
	if a.QualifiedName == "" || len(a.QualifiedName) > maxQualifiedNameLen {
		return fmt.Errorf("qualified_name length %d outside 1..%d: %w",
			len(a.QualifiedName), maxQualifiedNameLen, ErrInvalidArgument)
	}
	if strings.ContainsAny(a.QualifiedName, "\x00\n\r") {
		return fmt.Errorf("qualified_name contains forbidden control chars: %w", ErrInvalidArgument)
	}
	if a.Name == "" || len(a.Name) > maxNameLen {
		return fmt.Errorf("name length %d outside 1..%d: %w",
			len(a.Name), maxNameLen, ErrInvalidArgument)
	}
	if a.Type == AssetTypeUnspecified {
		return fmt.Errorf("type required: %w", ErrInvalidArgument)
	}
	if len(a.Description) > maxDescriptionLen {
		return fmt.Errorf("description too long: %w", ErrInvalidArgument)
	}
	if len(a.Tags) > maxTags {
		return fmt.Errorf("too many tags (max %d): %w", maxTags, ErrInvalidArgument)
	}
	if len(a.Owners) > maxOwners {
		return fmt.Errorf("too many owners (max %d): %w", maxOwners, ErrInvalidArgument)
	}
	if approxJSONSize(a.Properties) > maxPropertiesBytes {
		return fmt.Errorf("properties exceed %d bytes: %w", maxPropertiesBytes, ErrInvalidArgument)
	}
	return nil
}

// approxJSONSize returns a cheap upper bound on the JSON-encoded size of m.
// Avoids a full json.Marshal allocation for the common path.
func approxJSONSize(m map[string]any) int {
	if m == nil {
		return 0
	}
	const overhead = 8 // braces, quotes, separators per entry, very rough
	n := 2
	for k, v := range m {
		n += len(k) + overhead
		switch x := v.(type) {
		case string:
			n += len(x) + 2
		case map[string]any:
			n += approxJSONSize(x)
		case []any:
			for _, e := range x {
				if s, ok := e.(string); ok {
					n += len(s) + 2
				} else {
					n += 16
				}
			}
		default:
			n += 16
		}
	}
	return n
}

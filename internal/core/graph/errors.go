package graph

import "errors"

// Sentinel errors. Wrap with fmt.Errorf("...: %w", err) when adding context.
// Map to gRPC status codes only at the API layer.
var (
	ErrNotFound       = errors.New("asset not found")
	ErrConflict       = errors.New("asset already exists")
	ErrInvalidArgument = errors.New("invalid argument")
	ErrForbidden      = errors.New("forbidden")
	ErrTenantMissing  = errors.New("tenant_id missing from context")
)

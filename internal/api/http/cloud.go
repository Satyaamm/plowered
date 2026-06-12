package http

import (
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/policy"
)

// CloudStatus reports the platform's effective infrastructure bindings
// — which backend each pluggable seam resolved to at boot. Strictly
// non-secret: kinds, bucket/container names, regions. Never keys,
// connection strings, or URLs with credentials.
//
// The struct is computed once in cmd/plowered/main.go (where the env
// is read) and handed to the mux through Deps, so handlers never read
// os.Getenv at request time.
type CloudStatus struct {
	ObjectStore CloudBinding `json:"object_store"`
	Email       CloudBinding `json:"email"`
	Database    CloudBinding `json:"database"`
	Queue       CloudBinding `json:"queue"`
	Events      CloudBinding `json:"events"`
}

// CloudBinding is one resolved seam: the backend kind plus a
// non-secret identifying detail (bucket name, region, host).
type CloudBinding struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

// cloudHandlers wires GET /v1/cloud/status. Admin-gated: the response
// shape is non-secret but still describes the deployment's internals,
// which is operator information, not end-user information.
func cloudHandlers(mux *http.ServeMux, status *CloudStatus, authz policy.Authorizer) {
	mux.HandleFunc("GET /v1/cloud/status", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r, authz, policy.VerbAdmin, "cloud") {
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

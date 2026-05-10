package http

import (
	"net/http"

	mcphandlers "github.com/Satyaamm/plowered/internal/api/mcp"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/policy"
	pkgmcp "github.com/Satyaamm/plowered/pkg/mcp"
)

// mountMCP exposes the MCP JSON-RPC surface at POST /mcp on the same
// auth+tenant chain as the rest of the API. Any LLM agent that already
// holds a Plowered session cookie or bearer token gets a policy-filtered,
// audit-logged read interface to the catalog.
//
// We deliberately reuse the existing session middleware: the principal
// the audit + policy code reads is the same one the rest of the platform
// trusts. No bespoke API key bridge — fewer moving parts, fewer ways for
// the audit chain to be wrong.
func mountMCP(mux *http.ServeMux, d Deps) {
	server := pkgmcp.NewServer(pkgmcp.ServerInfo{
		Name:    "plowered-mcp",
		Version: "dev",
	})

	deps := mcphandlers.Deps{
		Store:    d.Catalog,
		Audit:    d.AuditWriter,
		ToolName: "plowered-mcp",
		Version:  "dev",
	}
	if d.Policies != nil {
		deps.Auth = policy.NewEngine(d.Policies)
	}
	if err := mcphandlers.RegisterWith(server.Tools, deps); err != nil {
		panic(err) // wiring error; happens on boot only
	}

	transport := pkgmcp.NewHTTPTransport(server)
	mux.Handle("POST /v1/mcp", attachPrincipal(transport))
}

// attachPrincipal copies the authenticated auth.Principal onto the MCP
// context key so handlers can read it via mcphandlers.Principal(ctx).
// The session middleware already populated auth.PrincipalFromContext;
// this is a tiny adapter so the MCP layer doesn't import middleware
// internals.
func attachPrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.PrincipalFromContext(r.Context())
		ctx := mcphandlers.WithPrincipal(r.Context(), p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

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
	// Use the same engine instance the tool handlers already consult so
	// the HTTP-level gate and the per-tool authz stay coherent. Anyone
	// the engine grants VerbRead on assets is allowed to open an MCP
	// session; individual tool calls still pass through deps.Auth.
	engine := deps.Auth
	if engine == nil {
		engine = policy.NewEngine(nil)
	}
	mux.Handle("POST /v1/mcp", attachPrincipal(gateMCP(engine, transport)))
}

// gateMCP is the HTTP-level RBAC checkpoint for the MCP transport.
// Split out from mountMCP so the gate is unit-testable.
func gateMCP(authz policy.Authorizer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r, authz, policy.VerbRead, "asset") {
			return
		}
		next.ServeHTTP(w, r)
	})
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

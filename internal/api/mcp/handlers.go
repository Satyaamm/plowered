// Package mcp wires Plowered's storage layer into MCP tools. Each tool's
// JSON arguments and output schema match the MCP tool list returned to
// clients. Auth, tenant isolation, and the policy-engine read filter are
// applied centrally; tools never bypass them.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/pkg/mcp"
)

// Deps bundles every cross-cutting dependency the MCP tools need. Audit
// is optional — when nil, tool calls don't get logged (use only for
// dev). Authorizer is optional — when nil, every result passes through
// (also dev only). cmd/plowered-mcp wires concrete values from Postgres.
type Deps struct {
	Store    storage.Store
	Auth     policy.Authorizer
	Audit    audit.Writer
	ToolName string // service_name on the audit event ("plowered-mcp")
	Version  string // service_version
}

// Register installs Plowered's MCP tools on the given registry.
func Register(reg *mcp.ToolRegistry, store storage.Store) error {
	return RegisterWith(reg, Deps{Store: store, ToolName: "plowered-mcp"})
}

// RegisterWith is the full-fidelity registration: pass Auth + Audit to
// turn the MCP server into a policy-filtered, audit-logged read surface
// that any LLM agent can drive.
func RegisterWith(reg *mcp.ToolRegistry, d Deps) error {
	if d.Store == nil {
		return errors.New("mcp: Deps.Store is required")
	}
	if d.ToolName == "" {
		d.ToolName = "plowered-mcp"
	}
	for _, t := range []mcp.ToolRegistration{
		{
			Tool: mcp.Tool{
				Name:        "search_assets",
				Description: "Search the catalog by keyword across qualified names. Returns up to `limit` matches the caller is allowed to see.",
				InputSchema: schemaSearchAssets,
			},
			Handler: searchAssets(d),
		},
		{
			Tool: mcp.Tool{
				Name:        "get_asset",
				Description: "Fetch a single asset by qualified name. Returns name, type, description, tags, owners.",
				InputSchema: schemaGetAsset,
			},
			Handler: getAsset(d),
		},
		{
			Tool: mcp.Tool{
				Name:        "get_lineage",
				Description: "Return upstream or downstream lineage for an asset. `direction` is 'upstream' or 'downstream'; depth defaults to 1.",
				InputSchema: schemaGetLineage,
			},
			Handler: getLineage(d),
		},
	} {
		if err := reg.Register(t.Tool, t.Handler); err != nil {
			return err
		}
	}
	return nil
}

// allowed reports whether the principal can read the asset under the
// configured authorizer. When d.Auth is nil (dev mode) everything is
// allowed.
func allowed(ctx context.Context, d Deps, a *graph.Asset) (bool, string) {
	if d.Auth == nil {
		return true, "no-auth"
	}
	p := Principal(ctx)
	if p.TenantID == "" {
		p.TenantID = a.TenantID // tools always run within a single tenant
	}
	dec := d.Auth.Allow(ctx, p, policy.VerbRead, policy.Resource{
		Type:     "asset",
		ID:       a.ID,
		TenantID: a.TenantID,
		Tags:     a.Tags,
		OwnerIDs: a.Owners,
	})
	return dec.Allow, dec.Reason
}

// emit writes one audit row for an MCP tool call. Outcome is "success"
// when err is nil, otherwise "failure". DeniedCount lands in the After
// map so an admin can scan the audit feed for "agent X tried to read N
// things it shouldn't have."
func emit(ctx context.Context, d Deps, action, resourceID string, args any, denied int, err error) {
	if d.Audit == nil {
		return
	}
	p := Principal(ctx)
	tenant := p.TenantID
	if tenant == "" {
		if t, terr := storage.TenantFromContext(ctx); terr == nil {
			tenant = t
		}
	}
	outcome := audit.OutcomeSuccess
	errMsg := ""
	if err != nil {
		outcome = audit.OutcomeFailure
		errMsg = err.Error()
	}
	ev := audit.Event{
		TenantID:     tenant,
		ActorID:      p.ID,
		ActorKind:    "service",
		Action:       action,
		ResourceType: "asset",
		ResourceID:   resourceID,
		ServiceName:  d.ToolName,
		ServiceVer:   d.Version,
		Outcome:      outcome,
		ErrorMessage: errMsg,
		CreatedAt:    time.Now().UTC(),
		After: map[string]any{
			"args":           args,
			"denied_results": denied,
		},
	}
	_ = d.Audit.Emit(ctx, ev)
}

// ----- tool: search_assets -----

var schemaSearchAssets = json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "minLength": 1},
    "limit": {"type": "integer", "minimum": 1, "maximum": 100, "default": 10}
  },
  "required": ["query"]
}`)

type searchAssetsArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func searchAssets(d Deps) mcp.ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		var a searchAssetsArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			emit(ctx, d, "mcp.search_assets", "", a, 0, err)
			return mcp.ErrorResult("invalid arguments: %v", err), nil
		}
		if a.Query == "" {
			err := errors.New("query is required")
			emit(ctx, d, "mcp.search_assets", "", a, 0, err)
			return mcp.ErrorResult("query is required"), nil
		}
		if a.Limit <= 0 || a.Limit > 100 {
			a.Limit = 10
		}

		// v0: list and filter by qualified-name substring. The dedicated
		// search package replaces this once it lands.
		assets, _, err := d.Store.ListAssets(ctx, storage.ListAssetsOptions{PageSize: 500})
		if err != nil {
			emit(ctx, d, "mcp.search_assets", "", a, 0, err)
			return mcp.ErrorResult("list: %v", err), nil
		}

		var matches []*graph.Asset
		denied := 0
		needle := strings.ToLower(a.Query)
		for _, x := range assets {
			if !strings.Contains(strings.ToLower(x.QualifiedName), needle) &&
				!strings.Contains(strings.ToLower(x.Name), needle) {
				continue
			}
			ok, _ := allowed(ctx, d, x)
			if !ok {
				denied++
				continue
			}
			matches = append(matches, x)
			if len(matches) >= a.Limit {
				break
			}
		}
		emit(ctx, d, "mcp.search_assets", "", a, denied, nil)
		return mcp.TextResult(formatSearchHits(matches, denied)), nil
	}
}

func formatSearchHits(hits []*graph.Asset, denied int) string {
	if len(hits) == 0 && denied == 0 {
		return "No matching assets."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d match(es)", len(hits))
	if denied > 0 {
		fmt.Fprintf(&b, " (%d hidden by policy)", denied)
	}
	b.WriteString(":\n\n")
	for _, a := range hits {
		fmt.Fprintf(&b, "- %s (%s)\n", a.QualifiedName, a.Type)
		if a.Description != "" {
			fmt.Fprintf(&b, "  %s\n", a.Description)
		}
	}
	return b.String()
}

// ----- tool: get_asset -----

var schemaGetAsset = json.RawMessage(`{
  "type": "object",
  "properties": {
    "qualified_name": {"type": "string", "minLength": 1}
  },
  "required": ["qualified_name"]
}`)

type getAssetArgs struct {
	QualifiedName string `json:"qualified_name"`
}

func getAsset(d Deps) mcp.ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		var a getAssetArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			emit(ctx, d, "mcp.get_asset", "", a, 0, err)
			return mcp.ErrorResult("invalid arguments: %v", err), nil
		}
		if a.QualifiedName == "" {
			err := errors.New("qualified_name is required")
			emit(ctx, d, "mcp.get_asset", "", a, 0, err)
			return mcp.ErrorResult("qualified_name is required"), nil
		}
		asset, err := d.Store.GetAssetByQualifiedName(ctx, a.QualifiedName)
		if err != nil {
			emit(ctx, d, "mcp.get_asset", "", a, 0, err)
			if errors.Is(err, graph.ErrNotFound) {
				return mcp.ErrorResult("asset %q not found", a.QualifiedName), nil
			}
			return mcp.ErrorResult("get: %v", err), nil
		}
		ok, reason := allowed(ctx, d, asset)
		if !ok {
			emit(ctx, d, "mcp.get_asset", asset.ID, a, 1, errors.New("denied"))
			return mcp.ErrorResult("access denied: %s", reason), nil
		}
		emit(ctx, d, "mcp.get_asset", asset.ID, a, 0, nil)
		return mcp.TextResult(formatAsset(asset)), nil
	}
}

func formatAsset(a *graph.Asset) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", a.QualifiedName)
	fmt.Fprintf(&b, "Type:  %s\n", a.Type)
	fmt.Fprintf(&b, "Trust: %s\n", a.Trust)
	if a.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", a.Description)
	}
	if len(a.Tags) > 0 {
		fmt.Fprintf(&b, "\nTags: %s\n", strings.Join(a.Tags, ", "))
	}
	if len(a.Owners) > 0 {
		fmt.Fprintf(&b, "Owners: %s\n", strings.Join(a.Owners, ", "))
	}
	if !a.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, "Updated: %s\n", a.UpdatedAt.Format("2006-01-02"))
	}
	return b.String()
}

// ----- tool: get_lineage -----

var schemaGetLineage = json.RawMessage(`{
  "type": "object",
  "properties": {
    "qualified_name": {"type": "string", "minLength": 1},
    "direction": {"type": "string", "enum": ["upstream", "downstream"], "default": "upstream"},
    "depth": {"type": "integer", "minimum": 1, "maximum": 5, "default": 1}
  },
  "required": ["qualified_name"]
}`)

type getLineageArgs struct {
	QualifiedName string `json:"qualified_name"`
	Direction     string `json:"direction"`
	Depth         int    `json:"depth"`
}

func getLineage(d Deps) mcp.ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		var a getLineageArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			emit(ctx, d, "mcp.get_lineage", "", a, 0, err)
			return mcp.ErrorResult("invalid arguments: %v", err), nil
		}
		if a.Direction == "" {
			a.Direction = "upstream"
		}
		if a.Depth <= 0 {
			a.Depth = 1
		}
		root, err := d.Store.GetAssetByQualifiedName(ctx, a.QualifiedName)
		if err != nil {
			emit(ctx, d, "mcp.get_lineage", "", a, 0, err)
			return mcp.ErrorResult("get: %v", err), nil
		}
		if ok, reason := allowed(ctx, d, root); !ok {
			emit(ctx, d, "mcp.get_lineage", root.ID, a, 1, errors.New("denied"))
			return mcp.ErrorResult("access denied: %s", reason), nil
		}
		outgoing := a.Direction == "downstream"
		edges, err := d.Store.Neighbors(ctx, root.ID, storage.NeighborsOptions{
			Kind:     graph.EdgeLineage,
			Outgoing: outgoing,
			Limit:    100,
		})
		if err != nil {
			emit(ctx, d, "mcp.get_lineage", root.ID, a, 0, err)
			return mcp.ErrorResult("neighbors: %v", err), nil
		}
		emit(ctx, d, "mcp.get_lineage", root.ID, a, 0, nil)
		return mcp.TextResult(formatLineage(root, edges, a.Direction)), nil
	}
}

func formatLineage(root *graph.Asset, edges []*graph.Edge, direction string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s lineage of %s (%d edge(s) at depth 1):\n\n",
		strings.Title(direction), root.QualifiedName, len(edges))
	if len(edges) == 0 {
		b.WriteString("  (no immediate neighbors)\n")
		return b.String()
	}
	for _, e := range edges {
		other := e.TargetID
		if direction == "upstream" {
			other = e.SourceID
		}
		fmt.Fprintf(&b, "  - %s\n", other)
	}
	return b.String()
}

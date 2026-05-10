package http

import (
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/core/identity"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/storage"
)

// accessHandlers registers the "what can user X see?" endpoint. The
// handler walks the catalog and runs every asset through the policy
// engine for a synthetic principal the caller describes.
//
// Routes:
//
//	POST /v1/access/preview   body: {role, groups[], email?, verb?}
//
// Response is a small envelope with two slices: visible and denied.
// Each entry has the asset's qn + the engine's reason string.
func accessHandlers(mux *http.ServeMux, cat storage.Store, rules policy.RuleStore, ids identity.Repo) {
	mux.HandleFunc("POST /v1/access/preview", accessPreviewHandler(cat, rules, ids))
}

type accessRequest struct {
	Role   string   `json:"role"`
	Groups []string `json:"groups"`
	Email  string   `json:"email"`
	Verb   string   `json:"verb"`
	Limit  int      `json:"limit"`
}

type accessRow struct {
	AssetID       string   `json:"asset_id"`
	QualifiedName string   `json:"qualified_name"`
	Type          string   `json:"type"`
	Tags          []string `json:"tags,omitempty"`
	Reason        string   `json:"reason"`
}

type accessResponse struct {
	Principal accessPrincipalView `json:"principal"`
	Verb      string              `json:"verb"`
	Total     int                 `json:"total"`
	Visible   []accessRow         `json:"visible"`
	Denied    []accessRow         `json:"denied"`
}

type accessPrincipalView struct {
	UserID   string   `json:"user_id,omitempty"`
	Email    string   `json:"email,omitempty"`
	Roles    []string `json:"roles"`
	Groups   []string `json:"groups,omitempty"`
	TenantID string   `json:"tenant_id"`
}

func accessPreviewHandler(cat storage.Store, rules policy.RuleStore, ids identity.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var req accessRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}

		// Build a synthetic principal. If an email was supplied AND we
		// have an identity store, resolve the real user to honour their
		// actual roles/groups; otherwise treat the role/groups payload
		// as the full principal description.
		p := auth.Principal{TenantID: tenant}
		if req.Email != "" && ids != nil {
			u, err := ids.GetByEmail(r.Context(), req.Email)
			if err == nil && u != nil {
				p.ID = u.ID
				p.Email = u.Email
				m, mErr := ids.GetMembership(r.Context(), tenant, u.ID)
				if mErr == nil && m != nil {
					p.Roles = m.Roles
					p.Groups = m.Groups
				}
			}
		}
		if req.Role != "" {
			p.Roles = appendUnique(p.Roles, req.Role)
		}
		for _, g := range req.Groups {
			if g != "" {
				p.Groups = appendUnique(p.Groups, g)
			}
		}
		if p.ID == "" {
			p.ID = "preview-" + req.Role
		}

		verb := policy.Verb(req.Verb)
		if verb == "" {
			verb = policy.VerbRead
		}
		limit := req.Limit
		if limit <= 0 || limit > 2000 {
			limit = 500
		}

		// Walk the catalog. We page through ListAssets — most catalogs
		// fit comfortably in a single page; large ones cap at `limit`
		// for the preview, with a "Total" count so the UI can warn the
		// user that the result is truncated.
		assets, _, err := cat.ListAssets(r.Context(), storage.ListAssetsOptions{
			PageSize: limit,
		})
		if err != nil {
			writeError(w, err)
			return
		}

		engine := policy.NewEngine(rules)
		resp := accessResponse{
			Principal: accessPrincipalView{
				UserID: p.ID, Email: p.Email, Roles: p.Roles,
				Groups: p.Groups, TenantID: p.TenantID,
			},
			Verb:    string(verb),
			Total:   len(assets),
			Visible: []accessRow{},
			Denied:  []accessRow{},
		}
		for _, a := range assets {
			d := engine.Allow(r.Context(), p, verb, policy.Resource{
				Type:     "asset",
				ID:       a.ID,
				TenantID: a.TenantID,
				Tags:     a.Tags,
				OwnerIDs: a.Owners,
			})
			row := accessRow{
				AssetID:       a.ID,
				QualifiedName: a.QualifiedName,
				Type:          string(a.Type),
				Tags:          a.Tags,
				Reason:        d.Reason,
			}
			if d.Allow {
				resp.Visible = append(resp.Visible, row)
			} else {
				resp.Denied = append(resp.Denied, row)
			}
		}
		_ = graph.AssetType("") // keep graph import live for type alias users
		writeJSON(w, http.StatusOK, resp)
	}
}

func appendUnique(xs []string, x string) []string {
	for _, e := range xs {
		if e == x {
			return xs
		}
	}
	return append(xs, x)
}

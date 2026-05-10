package http

import (
	"errors"
	"net/http"
	"time"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/secrets"
	"github.com/Satyaamm/plowered/internal/worker"
)

// ConnectionDeps groups what the connection routes need beyond the
// connection.Repo: the secrets vault and the per-Type tester registry.
type ConnectionDeps struct {
	Connections connection.Repo
	Vault       secrets.Vault
	Registry    *connection.Registry
	Enqueuer    worker.Enqueuer
}

// connectionHandlers registers /v1/connections endpoints. CRUD is
// straightforward; /test opens an actual driver connection (with a 5s
// cap) and persists the resulting Health onto the row.
func connectionHandlers(mux *http.ServeMux, d ConnectionDeps) {
	mux.HandleFunc("GET /v1/connections",                     listConnectionsHandler(d))
	mux.HandleFunc("POST /v1/connections",                    createConnectionHandler(d))
	mux.HandleFunc("GET /v1/connections/{id}",                getConnectionHandler(d))
	mux.HandleFunc("PATCH /v1/connections/{id}",              updateConnectionHandler(d))
	mux.HandleFunc("DELETE /v1/connections/{id}",             deleteConnectionHandler(d))
	mux.HandleFunc("POST /v1/connections/{id}/test",          testConnectionHandler(d))
	mux.HandleFunc("POST /v1/connections/{id}/crawl",         crawlConnectionHandler(d))
}

// crawlConnectionHandler enqueues an async TaskCrawlConnection job and
// returns 202. The actual schema walk happens in the worker — the user
// polls /v1/connections to see the catalog grow.
func crawlConnectionHandler(d ConnectionDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		c, err := d.Connections.Get(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		if d.Enqueuer == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorBody{"unavailable",
				"no async enqueuer configured"})
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		if err := d.Enqueuer.EnqueueCrawlConnection(r.Context(), worker.CrawlConnectionPayload{
			TenantID:     tenant,
			ConnectionID: c.ID,
			Actor:        p.ID,
		}); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":        "queued",
			"connection_id": c.ID,
			"queued_at":     time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
}

// connectionView is the JSON shape we expose. We never round-trip the
// secret material — the wire format only exposes the URN and the health.
type connectionView struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Config      map[string]any `json:"config"`
	Health      string         `json:"health"`
	LastCheckAt string         `json:"last_check_at,omitempty"`
	CreatedBy   string         `json:"created_by"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func toView(c *connection.Connection) connectionView {
	v := connectionView{
		ID: c.ID, Name: c.Name, Type: string(c.Type),
		Config: c.Config, Health: string(c.Health),
		CreatedBy: c.CreatedBy,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if !c.LastCheckAt.IsZero() {
		v.LastCheckAt = c.LastCheckAt.UTC().Format(time.RFC3339Nano)
	}
	return v
}

// createConnectionRequest is the wire body for POST. `password` is the
// only secret today — we accept it inline and stash it via the vault on
// the way to Postgres. Tomorrow we extend with token / private-key / IAM
// auth modes by adding optional fields here.
type createConnectionRequest struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Config   map[string]any `json:"config"`
	Password string         `json:"password,omitempty"`
}

func listConnectionsHandler(d ConnectionDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		out, err := d.Connections.List(r.Context(), tenant)
		if err != nil {
			writeError(w, err)
			return
		}
		views := make([]connectionView, 0, len(out))
		for _, c := range out {
			views = append(views, toView(c))
		}
		writeJSON(w, http.StatusOK, map[string]any{"connections": views})
	}
}

func createConnectionHandler(d ConnectionDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		var body createConnectionRequest
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if body.Name == "" || body.Type == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "name and type are required"})
			return
		}
		// Insert with empty SecretURN; we know the row's id only after
		// the INSERT, so the URN is patched in a follow-up UPDATE within
		// the same handler. The vault Put is best-effort if the password
		// is missing (some sources auth by IAM / instance identity).
		created, err := d.Connections.Create(r.Context(), &connection.Connection{
			TenantID:  tenant,
			Name:      body.Name,
			Type:      connection.Type(body.Type),
			Config:    body.Config,
			CreatedBy: p.ID,
		})
		if err != nil {
			if errors.Is(err, connection.ErrNameTaken) {
				writeJSON(w, http.StatusConflict, errorBody{"name_taken", err.Error()})
				return
			}
			writeError(w, err)
			return
		}

		urn := connection.SecretURNFor(tenant, created.ID)
		if body.Password != "" && d.Vault != nil {
			if err := d.Vault.Put(r.Context(), tenant, urn, []byte(body.Password)); err != nil {
				// Roll back the row — leaving a connection with a known
				// URN but no actual secret is worse than failing.
				_ = d.Connections.Delete(r.Context(), tenant, created.ID)
				writeJSON(w, http.StatusInternalServerError, errorBody{"vault_error", err.Error()})
				return
			}
			created.SecretURN = urn
			if _, err := d.Connections.Update(r.Context(), created); err != nil {
				writeError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusCreated, toView(created))
	}
}

func getConnectionHandler(d ConnectionDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		c, err := d.Connections.Get(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, toView(c))
	}
}

// updateConnectionRequest mirrors create — config + optional new password.
type updateConnectionRequest struct {
	Name     string         `json:"name"`
	Config   map[string]any `json:"config"`
	Password string         `json:"password,omitempty"`
}

func updateConnectionHandler(d ConnectionDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var body updateConnectionRequest
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		existing, err := d.Connections.Get(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		if body.Name != "" {
			existing.Name = body.Name
		}
		if body.Config != nil {
			existing.Config = body.Config
		}
		updated, err := d.Connections.Update(r.Context(), existing)
		if err != nil {
			writeError(w, err)
			return
		}
		if body.Password != "" && d.Vault != nil {
			urn := connection.SecretURNFor(tenant, updated.ID)
			if err := d.Vault.Put(r.Context(), tenant, urn, []byte(body.Password)); err != nil {
				writeJSON(w, http.StatusInternalServerError, errorBody{"vault_error", err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, toView(updated))
	}
}

func deleteConnectionHandler(d ConnectionDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		c, _ := d.Connections.Get(r.Context(), tenant, id)
		if err := d.Connections.Delete(r.Context(), tenant, id); err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		// Best-effort: scrub the secret too. A leftover secret is benign
		// (no row references it) but rotating master keys later is
		// cleaner without orphans.
		if c != nil && c.SecretURN != "" && d.Vault != nil {
			_ = d.Vault.Delete(r.Context(), tenant, c.SecretURN)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func testConnectionHandler(d ConnectionDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		c, err := d.Connections.Get(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		tester, ok := d.Registry.Tester(c.Type)
		if !ok {
			writeJSON(w, http.StatusBadRequest, errorBody{"unsupported_type",
				"no tester registered for type " + string(c.Type)})
			return
		}
		var secret []byte
		if c.SecretURN != "" && d.Vault != nil {
			b, err := d.Vault.Get(r.Context(), tenant, c.SecretURN)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, errorBody{"vault_error", err.Error()})
				return
			}
			secret = b
		}
		now := time.Now().UTC()
		if err := tester.Test(r.Context(), c.Config, secret); err != nil {
			_ = d.Connections.UpdateHealth(r.Context(), tenant, c.ID, connection.HealthUnreachable, now)
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":         false,
				"health":     connection.HealthUnreachable,
				"checked_at": now.Format(time.RFC3339Nano),
				"error":      err.Error(),
			})
			return
		}
		_ = d.Connections.UpdateHealth(r.Context(), tenant, c.ID, connection.HealthHealthy, now)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"health":     connection.HealthHealthy,
			"checked_at": now.Format(time.RFC3339Nano),
		})
	}
}

package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/policy"
)

// Restorer knows how to put a tombstoned row back into its source table.
// Each domain (asset/pipeline/check/...) provides one. The recycle-bin
// handler dispatches by ResourceType.
type Restorer func(ctx context.Context, r *deleted.Record) error

// deletedHandlers wires the recycle-bin endpoints onto mux. The
// restorers map is owned by the caller because each domain decides what
// "restore" means — usually re-INSERT the payload into the source table.
func deletedHandlers(mux *http.ServeMux, repo deleted.Repo, restorers map[string]Restorer) {
	mux.HandleFunc("GET /v1/deleted",                listDeletedHandler(repo))
	mux.HandleFunc("GET /v1/deleted/{id}",           getDeletedHandler(repo))
	mux.HandleFunc("POST /v1/deleted/{id}/restore",  restoreDeletedHandler(repo, restorers))
	mux.HandleFunc("DELETE /v1/deleted/{id}",        purgeDeletedHandler(repo))
}

func listDeletedHandler(repo deleted.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		out, err := repo.List(r.Context(), tenant, deleted.ListOptions{
			ResourceType:    q.Get("type"),
			Limit:           limit,
			IncludeRestored: q.Get("include_restored") == "true",
			IncludePurged:   q.Get("include_purged") == "true",
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"records": out})
	}
}

func getDeletedHandler(repo deleted.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		got, err := repo.Get(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

func restoreDeletedHandler(repo deleted.Repo, restorers map[string]Restorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		rec, err := repo.Get(r.Context(), tenant, id)
		if err != nil || !rec.IsActive() {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "tombstone not active"})
			return
		}
		fn, ok := restorers[rec.ResourceType]
		if !ok {
			writeJSON(w, http.StatusBadRequest, errorBody{"unsupported",
				"no restorer registered for " + rec.ResourceType})
			return
		}
		if err := fn(r.Context(), rec); err != nil {
			writeError(w, err)
			return
		}
		actor := ""
		if p, ok := principalFrom(r); ok {
			actor = p.ID
		}
		if err := repo.MarkRestored(r.Context(), tenant, id, actor, time.Now().UTC()); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "restored", "id": id})
	}
}

// purgeDeletedHandler permanently removes a tombstone. The handler ENFORCES
// the super_admin role inline — defense in depth on top of the policy
// engine, since this is a destructive, non-reversible action.
func purgeDeletedHandler(repo deleted.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		if !policy.HasRole(p, "super_admin") {
			writeJSON(w, http.StatusForbidden, errorBody{"forbidden",
				"only super_admin can permanently delete a tombstone"})
			return
		}
		id := r.PathValue("id")
		if err := repo.MarkPurged(r.Context(), tenant, id, p.ID, time.Now().UTC()); err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// jsonRoundTrip turns a typed value into a map[string]any payload via the
// JSON marshaller. We use this when capturing a tombstone so the schema
// stays decoupled from the Go struct.
func jsonRoundTrip(v any) (map[string]any, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// remapJSON reverses jsonRoundTrip — turn the Payload back into a typed
// struct so a Restorer can pass it to the source repo's Create method.
func remapJSON(payload map[string]any, dst any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

// captureTombstone is the shared "delete-handler prelude": serialize the
// row, attach the principal + request id, and persist into deleted.Repo.
// Returns an error so the caller can surface it.
//
// When `holds` is non-nil we consult it BEFORE writing the tombstone so a
// resource caught by an active legal hold cannot be removed even
// transiently. The error is unwrapped to legalhold.ErrHeld upstream so
// the handler can return 409 with the hold id (see writeDeleteError).
func captureTombstone(r *http.Request, repo deleted.Repo, holds legalhold.Repo, resourceType, resourceID string, payload any) error {
	tenant, err := auth.PrincipalFromContext(r.Context())
	if err != nil {
		return err
	}
	if holds != nil {
		if h, herr := holds.Check(r.Context(), tenant.TenantID, resourceType, resourceID, nil); herr != nil {
			return &HeldError{Hold: h, Err: herr}
		}
	}
	body, err := jsonRoundTrip(payload)
	if err != nil {
		return err
	}
	requestID, _ := r.Context().Value(requestIDKey{}).(string)
	_, err = repo.Capture(r.Context(), &deleted.Record{
		TenantID:       tenant.TenantID,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Payload:        body,
		DeletedBy:      tenant.ID,
		DeletedKind:    "user",
		DeletionReason: deleted.ReasonUserAction,
		RequestID:      requestID,
		DeletedAt:      time.Now().UTC(),
	})
	return err
}

// HeldError wraps legalhold.ErrHeld so the caller can extract the matching
// hold for its 409 response without re-querying.
type HeldError struct {
	Hold *legalhold.Hold
	Err  error
}

func (e *HeldError) Error() string { return e.Err.Error() }
func (e *HeldError) Unwrap() error { return e.Err }

// writeDeleteError translates an error from captureTombstone (or a delete
// repo call) into a structured response. Returns true when the error was
// handled so the caller can stop processing.
func writeDeleteError(w http.ResponseWriter, err error) bool {
	var held *HeldError
	if errors.As(err, &held) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":    "legal_hold",
			"message": "resource is under an active legal hold and cannot be deleted",
			"hold_id": held.Hold.ID,
			"matter":  held.Hold.Matter,
		})
		return true
	}
	if errors.Is(err, legalhold.ErrHeld) {
		writeJSON(w, http.StatusConflict, errorBody{"legal_hold", err.Error()})
		return true
	}
	writeError(w, err)
	return true
}

package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/feedback"
	"github.com/Satyaamm/plowered/internal/core/policy"
)

// feedbackHandlers wires the user-submitted feedback surface.
//
//	POST   /v1/feedback                   submit (any authenticated user)
//	GET    /v1/feedback                   list (tenant-scoped; platform_admin
//	                                       may pass ?cross_tenant=1)
//	GET    /v1/feedback/{id}              read
//	PATCH  /v1/feedback/{id}              triage (platform_admin only)
//	DELETE /v1/feedback/{id}              delete (platform_admin or submitter)
//	POST   /v1/feedback/{id}/vote         add a vote (any auth)
//	DELETE /v1/feedback/{id}/vote         remove a vote (any auth)
//	GET    /v1/feedback/{id}/comments     list comments
//	POST   /v1/feedback/{id}/comments     append a comment (any auth)
func feedbackHandlers(mux *http.ServeMux, repo feedback.Repo, authz policy.Authorizer) {
	if repo == nil {
		return
	}
	mux.HandleFunc("POST   /v1/feedback",                    submitFeedbackHandler(repo, authz))
	mux.HandleFunc("GET    /v1/feedback",                    listFeedbackHandler(repo, authz))
	mux.HandleFunc("GET    /v1/feedback/{id}",               getFeedbackHandler(repo, authz))
	mux.HandleFunc("PATCH  /v1/feedback/{id}",               triageFeedbackHandler(repo, authz))
	mux.HandleFunc("DELETE /v1/feedback/{id}",               deleteFeedbackHandler(repo, authz))
	mux.HandleFunc("POST   /v1/feedback/{id}/vote",          voteFeedbackHandler(repo, authz))
	mux.HandleFunc("DELETE /v1/feedback/{id}/vote",          unvoteFeedbackHandler(repo, authz))
	mux.HandleFunc("GET    /v1/feedback/{id}/comments",      listFeedbackCommentsHandler(repo, authz))
	mux.HandleFunc("POST   /v1/feedback/{id}/comments",      commentFeedbackHandler(repo, authz))
}

type submitFeedbackReq struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	PageURL   string `json:"page_url"`
	UserAgent string `json:"user_agent"`
	Priority  string `json:"priority"`
}

type feedbackView struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenant_id"`
	SubmitterID    string `json:"submitter_id"`
	SubmitterEmail string `json:"submitter_email,omitempty"`
	Type           string `json:"type"`
	Title          string `json:"title"`
	Body           string `json:"body,omitempty"`
	PageURL        string `json:"page_url,omitempty"`
	UserAgent      string `json:"user_agent,omitempty"`
	Priority       string `json:"priority"`
	Status         string `json:"status"`
	AssigneeID     string `json:"assignee_id,omitempty"`
	VoteCount      int    `json:"vote_count"`
	VotedByMe      bool   `json:"voted_by_me,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func toFeedbackView(it *feedback.Item, votedByMe bool) feedbackView {
	return feedbackView{
		ID: it.ID, TenantID: it.TenantID,
		SubmitterID: it.SubmitterID, SubmitterEmail: it.SubmitterEmail,
		Type: string(it.Type), Title: it.Title, Body: it.Body,
		PageURL: it.PageURL, UserAgent: it.UserAgent,
		Priority: string(it.Priority), Status: string(it.Status),
		AssigneeID: it.AssigneeID, VoteCount: it.VoteCount,
		VotedByMe: votedByMe,
		CreatedAt:  it.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:  it.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func submitFeedbackHandler(repo feedback.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Any authenticated tenant member may submit. The gate
		// resolves to "the principal can read in some tenant" which
		// every role grants, so the lightest verb (VerbRead) is the
		// right choice — there's no point requiring edit for a feedback
		// post when the goal is to maximise the channel's openness.
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRead, "feedback")
		if tenant == "" {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		var body submitFeedbackReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if body.Title == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "title required"})
			return
		}
		typ := feedback.Type(body.Type)
		if !validFeedbackType(typ) {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request",
				"type must be one of bug | enhancement | question | praise"})
			return
		}
		// Cap the long-form fields. The schema is TEXT so the DB
		// would accept anything; cap here so a runaway paste doesn't
		// fill a tenant's quota or break the queue UI.
		if len(body.Title) > 200 {
			body.Title = body.Title[:200]
		}
		if len(body.Body) > 5000 {
			body.Body = body.Body[:5000]
		}
		priority := feedback.Priority(body.Priority)
		if !validFeedbackPriority(priority) {
			priority = feedback.PriorityNormal
		}
		it, err := repo.Create(r.Context(), &feedback.Item{
			TenantID:       tenant,
			SubmitterID:    p.ID,
			SubmitterEmail: p.Email,
			Type:           typ,
			Title:          body.Title,
			Body:           body.Body,
			PageURL:        body.PageURL,
			UserAgent:      body.UserAgent,
			Priority:       priority,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toFeedbackView(it, false))
	}
}

func listFeedbackHandler(repo feedback.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		// Cross-tenant view requires VerbPlatform (platform_admin only).
		// Anyone else gets their own tenant's items even if they pass
		// ?cross_tenant=1.
		opts := feedback.ListOptions{
			TenantID: tenant,
			Status:   feedback.Status(r.URL.Query().Get("status")),
			Type:     feedback.Type(r.URL.Query().Get("type")),
			Priority: feedback.Priority(r.URL.Query().Get("priority")),
			Assignee: r.URL.Query().Get("assignee"),
		}
		if limit, _ := strconv.Atoi(r.URL.Query().Get("limit")); limit > 0 {
			opts.Limit = limit
		}
		if r.URL.Query().Get("cross_tenant") == "1" {
			if !gate(w, r, authz, policy.VerbPlatform, "feedback") {
				return
			}
			opts.TenantID = "" // drop the filter
		} else {
			// Even tenant-scoped reads need the basic read verb so a
			// principal with zero grants (no role at all) is rejected.
			if !gate(w, r, authz, policy.VerbRead, "feedback") {
				return
			}
		}
		items, err := repo.List(r.Context(), opts)
		if err != nil {
			writeError(w, err)
			return
		}
		// Decorate with voted_by_me for the calling user.
		p, _ := auth.PrincipalFromContext(r.Context())
		ids := make([]string, 0, len(items))
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		voted, _ := repo.VotedBy(r.Context(), p.ID, ids)
		out := make([]feedbackView, 0, len(items))
		for _, it := range items {
			out = append(out, toFeedbackView(it, voted[it.ID]))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": out})
	}
}

func getFeedbackHandler(repo feedback.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		it, err := repo.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		// Tenant-scoped read for non-platform principals.
		p, _ := auth.PrincipalFromContext(r.Context())
		if it.TenantID != tenant {
			if !gate(w, r, authz, policy.VerbPlatform, "feedback") {
				return
			}
		} else if !gate(w, r, authz, policy.VerbRead, "feedback") {
			return
		}
		voted, _ := repo.VotedBy(r.Context(), p.ID, []string{it.ID})
		writeJSON(w, http.StatusOK, toFeedbackView(it, voted[it.ID]))
	}
}

type triageFeedbackReq struct {
	Type       *string `json:"type,omitempty"`
	Title      *string `json:"title,omitempty"`
	Body       *string `json:"body,omitempty"`
	Priority   *string `json:"priority,omitempty"`
	Status     *string `json:"status,omitempty"`
	AssigneeID *string `json:"assignee_id,omitempty"`
}

func triageFeedbackHandler(repo feedback.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Triage is platform_admin only — changing status / priority /
		// assignee across the queue is the operator's job, not the
		// submitter's.
		if !gate(w, r, authz, policy.VerbPlatform, "feedback") {
			return
		}
		it, err := repo.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		var body triageFeedbackReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if body.Type != nil {
			if !validFeedbackType(feedback.Type(*body.Type)) {
				writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "invalid type"})
				return
			}
			it.Type = feedback.Type(*body.Type)
		}
		if body.Title != nil {
			it.Title = *body.Title
		}
		if body.Body != nil {
			it.Body = *body.Body
		}
		if body.Priority != nil {
			if !validFeedbackPriority(feedback.Priority(*body.Priority)) {
				writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "invalid priority"})
				return
			}
			it.Priority = feedback.Priority(*body.Priority)
		}
		if body.Status != nil {
			if !validFeedbackStatus(feedback.Status(*body.Status)) {
				writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "invalid status"})
				return
			}
			it.Status = feedback.Status(*body.Status)
		}
		if body.AssigneeID != nil {
			it.AssigneeID = *body.AssigneeID
		}
		updated, err := repo.Update(r.Context(), it)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toFeedbackView(updated, false))
	}
}

func deleteFeedbackHandler(repo feedback.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		it, err := repo.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		// Two ways to delete: you submitted it, OR you're a platform
		// admin. Anyone else gets 403.
		if it.SubmitterID != p.ID {
			if !gate(w, r, authz, policy.VerbPlatform, "feedback") {
				return
			}
		}
		if err := repo.Delete(r.Context(), it.ID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func voteFeedbackHandler(repo feedback.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r, authz, policy.VerbRead, "feedback") {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		count, already, err := repo.Vote(r.Context(), r.PathValue("id"), p.ID)
		if err != nil {
			if errors.Is(err, feedback.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
				return
			}
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"vote_count":     count,
			"already_voted":  already,
		})
	}
}

func unvoteFeedbackHandler(repo feedback.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r, authz, policy.VerbRead, "feedback") {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		count, err := repo.Unvote(r.Context(), r.PathValue("id"), p.ID)
		if err != nil {
			if errors.Is(err, feedback.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
				return
			}
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"vote_count": count})
	}
}

type commentReq struct {
	Body string `json:"body"`
}

func commentFeedbackHandler(repo feedback.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r, authz, policy.VerbRead, "feedback") {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		var body commentReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if body.Body == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "body required"})
			return
		}
		if len(body.Body) > 5000 {
			body.Body = body.Body[:5000]
		}
		role := "submitter"
		if policy.HasRole(p, "platform_admin") {
			role = "platform_admin"
		}
		c, err := repo.AddComment(r.Context(), &feedback.Comment{
			ItemID:     r.PathValue("id"),
			AuthorID:   p.ID,
			AuthorRole: role,
			Body:       body.Body,
		})
		if err != nil {
			if errors.Is(err, feedback.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
				return
			}
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, c)
	}
}

func listFeedbackCommentsHandler(repo feedback.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r, authz, policy.VerbRead, "feedback") {
			return
		}
		cs, err := repo.ListComments(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"comments": cs})
	}
}

func validFeedbackType(t feedback.Type) bool {
	for _, x := range feedback.AllTypes {
		if x == t {
			return true
		}
	}
	return false
}

func validFeedbackPriority(p feedback.Priority) bool {
	for _, x := range feedback.AllPriorities {
		if x == p {
			return true
		}
	}
	return false
}

func validFeedbackStatus(s feedback.Status) bool {
	for _, x := range feedback.AllStatuses {
		if x == s {
			return true
		}
	}
	return false
}

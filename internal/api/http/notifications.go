package http

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/Satyaamm/plowered/internal/core/notify"
)

func newChannelID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "ch-fallback"
	}
	return hex.EncodeToString(b[:])
}

func notifyHandlers(mux *http.ServeMux, store notify.Repo) {
	mux.HandleFunc("GET /v1/notifications/channels",       listChannelsHandler(store))
	mux.HandleFunc("POST /v1/notifications/channels",      createChannelHandler(store))
	mux.HandleFunc("GET /v1/notifications/rules",          listNotifyRulesHandler(store))
	mux.HandleFunc("POST /v1/notifications/rules",         createNotifyRuleHandler(store))
	mux.HandleFunc("GET /v1/notifications/deliveries",     listDeliveriesHandler(store))
}

func listChannelsHandler(s notify.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"channels": s.ListChannelsForTenant(tenant)})
	}
}

func createChannelHandler(s notify.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var c notify.ChannelConfig
		if err := decodeJSON(r, &c); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if c.Kind == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "kind required"})
			return
		}
		c.TenantID = tenant
		if c.ID == "" {
			c.ID = newChannelID()
		}
		s.AddChannel(&c)
		writeJSON(w, http.StatusCreated, c)
	}
}

func listNotifyRulesHandler(s notify.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		rules := s.ListRules(tenant)
		lastBy := s.LastDeliveryPerRule(tenant)
		// Enrich each rule with last_delivered_at so the UI can render
		// "last fired" inline without a second round-trip per row.
		out := make([]map[string]any, 0, len(rules))
		for _, rule := range rules {
			item := map[string]any{
				"id":           rule.ID,
				"tenant_id":    rule.TenantID,
				"name":         rule.Name,
				"channel_id":   rule.ChannelID,
				"event_types":  rule.EventTypes,
				"min_severity": rule.MinSeverity,
				"enabled":      rule.Enabled,
				"created_at":   rule.CreatedAt,
			}
			if t, ok := lastBy[rule.ID]; ok {
				item["last_delivered_at"] = t
			}
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{"rules": out})
	}
}

func createNotifyRuleHandler(s notify.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var rule notify.Rule
		if err := decodeJSON(r, &rule); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if rule.ChannelID == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "channel_id required"})
			return
		}
		rule.TenantID = tenant
		if rule.ID == "" {
			rule.ID = newChannelID()
		}
		s.AddRule(rule)
		writeJSON(w, http.StatusCreated, rule)
	}
}

func listDeliveriesHandler(s notify.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		out, err := s.ListDeliveries(r.Context(), tenant, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deliveries": out})
	}
}

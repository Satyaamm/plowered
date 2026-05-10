package http

import (
	"net/http"
	"sync"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/dsr"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/storage"
)

// StatsDeps groups the read-only repos /v1/stats needs. Each field is
// optional — missing repos return zero counts rather than an error so
// the endpoint stays useful in a half-wired memory deployment.
type StatsDeps struct {
	Catalog     storage.Store
	Pipelines   pipeline.Repo
	Quality     quality.Store
	Deleted     deleted.Repo
	LegalHolds  legalhold.Repo
	DSR         dsr.Repo
	Connections connection.Repo
}

// StatsResponse is the JSON the home page reads. Single-roundtrip to fill
// every tile so the dashboard renders in one network hop.
type StatsResponse struct {
	Catalog struct {
		Total   int            `json:"total"`
		ByType  map[string]int `json:"by_type"`
		Tagged  int            `json:"tagged"`
	} `json:"catalog"`
	Pipelines     int `json:"pipelines"`
	Checks        int `json:"checks"`
	FailingChecks int `json:"failing_checks"`
	DeletedActive int `json:"deleted_active"`
	HoldsActive   int `json:"holds_active"`
	DSROpen       int `json:"dsr_open"`
	Connections   int `json:"connections"`
	HealthyConns  int `json:"healthy_connections"`
}

// statsHandler fans out the read calls in parallel — every count is a
// single index hit and each repo guards itself against tenant leakage,
// so concurrency here is safe and roughly halves the home-page TTFB
// over the serial alternative.
func statsHandler(d StatsDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var (
			out StatsResponse
			wg  sync.WaitGroup
		)
		out.Catalog.ByType = map[string]int{}

		// Catalog: list and aggregate. ListAssets reads tenant from
		// context, so we can let it walk the index.
		if d.Catalog != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				assets, _, err := d.Catalog.ListAssets(r.Context(), storage.ListAssetsOptions{
					PageSize: 1000,
				})
				if err != nil {
					return
				}
				out.Catalog.Total = len(assets)
				for _, a := range assets {
					out.Catalog.ByType[string(a.Type)]++
					if len(a.Tags) > 0 {
						out.Catalog.Tagged++
					}
				}
			}()
		}

		if d.Pipelines != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ps, err := d.Pipelines.ListPipelines(r.Context(), tenant)
				if err == nil {
					out.Pipelines = len(ps)
				}
			}()
		}

		if d.Quality != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cs, err := d.Quality.ListChecks(r.Context(), tenant, "")
				if err != nil {
					return
				}
				out.Checks = len(cs)
				// FailingChecks = checks whose latest run was a fail. The
				// quality.Check struct doesn't surface a denormalised
				// last-outcome field today — Track 2 wires the run-history
				// summary onto Check; until then this stays 0.
			}()
		}

		if d.Deleted != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ds, err := d.Deleted.List(r.Context(), tenant, deleted.ListOptions{Limit: 500})
				if err == nil {
					out.DeletedActive = len(ds)
				}
			}()
		}

		if d.LegalHolds != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				holds, err := d.LegalHolds.List(r.Context(), tenant)
				if err != nil {
					return
				}
				for _, h := range holds {
					if h.IsActive() {
						out.HoldsActive++
					}
				}
			}()
		}

		if d.DSR != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rs, err := d.DSR.List(r.Context(), tenant)
				if err != nil {
					return
				}
				for _, req := range rs {
					if req.Status == dsr.StatusReceived || req.Status == dsr.StatusProcessing {
						out.DSROpen++
					}
				}
			}()
		}

		if d.Connections != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cs, err := d.Connections.List(r.Context(), tenant)
				if err != nil {
					return
				}
				out.Connections = len(cs)
				for _, c := range cs {
					if c.Health == connection.HealthHealthy {
						out.HealthyConns++
					}
				}
			}()
		}

		wg.Wait()
		writeJSON(w, http.StatusOK, out)
	}
}


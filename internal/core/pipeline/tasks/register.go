// Package tasks wires every built-in pipeline task executor into a
// pipeline.Registry. It exists so cmd/plowered and cmd/plowered-worker
// share a single registration call instead of duplicating the import
// list (which would let one binary support a task type the other
// silently doesn't).
package tasks

import (
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/lineage"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/connectorsync"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/qualitycheck"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/sql"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/taskdeps"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/transformrun"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/webhook"
	"github.com/Satyaamm/plowered/internal/core/quality"
)

// Deps bundles every cross-cutting dep an executor might want. Pass a
// nil ConnFactory to skip the SQL/transform/copy executors (they error
// at execute time anyway, but registration would lie).
type Deps struct {
	ConnFactory     taskdeps.ConnFactory
	HTTPClient      *http.Client
	QualityStore    quality.Store
	QualityRunner   *quality.Scheduler
	QualityResolver qualitycheck.Resolver
	ColumnLineage   lineage.ColumnSink // optional; transform_run uses it
}

// RegisterAll registers every built-in executor on reg. Skipping deps is
// best-effort: an executor whose deps are nil still registers (so the
// task type is *known* to the registry and produces a clear error at
// runtime), but it will fail any execute attempt with a descriptive
// "deps not configured" error.
func RegisterAll(reg *pipeline.Registry, d Deps) {
	reg.MustRegister(sql.New(d.ConnFactory))
	reg.MustRegister(transformrun.New(d.ConnFactory, d.ColumnLineage))
	reg.MustRegister(connectorsync.New(d.ConnFactory))
	reg.MustRegister(webhook.New(d.HTTPClient))
	reg.MustRegister(qualitycheck.New(d.QualityStore, d.QualityRunner, d.QualityResolver))
}

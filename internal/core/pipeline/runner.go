package pipeline

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Satyaamm/plowered/internal/core/events"
)

// Runner executes a Run end-to-end. One Runner is shared across all Runs in
// a process; it dispatches to per-Run goroutines internally.
type Runner struct {
	Store    Store
	Registry *Registry
	Events   events.Bus
	Logs     LogSink // append-only log of executor progress; nil = no-op
	Now      func() time.Time
	Logger   *slog.Logger
}

// Store is the persistence interface a Runner needs. Defined here at the
// consumer side so the storage package can depend on internal/core/pipeline
// without a cycle.
type Store interface {
	GetPipeline(ctx context.Context, id string) (*Pipeline, error)
	GetRun(ctx context.Context, id string) (*Run, error)
	UpdateRun(ctx context.Context, r *Run) error
	CreateTaskRun(ctx context.Context, tr *TaskRun) (*TaskRun, error)
	UpdateTaskRun(ctx context.Context, tr *TaskRun) error
	ListTaskRuns(ctx context.Context, runID string) ([]*TaskRun, error)
}

// RunOnce drives a single Run to completion. Returns the final RunStatus.
//
// Behavior:
//   - Topological sort tasks; execute one depth level at a time, in lexical
//     order within a level (concurrency tunable on Pipeline.Concurrency in
//     a follow-up — v0 is sequential for reproducibility).
//   - Per-task retries via Task.Retry (or pipeline.DefaultRetry when zero).
//   - Failed task → downstream tasks marked TaskSkipped (no execution).
//   - FailFast pipelines short-circuit at the first failure.
func (r *Runner) RunOnce(ctx context.Context, runID string) (RunStatus, error) {
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.Logger == nil {
		r.Logger = slog.Default()
	}

	run, err := r.Store.GetRun(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("load run: %w", err)
	}
	if run.Status != RunQueued && run.Status != RunRunning {
		return run.Status, nil // already terminal; idempotent
	}

	pipeline, err := r.Store.GetPipeline(ctx, run.PipelineID)
	if err != nil {
		return "", fmt.Errorf("load pipeline: %w", err)
	}

	levels, err := TopologicalSort(pipeline.Tasks)
	if err != nil {
		run.Status = RunFailed
		run.FinishedAt = r.Now().UTC()
		_ = r.Store.UpdateRun(ctx, run)
		return RunFailed, err
	}

	run.Status = RunRunning
	run.StartedAt = r.Now().UTC()
	if err := r.Store.UpdateRun(ctx, run); err != nil {
		return "", err
	}
	r.publish(ctx, events.Event{
		Type: events.RunStarted, TenantID: run.TenantID,
		ResourceID: run.ID, ResourceType: "run",
	})
	_ = r.logSink().Append(ctx, LogLine{
		TenantID: run.TenantID, RunID: run.ID, Level: "info",
		Line: fmt.Sprintf("run started (pipeline=%s, %d tasks)", pipeline.Name, len(pipeline.Tasks)),
	})

	taskByID := make(map[string]Task, len(pipeline.Tasks))
	for _, t := range pipeline.Tasks {
		taskByID[t.ID] = t
	}
	taskRuns := make(map[string]*TaskRun, len(pipeline.Tasks))
	pipelineFailed := false

	for _, level := range levels {
		for _, taskID := range level {
			task := taskByID[taskID]

			// If any dependency failed/skipped, this task is skipped.
			if shouldSkip(task, taskRuns) {
				skipped := r.startTaskRun(ctx, run, task)
				skipped.Status = TaskSkipped
				skipped.FinishedAt = r.Now().UTC()
				_ = r.Store.UpdateTaskRun(ctx, skipped)
				taskRuns[taskID] = skipped
				continue
			}

			tr := r.executeTaskWithRetry(ctx, pipeline, run, task)
			taskRuns[taskID] = tr

			if tr.Status == TaskFailed {
				pipelineFailed = true
				if pipeline.FailFast {
					// short-circuit: mark all unstarted tasks as skipped
					r.skipRemaining(ctx, run, levels, taskRuns, pipeline.Tasks)
					goto done
				}
			}
		}
	}

done:
	run.FinishedAt = r.Now().UTC()
	if pipelineFailed {
		run.Status = RunFailed
		r.publish(ctx, events.Event{
			Type: events.RunFailed, TenantID: run.TenantID,
			ResourceID: run.ID, ResourceType: "run",
		})
		_ = r.logSink().Append(ctx, LogLine{
			TenantID: run.TenantID, RunID: run.ID, Level: "error",
			Line: "run finished: failed",
		})
	} else {
		run.Status = RunSucceeded
		r.publish(ctx, events.Event{
			Type: events.RunSucceeded, TenantID: run.TenantID,
			ResourceID: run.ID, ResourceType: "run",
		})
		_ = r.logSink().Append(ctx, LogLine{
			TenantID: run.TenantID, RunID: run.ID, Level: "info",
			Line: "run finished: succeeded",
		})
	}
	if err := r.Store.UpdateRun(ctx, run); err != nil {
		return run.Status, err
	}
	return run.Status, nil
}

// executeTaskWithRetry creates a TaskRun and runs the executor up to
// task.Retry.MaxAttempts times.
func (r *Runner) executeTaskWithRetry(ctx context.Context, p *Pipeline, run *Run, task Task) *TaskRun {
	tr := r.startTaskRun(ctx, run, task)
	policy := task.Retry
	if policy.MaxAttempts == 0 {
		policy = DefaultRetry
	}

	exec, err := r.Registry.Get(task.Type)
	if err != nil {
		tr.Status = TaskFailed
		tr.Error = err.Error()
		tr.FinishedAt = r.Now().UTC()
		tr.DeadLetter = true
		_ = r.Store.UpdateTaskRun(ctx, tr)
		r.publish(ctx, events.Event{
			Type: events.TaskFailed, TenantID: run.TenantID,
			ResourceID: tr.ID, ResourceType: "task_run",
			Attributes: map[string]any{"reason": "no_executor", "type": string(task.Type)},
		})
		return tr
	}

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if d := policy.BackoffFor(attempt); d > 0 {
			select {
			case <-ctx.Done():
				tr.Status = TaskFailed
				tr.Error = ctx.Err().Error()
				tr.FinishedAt = r.Now().UTC()
				_ = r.Store.UpdateTaskRun(ctx, tr)
				return tr
			case <-time.After(d):
			}
		}
		tr.AttemptCount = attempt
		if attempt > 1 {
			tr.Status = TaskRetrying
			_ = r.Store.UpdateTaskRun(ctx, tr)
		}

		ec := ExecutionContext{
			Pipeline: p, Task: &task, Run: run, TaskRun: tr,
			Logs: r.logSink(),
		}
		ec.Log(ctx, "info", "task %s attempt %d starting (type=%s)", task.ID, attempt, task.Type)
		out, err := exec.Execute(ctx, ec)
		if err == nil {
			tr.Status = TaskSucceeded
			tr.FinishedAt = r.Now().UTC()
			tr.Output = out.Properties
			_ = r.Store.UpdateTaskRun(ctx, tr)
			ec.Log(ctx, "info", "task %s succeeded (attempt %d)", task.ID, attempt)
			r.publish(ctx, events.Event{
				Type: events.TaskSucceeded, TenantID: run.TenantID,
				ResourceID: tr.ID, ResourceType: "task_run",
			})
			return tr
		}
		tr.Error = err.Error()
		ec.Log(ctx, "error", "task %s attempt %d failed: %s", task.ID, attempt, err.Error())
		if attempt == policy.MaxAttempts {
			tr.Status = TaskFailed
			tr.FinishedAt = r.Now().UTC()
			tr.DeadLetter = true
			_ = r.Store.UpdateTaskRun(ctx, tr)
			r.publish(ctx, events.Event{
				Type: events.TaskFailed, TenantID: run.TenantID,
				ResourceID: tr.ID, ResourceType: "task_run",
				Attributes: map[string]any{"error": err.Error(), "attempts": attempt},
			})
			return tr
		}
	}
	return tr
}

func (r *Runner) startTaskRun(ctx context.Context, run *Run, task Task) *TaskRun {
	tr := &TaskRun{
		ID:        newID(),
		TenantID:  run.TenantID,
		RunID:     run.ID,
		TaskID:    task.ID,
		Status:    TaskRunning,
		StartedAt: r.Now().UTC(),
	}
	created, err := r.Store.CreateTaskRun(ctx, tr)
	if err != nil {
		// Best-effort: keep the in-memory tr so the run can continue.
		r.Logger.WarnContext(ctx, "create task run", "err", err, "task", task.ID)
		return tr
	}
	return created
}

// shouldSkip returns true when any of task's dependencies didn't succeed.
func shouldSkip(task Task, prior map[string]*TaskRun) bool {
	for _, d := range task.DependsOn {
		dep, ok := prior[d]
		if !ok {
			return true
		}
		if dep.Status != TaskSucceeded {
			return true
		}
	}
	return false
}

func (r *Runner) skipRemaining(ctx context.Context, run *Run, levels [][]string, started map[string]*TaskRun, tasks []Task) {
	all := make(map[string]Task, len(tasks))
	for _, t := range tasks {
		all[t.ID] = t
	}
	for _, level := range levels {
		for _, id := range level {
			if _, done := started[id]; done {
				continue
			}
			tr := r.startTaskRun(ctx, run, all[id])
			tr.Status = TaskSkipped
			tr.FinishedAt = r.Now().UTC()
			_ = r.Store.UpdateTaskRun(ctx, tr)
			started[id] = tr
		}
	}
}

func (r *Runner) logSink() LogSink {
	if r.Logs == nil {
		return NoopSink{}
	}
	return r.Logs
}

func (r *Runner) publish(ctx context.Context, e events.Event) {
	if r.Events == nil {
		return
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = r.Now().UTC()
	}
	r.Events.Publish(ctx, e)
}

// newID returns a UUIDv4 string. The Postgres TaskRun store stores IDs
// as `uuid`, so any pre-filled value from the runner has to parse as one.
// (Earlier versions emitted 24-char hex strings, which the store rejected.)
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC4122 variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// HelperRetryAttempts is exposed for tests that want to assert about the
// runner's retry behavior without constructing full Runs.
var ErrUnknownPipeline = errors.New("pipeline: unknown id")

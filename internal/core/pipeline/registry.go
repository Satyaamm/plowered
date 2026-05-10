package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// sprintf is a thin alias so the ExecutionContext.Log helper can format
// without importing fmt at every call site. Renamed so the package
// import remains a single use.
var sprintf = fmt.Sprintf

// Executor runs one task. Implementations live under
// internal/core/pipeline/tasks/<type>/ and self-register at init time.
type Executor interface {
	Type() TaskType
	Execute(ctx context.Context, ec ExecutionContext) (Output, error)
}

// ExecutionContext is everything an Executor needs. The Runner constructs it
// fresh per attempt.
type ExecutionContext struct {
	Pipeline *Pipeline
	Task     *Task
	Run      *Run
	TaskRun  *TaskRun
	Logs     LogSink
}

// Log records a progress line scoped to this attempt. Safe to call when
// no sink is wired — the no-op sink swallows the call. level is one of
// "info" | "warn" | "error"; format/args mirror fmt.Sprintf.
func (ec ExecutionContext) Log(ctx context.Context, level, format string, args ...any) {
	sink := ec.Logs
	if sink == nil {
		sink = NoopSink{}
	}
	line := format
	if len(args) > 0 {
		line = sprintf(format, args...)
	}
	tenant, run, taskRun, taskID := "", "", "", ""
	if ec.Run != nil {
		tenant = ec.Run.TenantID
		run = ec.Run.ID
	}
	if ec.TaskRun != nil {
		taskRun = ec.TaskRun.ID
	}
	if ec.Task != nil {
		taskID = ec.Task.ID
	}
	_ = sink.Append(ctx, LogLine{
		TenantID: tenant, RunID: run, TaskRunID: taskRun, TaskID: taskID,
		Level: level, Line: line,
	})
}

// Output is the task's structured result. Properties land on TaskRun.Output.
// LineageEdges are written to the metadata graph by the Runner so executors
// don't need a Store reference.
type Output struct {
	Properties map[string]any
	// LineageEdges, when non-nil, are appended to the graph after the task
	// succeeds. Edges should set source_qn / target_qn in Properties; the
	// BatchedSink resolves them to UUIDs.
	LineageEdges []LineageEdgeProposal
}

// LineageEdgeProposal is a graph edge expressed as qualified-name pairs
// (resolved at write time). Avoids importing graph here so executors can be
// independent of the graph package.
type LineageEdgeProposal struct {
	SourceQN   string
	TargetQN   string
	Op         string
	Properties map[string]any
}

// Registry is a process-local map of TaskType → Executor.
type Registry struct {
	mu        sync.RWMutex
	executors map[TaskType]Executor
}

func NewRegistry() *Registry {
	return &Registry{executors: make(map[TaskType]Executor)}
}

// DefaultRegistry is populated by init() in built-in tasks/<type>/ packages.
var DefaultRegistry = NewRegistry()

func (r *Registry) Register(e Executor) error {
	if e == nil || e.Type() == "" {
		return errors.New("registry: executor and type required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.executors[e.Type()]; exists {
		return fmt.Errorf("registry: type %q already registered", e.Type())
	}
	r.executors[e.Type()] = e
	return nil
}

func (r *Registry) MustRegister(e Executor) {
	if err := r.Register(e); err != nil {
		panic(err)
	}
}

func (r *Registry) Get(t TaskType) (Executor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.executors[t]
	if !ok {
		return nil, fmt.Errorf("registry: no executor for type %q", t)
	}
	return e, nil
}

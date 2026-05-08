// Package shared defines the connector framework. Concrete connectors live
// under internal/connectors/<name>/ and implement the Connector interface.
//
// Lifecycle of a single sync:
//
//	cfg := loadConfig(...)
//	if err := c.Validate(ctx, cfg); err != nil { ... }
//	run := NewRun(c.Info().Name, cfg.InstanceID)
//	if err := c.Crawl(ctx, cfg, sink); err != nil { run.Fail(err) }
//	sink.Flush(ctx)
//	run.Succeed(sink.Stats())
package shared

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/graph"
)

// Connector is the contract every data-source adapter must implement.
type Connector interface {
	Info() Info
	Validate(ctx context.Context, cfg Config) error
	Crawl(ctx context.Context, cfg Config, sink Sink) error
}

// Info describes a connector statically (no per-instance config needed).
type Info struct {
	Name              string
	Version           string
	SupportedAssetTypes []graph.AssetType
	SupportsLineage   bool
}

// Config carries connector-specific settings. It intentionally uses a
// dynamic map so the framework can stay neutral about per-connector schemas;
// each connector validates its own keys via Validate().
type Config struct {
	InstanceID string
	TenantID   string
	Values     map[string]any
}

// String returns the value at key, or def if absent / wrong type.
func (c Config) String(key, def string) string {
	if v, ok := c.Values[key].(string); ok && v != "" {
		return v
	}
	return def
}

// Bool returns the value at key, or def if absent / wrong type.
func (c Config) Bool(key string, def bool) bool {
	if v, ok := c.Values[key].(bool); ok {
		return v
	}
	return def
}

// Required returns an error if any of the named keys is missing or empty.
func (c Config) Required(keys ...string) error {
	var missing []string
	for _, k := range keys {
		v, ok := c.Values[k].(string)
		if !ok || v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("connector config missing keys: %s", strings.Join(missing, ", "))
	}
	return nil
}

// RunStatus tracks the lifecycle of a single sync invocation.
type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// Run is a single execution of a connector. The framework owns its lifecycle.
type Run struct {
	ID            string
	ConnectorName string
	InstanceID    string
	Status        RunStatus
	StartedAt     time.Time
	FinishedAt    time.Time
	AssetsSeen    int64
	EdgesSeen     int64
	Err           string
}

func NewRun(connectorName, instanceID string) *Run {
	return &Run{
		ID:            newID(),
		ConnectorName: connectorName,
		InstanceID:    instanceID,
		Status:        RunRunning,
		StartedAt:     time.Now().UTC(),
	}
}

func (r *Run) Succeed(stats SinkStats) {
	r.Status = RunSucceeded
	r.FinishedAt = time.Now().UTC()
	r.AssetsSeen = stats.Assets
	r.EdgesSeen = stats.Edges
}

func (r *Run) Fail(err error) {
	r.Status = RunFailed
	r.FinishedAt = time.Now().UTC()
	if err != nil {
		r.Err = err.Error()
	}
}

func (r *Run) Cancel() {
	r.Status = RunCancelled
	r.FinishedAt = time.Now().UTC()
}

// Duration returns how long the run took (or has been running).
func (r *Run) Duration() time.Duration {
	end := r.FinishedAt
	if end.IsZero() {
		end = time.Now().UTC()
	}
	return end.Sub(r.StartedAt)
}

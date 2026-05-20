package contract

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/events"
	"github.com/Satyaamm/plowered/internal/core/profile"
)

// ProfileReader is the slice of the profile cache the evaluator needs.
type ProfileReader interface {
	Get(ctx context.Context, tenantID, tableAssetID string) (*profile.Report, error)
}

// Service is the application surface the HTTP layer holds.
type Service struct {
	Store   Store
	Profile ProfileReader
	Events  events.Bus  // optional; published BreachDetected events route through notify
	Logger  *slog.Logger
	Now     func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// Upsert creates a contract or bumps the version on the existing one.
// The store does the dedupe via UNIQUE(tenant_id, asset_id).
func (s *Service) Upsert(ctx context.Context, c *Contract) (*Contract, error) {
	if c == nil {
		return nil, errors.New("contract: nil")
	}
	if c.TenantID == "" || c.AssetID == "" {
		return nil, errors.New("contract: tenant + asset required")
	}
	if c.Status == "" {
		c.Status = StatusActive
	}
	return s.Store.UpsertContract(ctx, c)
}

// Get / List / Delete are thin pass-throughs.
func (s *Service) Get(ctx context.Context, tenantID, id string) (*Contract, error) {
	return s.Store.GetContract(ctx, tenantID, id)
}

func (s *Service) GetByAsset(ctx context.Context, tenantID, assetID string) (*Contract, error) {
	return s.Store.GetContractByAsset(ctx, tenantID, assetID)
}

func (s *Service) List(ctx context.Context, tenantID string) ([]*Contract, error) {
	return s.Store.ListContracts(ctx, tenantID)
}

func (s *Service) Delete(ctx context.Context, tenantID, id string) error {
	return s.Store.DeleteContract(ctx, tenantID, id)
}

// Breaches surfaces the breach history for the UI.
func (s *Service) Breaches(ctx context.Context, tenantID string, limit int) ([]*Breach, error) {
	return s.Store.ListBreaches(ctx, tenantID, limit)
}

func (s *Service) BreachesForContract(ctx context.Context, tenantID, contractID string, limit int) ([]*Breach, error) {
	return s.Store.ListBreachesForContract(ctx, tenantID, contractID, limit)
}

// Evaluate runs one validation cycle for a single contract. Pulls the
// latest profile and emits a Breach row + an events.Bus message per
// detected violation. No-op when status != active. Errors during a
// single check don't abort the rest — we want a freshness report
// even when null-threshold evaluation fails.
//
// Returns the list of breaches recorded this run (possibly empty).
func (s *Service) Evaluate(ctx context.Context, tenantID, contractID string) ([]*Breach, error) {
	if s.Store == nil || s.Profile == nil {
		return nil, errors.New("contract: service not fully configured")
	}
	c, err := s.Store.GetContract(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	if c.Status != StatusActive {
		return nil, nil
	}
	report, err := s.Profile.Get(ctx, tenantID, c.AssetID)
	if err != nil {
		// No profile yet ⇒ can't evaluate. That's not a breach; the
		// caller decides whether to trigger a profile run.
		return nil, nil
	}

	var out []*Breach
	now := s.now().UTC()
	for _, b := range detectBreaches(c, report, now) {
		saved, err := s.Store.RecordBreach(ctx, b)
		if err != nil {
			s.logger().WarnContext(ctx, "contract: record breach", "err", err)
			continue
		}
		out = append(out, saved)
		s.publishBreach(ctx, c, saved)
	}
	return out, nil
}

// EvaluateAll iterates every active contract. Used by the periodic
// evaluator job + on-demand from the HTTP layer.
func (s *Service) EvaluateAll(ctx context.Context, tenantID string) (int, error) {
	contracts, err := s.Store.ListContracts(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	var breachCount int
	for _, c := range contracts {
		if c.Status != StatusActive {
			continue
		}
		breaches, err := s.Evaluate(ctx, tenantID, c.ID)
		if err != nil {
			s.logger().WarnContext(ctx, "contract: evaluate", "contract", c.ID, "err", err)
			continue
		}
		breachCount += len(breaches)
	}
	return breachCount, nil
}

func (s *Service) publishBreach(ctx context.Context, c *Contract, b *Breach) {
	if s.Events == nil {
		return
	}
	s.Events.Publish(ctx, events.Event{
		ID:           b.ID,
		Type:         events.CheckFailed, // reuse check.failed so existing rules at severity>=error fire
		Severity:     events.Severity(b.Severity),
		TenantID:     b.TenantID,
		ResourceType: "contract_breach",
		ResourceID:   b.ID,
		Attributes: map[string]any{
			"contract_id":      c.ID,
			"contract_version": c.Version,
			"asset_id":         c.AssetID,
			"kind":             string(b.Kind),
			"message":          b.Message,
			"observed":         b.Observed,
			"expected":         b.Expected,
		},
		OccurredAt: b.ObservedAt,
	})
}

// detectBreaches is the pure-function evaluator. Kept separate from
// Evaluate so it's directly testable without touching the store/bus.
func detectBreaches(c *Contract, r *profile.Report, now time.Time) []*Breach {
	var out []*Breach
	out = append(out, schemaDriftBreaches(c, r, now)...)
	if b := freshnessBreach(c, r, now); b != nil {
		out = append(out, b)
	}
	out = append(out, nullThresholdBreaches(c, r, now)...)
	return out
}

func schemaDriftBreaches(c *Contract, r *profile.Report, now time.Time) []*Breach {
	if len(c.ExpectedColumns) == 0 {
		return nil
	}
	expected := map[string]string{}
	for _, ec := range c.ExpectedColumns {
		expected[strings.ToLower(ec.Name)] = strings.ToLower(ec.Type)
	}
	observed := map[string]string{}
	for _, col := range r.Columns {
		observed[strings.ToLower(col.Name)] = strings.ToLower(col.DataType)
	}
	var missing, extra []string
	typeChanges := map[string]map[string]string{}
	for name, expType := range expected {
		obsType, ok := observed[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if expType != "" && obsType != "" && expType != obsType {
			typeChanges[name] = map[string]string{"expected": expType, "observed": obsType}
		}
	}
	for name := range observed {
		if _, ok := expected[name]; !ok {
			extra = append(extra, name)
		}
	}
	if len(missing) == 0 && len(extra) == 0 && len(typeChanges) == 0 {
		return nil
	}
	expList := make([]map[string]string, 0, len(c.ExpectedColumns))
	for _, ec := range c.ExpectedColumns {
		expList = append(expList, map[string]string{"name": ec.Name, "type": ec.Type})
	}
	return []*Breach{{
		TenantID:        c.TenantID,
		ContractID:      c.ID,
		AssetID:         c.AssetID,
		ContractVersion: c.Version,
		Kind:            BreachSchemaDrift,
		Severity:        "error",
		Observed: map[string]any{
			"missing_columns": missing,
			"extra_columns":   extra,
			"type_changes":    typeChanges,
		},
		Expected: map[string]any{"columns": expList},
		Message:  schemaDriftMessage(missing, extra, typeChanges),
		ObservedAt: now,
	}}
}

func schemaDriftMessage(missing, extra []string, typeChanges map[string]map[string]string) string {
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("missing %d", len(missing)))
	}
	if len(extra) > 0 {
		parts = append(parts, fmt.Sprintf("extra %d", len(extra)))
	}
	if len(typeChanges) > 0 {
		parts = append(parts, fmt.Sprintf("type-changed %d", len(typeChanges)))
	}
	return "schema drift: " + strings.Join(parts, ", ")
}

func freshnessBreach(c *Contract, r *profile.Report, now time.Time) *Breach {
	if c.FreshnessSeconds <= 0 {
		return nil
	}
	age := now.Sub(r.GeneratedAt)
	max := time.Duration(c.FreshnessSeconds) * time.Second
	if age <= max {
		return nil
	}
	return &Breach{
		TenantID:        c.TenantID,
		ContractID:      c.ID,
		AssetID:         c.AssetID,
		ContractVersion: c.Version,
		Kind:            BreachFreshness,
		Severity:        "warning",
		Observed: map[string]any{
			"profile_generated_at": r.GeneratedAt.UTC().Format(time.RFC3339),
			"age_seconds":          int(age.Seconds()),
		},
		Expected: map[string]any{
			"max_age_seconds": c.FreshnessSeconds,
		},
		Message:    fmt.Sprintf("profile %ds old, max %ds", int(age.Seconds()), c.FreshnessSeconds),
		ObservedAt: now,
	}
}

func nullThresholdBreaches(c *Contract, r *profile.Report, now time.Time) []*Breach {
	if len(c.NullThresholds) == 0 {
		return nil
	}
	byName := map[string]profile.Column{}
	for _, col := range r.Columns {
		byName[strings.ToLower(col.Name)] = col
	}
	var out []*Breach
	for colName, threshold := range c.NullThresholds {
		col, ok := byName[strings.ToLower(colName)]
		if !ok || col.RowsSampled == 0 {
			continue
		}
		nullFraction := float64(col.NullCount) / float64(col.RowsSampled)
		if nullFraction <= threshold {
			continue
		}
		out = append(out, &Breach{
			TenantID:        c.TenantID,
			ContractID:      c.ID,
			AssetID:         c.AssetID,
			ContractVersion: c.Version,
			Kind:            BreachNullThreshold,
			Severity:        "error",
			Observed: map[string]any{
				"column":        colName,
				"null_fraction": nullFraction,
			},
			Expected: map[string]any{
				"column":              colName,
				"max_null_fraction":   threshold,
			},
			Message:    fmt.Sprintf("%s null fraction %.3f > %.3f", colName, nullFraction, threshold),
			ObservedAt: now,
		})
	}
	return out
}

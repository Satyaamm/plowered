package contract

import (
	"testing"
	"time"

	"github.com/Satyaamm/plowered/internal/core/profile"
)

func ts(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func TestNoExpectationsNoBreaches(t *testing.T) {
	c := &Contract{TenantID: "t", AssetID: "a", Status: StatusActive, Version: 1}
	r := &profile.Report{GeneratedAt: ts("2026-05-20T10:00:00Z")}
	if got := detectBreaches(c, r, ts("2026-05-20T10:01:00Z")); len(got) != 0 {
		t.Errorf("empty contract should produce no breaches, got %d", len(got))
	}
}

func TestSchemaDriftMissingAndExtra(t *testing.T) {
	c := &Contract{
		TenantID: "t", AssetID: "a", Version: 1,
		ExpectedColumns: []ExpectedColumn{
			{Name: "id", Type: "int"},
			{Name: "email", Type: "string"},
		},
	}
	r := &profile.Report{
		GeneratedAt: ts("2026-05-20T10:00:00Z"),
		Columns: []profile.Column{
			{Name: "id", DataType: "int"},
			{Name: "phone", DataType: "string"}, // missing email, extra phone
		},
	}
	bs := detectBreaches(c, r, ts("2026-05-20T10:01:00Z"))
	if len(bs) != 1 || bs[0].Kind != BreachSchemaDrift {
		t.Fatalf("want one schema_drift breach, got %+v", bs)
	}
	obs := bs[0].Observed
	missing, _ := obs["missing_columns"].([]string)
	extra, _ := obs["extra_columns"].([]string)
	if len(missing) != 1 || missing[0] != "email" {
		t.Errorf("missing: %v", missing)
	}
	if len(extra) != 1 || extra[0] != "phone" {
		t.Errorf("extra: %v", extra)
	}
}

func TestSchemaDriftTypeChange(t *testing.T) {
	c := &Contract{TenantID: "t", AssetID: "a", Version: 1,
		ExpectedColumns: []ExpectedColumn{{Name: "id", Type: "int"}}}
	r := &profile.Report{
		Columns: []profile.Column{{Name: "id", DataType: "string"}},
	}
	bs := detectBreaches(c, r, ts("2026-05-20T10:00:00Z"))
	if len(bs) != 1 {
		t.Fatalf("want one breach, got %d", len(bs))
	}
	changes, _ := bs[0].Observed["type_changes"].(map[string]map[string]string)
	if changes["id"]["expected"] != "int" || changes["id"]["observed"] != "string" {
		t.Errorf("type_changes: %v", changes)
	}
}

func TestFreshnessBreachOnlyWhenOlderThanThreshold(t *testing.T) {
	c := &Contract{TenantID: "t", AssetID: "a", Version: 1, FreshnessSeconds: 3600}
	r := &profile.Report{GeneratedAt: ts("2026-05-20T08:00:00Z")} // 2h old at now
	now := ts("2026-05-20T10:00:00Z")
	bs := detectBreaches(c, r, now)
	if len(bs) != 1 || bs[0].Kind != BreachFreshness {
		t.Fatalf("want freshness breach, got %+v", bs)
	}
	// Within threshold ⇒ no breach.
	r.GeneratedAt = ts("2026-05-20T09:30:00Z") // 30min old
	if got := detectBreaches(c, r, now); len(got) != 0 {
		t.Errorf("within threshold should not breach, got %+v", got)
	}
}

func TestNullThresholdBreach(t *testing.T) {
	c := &Contract{TenantID: "t", AssetID: "a", Version: 1,
		NullThresholds: map[string]float64{"email": 0.05}}
	r := &profile.Report{
		Columns: []profile.Column{
			{Name: "email", RowsSampled: 1000, NullCount: 200}, // 20% > 5%
		},
	}
	bs := detectBreaches(c, r, ts("2026-05-20T10:00:00Z"))
	if len(bs) != 1 || bs[0].Kind != BreachNullThreshold {
		t.Fatalf("want null_threshold breach, got %+v", bs)
	}
	if msg := bs[0].Message; msg == "" {
		t.Error("message empty")
	}
}

func TestNullThresholdNoBreachWhenWithin(t *testing.T) {
	c := &Contract{TenantID: "t", AssetID: "a", Version: 1,
		NullThresholds: map[string]float64{"email": 0.5}}
	r := &profile.Report{
		Columns: []profile.Column{{Name: "email", RowsSampled: 100, NullCount: 10}}, // 10% < 50%
	}
	if got := detectBreaches(c, r, ts("2026-05-20T10:00:00Z")); len(got) != 0 {
		t.Errorf("should not breach, got %+v", got)
	}
}

func TestBreachSigStableAcrossKeyOrder(t *testing.T) {
	a := breachSig(BreachFreshness, map[string]any{"a": 1, "b": 2})
	b := breachSig(BreachFreshness, map[string]any{"b": 2, "a": 1})
	if a != b {
		t.Errorf("sig depends on map iteration order: %s vs %s", a, b)
	}
}

func TestBreachSigDiffersByKind(t *testing.T) {
	observed := map[string]any{"x": 1}
	if breachSig(BreachFreshness, observed) == breachSig(BreachNullThreshold, observed) {
		t.Error("different kinds should produce different sigs")
	}
}

func TestCombinedBreaches(t *testing.T) {
	c := &Contract{TenantID: "t", AssetID: "a", Version: 1,
		ExpectedColumns:  []ExpectedColumn{{Name: "id", Type: "int"}, {Name: "email", Type: "string"}},
		FreshnessSeconds: 60,
		NullThresholds:   map[string]float64{"email": 0.0},
	}
	r := &profile.Report{
		GeneratedAt: ts("2026-05-20T08:00:00Z"),
		Columns: []profile.Column{
			{Name: "id", DataType: "int"},
			{Name: "email", DataType: "string", RowsSampled: 100, NullCount: 5},
			{Name: "extra", DataType: "text"},
		},
	}
	bs := detectBreaches(c, r, ts("2026-05-20T10:00:00Z"))
	kinds := map[BreachKind]bool{}
	for _, b := range bs {
		kinds[b.Kind] = true
	}
	for _, want := range []BreachKind{BreachSchemaDrift, BreachFreshness, BreachNullThreshold} {
		if !kinds[want] {
			t.Errorf("missing breach kind %s in %+v", want, bs)
		}
	}
}

package classifier

import (
	"sort"
	"strings"
	"testing"
)

func TestClassifySamples_Email(t *testing.T) {
	samples := []string{
		"alice@example.com",
		"bob@plowered.io",
		"carol@example.org",
		"  dan@test.co  ",
		"not an email",
	}
	r := ClassifySamples("email", samples)
	if !contains(r.Tags, "class:email") {
		t.Errorf("expected class:email; got %v", r.Tags)
	}
	if !contains(r.Tags, "class:pii") {
		t.Errorf("expected umbrella class:pii; got %v", r.Tags)
	}
}

func TestClassifySamples_CreditCard(t *testing.T) {
	samples := []string{
		"4111 1111 1111 1111", // Visa test
		"5500-0000-0000-0004", // Mastercard test
		"340000000000009",     // Amex test (15-digit)
		"6011000000000004",    // Discover test
		"random text",
	}
	r := ClassifySamples("ccnum", samples)
	if !contains(r.Tags, "class:credit_card") {
		t.Errorf("expected class:credit_card; got %v", r.Tags)
	}
	if !contains(r.Tags, "class:pci") {
		t.Errorf("expected umbrella class:pci; got %v", r.Tags)
	}
}

func TestClassifySamples_NoMatch(t *testing.T) {
	r := ClassifySamples("notes", []string{"this is", "totally fine", "free text"})
	if len(r.Tags) != 0 {
		t.Errorf("expected zero tags; got %v", r.Tags)
	}
}

func TestClassifySamples_Empty(t *testing.T) {
	r := ClassifySamples("", nil)
	if r.Sampled != 0 || len(r.Tags) != 0 {
		t.Errorf("expected zero result; got %+v", r)
	}
}

func TestClassifySamples_LowSignal(t *testing.T) {
	// One email out of ten shouldn't classify the column.
	samples := []string{"foo", "bar", "baz", "qux", "alice@example.com", "x", "y", "z", "a", "b"}
	r := ClassifySamples("misc", samples)
	if contains(r.Tags, "class:email") {
		t.Errorf("expected no email tag below threshold; got %v", r.Tags)
	}
}

func contains(haystack []string, needle string) bool {
	sorted := append([]string(nil), haystack...)
	sort.Strings(sorted)
	idx := sort.SearchStrings(sorted, needle)
	return idx < len(sorted) && strings.EqualFold(sorted[idx], needle)
}

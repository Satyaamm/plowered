// Package classifier inspects sampled column values and decides what
// classifications (PII / PHI / PCI / secret kinds) the column carries.
//
// The detection is local — every pattern is a Go regexp or arithmetic
// check that runs on the sampled values themselves. No values leave the
// process. The output is a set of `class:*` tags identical to the
// namespace the column-name classifier writes, so the storage layer can
// merge auto-detected and explicit tags without having to know which
// produced which.
//
// Detection design:
//   - Each detector returns the fraction of non-null samples that match.
//   - Samples → tag mapping happens at the Classifier level: a tag is
//     attached when the matching fraction crosses the per-detector
//     threshold (default 0.7).
//   - Empty or all-null samples never produce a classification.
package classifier

import (
	"regexp"
	"strconv"
	"strings"
)

// Detector recognises one kind of value (emails, SSNs, credit cards…).
// Match returns true when the supplied value looks like the detector's
// kind; the Classifier counts true rates across all samples.
type Detector struct {
	// Kind is the tag emitted when this detector fires. Use the
	// `class:<label>` namespace.
	Kind string
	// Threshold is the minimum match fraction (0–1) required to emit
	// the tag. Per-detector tuning lets noisy patterns (UUIDs, URLs)
	// require near-100% match while looser ones (phone numbers, names)
	// fire on a majority.
	Threshold float64
	// Match runs the detector on a single value.
	Match func(string) bool
}

// All returns the built-in detector list. Order doesn't matter; the
// Classifier runs every detector against every value.
func All() []Detector {
	return []Detector{
		{Kind: "class:email",       Threshold: 0.8, Match: matchRegex(emailRE)},
		{Kind: "class:phone",       Threshold: 0.7, Match: matchPhone},
		{Kind: "class:ssn",         Threshold: 0.8, Match: matchRegex(ssnRE)},
		{Kind: "class:credit_card", Threshold: 0.6, Match: matchCreditCard},
		{Kind: "class:ip_address",  Threshold: 0.8, Match: matchRegex(ipRE)},
		{Kind: "class:url",         Threshold: 0.7, Match: matchRegex(urlRE)},
		{Kind: "class:uuid",        Threshold: 0.9, Match: matchRegex(uuidRE)},
		{Kind: "class:iban",        Threshold: 0.9, Match: matchIBAN},
		{Kind: "class:secret",      Threshold: 0.6, Match: matchSecretLooking},
	}
}

// Tags emits the `class:pii` / `class:pci` / `class:phi` umbrella tags
// alongside the specific kinds. Plowered has historically rendered the
// umbrella tags as severity colors in the UI; emitting both means the
// auto-detector matches the manual + name-based output exactly.
func Tags(kind string) []string {
	switch kind {
	case "class:email", "class:phone", "class:ssn", "class:ip_address":
		return []string{"class:pii", kind}
	case "class:credit_card", "class:iban":
		return []string{"class:pci", kind}
	case "class:secret":
		return []string{"class:secret"}
	default:
		return []string{kind}
	}
}

// --- precompiled patterns ---

var (
	emailRE = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	ssnRE   = regexp.MustCompile(`^\d{3}-\d{2}-\d{4}$|^\d{9}$`)
	ipRE    = regexp.MustCompile(`^(?:\d{1,3}\.){3}\d{1,3}$`)
	urlRE   = regexp.MustCompile(`^https?://[^\s]+$`)
	uuidRE  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

func matchRegex(re *regexp.Regexp) func(string) bool {
	return func(v string) bool { return re.MatchString(v) }
}

// matchPhone is more permissive than a regex because phone formatting
// varies wildly. Strategy: keep digits, accept anything 7..15 digits.
func matchPhone(v string) bool {
	digits := 0
	for _, r := range v {
		if r >= '0' && r <= '9' {
			digits++
		} else if r != ' ' && r != '-' && r != '.' && r != '(' && r != ')' && r != '+' && r != '/' {
			return false
		}
	}
	return digits >= 7 && digits <= 15
}

// matchCreditCard requires a 13–19 digit string passing the Luhn check.
// Spaces and dashes are tolerated; everything else disqualifies.
func matchCreditCard(v string) bool {
	digits := make([]int, 0, 19)
	for _, r := range v {
		if r >= '0' && r <= '9' {
			digits = append(digits, int(r-'0'))
		} else if r != ' ' && r != '-' {
			return false
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if (len(digits)-i)%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

// matchIBAN does a structural + mod-97 check. Matches "DE89 3704 …".
func matchIBAN(v string) bool {
	v = strings.ReplaceAll(strings.ReplaceAll(v, " ", ""), "-", "")
	if len(v) < 15 || len(v) > 34 {
		return false
	}
	for i, r := range v[:2] {
		_ = i
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	for _, r := range v[2:4] {
		if r < '0' || r > '9' {
			return false
		}
	}
	rotated := v[4:] + v[:4]
	digits := strings.Builder{}
	for _, r := range rotated {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			digits.WriteString(strconv.Itoa(int(r-'A') + 10))
		default:
			return false
		}
	}
	return mod97(digits.String()) == 1
}

func mod97(s string) int {
	rem := 0
	for _, r := range s {
		rem = (rem*10 + int(r-'0')) % 97
	}
	return rem
}

// matchSecretLooking heuristically catches "looks like an API key": long
// (≥20) random-looking strings with mixed alphanumerics and uncommon
// punctuation. Deliberately loose — false positives are much cheaper than
// missing a leaked secret because the platform classifies it as benign.
func matchSecretLooking(v string) bool {
	if len(v) < 20 || len(v) > 200 {
		return false
	}
	letters, digits, special := 0, 0, 0
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			letters++
		case r >= '0' && r <= '9':
			digits++
		case r == '_' || r == '-' || r == '.' || r == '/' || r == '+' || r == '=':
			special++
		default:
			return false
		}
	}
	// Require at least both letters and digits — pure-letter strings
	// are usually descriptive text or words, not credentials.
	return letters >= 4 && digits >= 4
}

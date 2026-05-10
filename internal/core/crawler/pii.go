package crawler

import "strings"

// ColumnNameClassifier scans column names for likely PII / sensitive
// content. The result is a set of tag names that get attached to the
// column asset on first crawl. It's intentionally conservative: false
// positives are easier to triage than false negatives, but we don't want
// to flag every column called "name" as PII.
//
// Heuristics map a normalized column name to one or more tags. The
// matching is substring-based after stripping non-alphanumerics, so
// `customer_email` and `customerEmail` both match the `email` rule.
//
// This is the v0 surface; step 3.2 layers a column-profiler on top that
// scans actual values (regex on samples) and votes against / for the
// classification.
type ColumnNameClassifier struct{}

func NewColumnNameClassifier() ColumnNameClassifier { return ColumnNameClassifier{} }

// Classify returns the set of tags to attach to a column with the given
// name. An empty slice means "no automatic classification".
func (ColumnNameClassifier) Classify(columnName string) []string {
	n := normalize(columnName)
	if n == "" {
		return nil
	}
	seen := map[string]struct{}{}
	for _, rule := range piiRules {
		for _, needle := range rule.matches {
			if strings.Contains(n, needle) {
				for _, tag := range rule.tags {
					seen[tag] = struct{}{}
				}
				break
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// normalize lower-cases and strips non-alphanumerics so the substring
// match doesn't care about snake_case / camelCase / kebab-case.
func normalize(s string) string {
	b := strings.Builder{}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

type piiRule struct {
	matches []string
	tags    []string
}

// piiRules is the lookup table. Tags use the `class:<label>` namespace
// the policy engine and SCHEMA.md document. Multiple tags per rule when
// a column is both PII and a regulated subset (e.g. SSN is PII + PCI-
// adjacent in some frameworks).
var piiRules = []piiRule{
	// Direct identifiers
	{matches: []string{"email", "emailaddress"},                tags: []string{"class:pii", "class:email"}},
	{matches: []string{"phone", "phonenumber", "mobile", "cell", "telephone"}, tags: []string{"class:pii", "class:phone"}},
	{matches: []string{"ssn", "socialsecurity", "tin"},         tags: []string{"class:pii", "class:ssn"}},
	{matches: []string{"passport", "drivinglicense", "drivinglicence", "license"}, tags: []string{"class:pii"}},
	{matches: []string{"firstname", "lastname", "fullname", "givenname", "familyname", "surname"}, tags: []string{"class:pii", "class:name"}},
	{matches: []string{"dob", "dateofbirth", "birthdate", "birthday"}, tags: []string{"class:pii"}},
	{matches: []string{"addressline", "streetaddress", "homeaddress", "billingaddress", "shippingaddress"}, tags: []string{"class:pii", "class:address"}},
	{matches: []string{"zip", "postalcode", "postcode"},        tags: []string{"class:pii"}},
	{matches: []string{"ipaddress", "clientip", "userip"},      tags: []string{"class:pii", "class:ip"}},

	// Financial
	{matches: []string{"creditcard", "cardnumber", "ccnumber", "pan"}, tags: []string{"class:pci", "class:pii"}},
	{matches: []string{"cvv", "cvc"},                            tags: []string{"class:pci"}},
	{matches: []string{"iban", "swift", "bicroute", "routingnumber", "accountnumber", "bankaccount"}, tags: []string{"class:pii", "class:financial"}},

	// Health
	{matches: []string{"patient", "diagnosis", "icd10", "icd9", "mrn"}, tags: []string{"class:phi", "class:pii"}},
	{matches: []string{"prescription", "medication"},            tags: []string{"class:phi"}},

	// Auth / secrets
	{matches: []string{"passwordhash", "pwhash", "pwd", "secret", "apikey", "accesstoken", "refreshtoken", "privatekey"},
		tags: []string{"class:secret"}},
}

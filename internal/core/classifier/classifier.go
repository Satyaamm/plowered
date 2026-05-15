package classifier

// Result is the per-column classification verdict. Tags is the merged
// set of `class:*` labels (umbrella + specific) ready to insert. Hits
// is per-detector match counts so the UI can show "5/10 rows looked
// like emails" instead of an opaque tag.
type Result struct {
	Column  string
	Sampled int
	Tags    []string
	Hits    map[string]int // detector kind → match count
}

// ClassifySamples runs every detector against the supplied non-null
// values and returns the winning Result. Empty input yields a zero
// Result (no tags).
func ClassifySamples(column string, samples []string) Result {
	return ClassifySamplesWith(column, samples, nil)
}

// ClassifySamplesWith is the filtered variant of ClassifySamples. When
// enabled is non-nil only detectors whose Kind is present in the map
// run; other detectors are skipped entirely (no Hits entry, no tag).
// Passing nil is equivalent to ClassifySamples.
func ClassifySamplesWith(column string, samples []string, enabled map[string]bool) Result {
	r := Result{Column: column, Sampled: len(samples), Hits: map[string]int{}}
	if len(samples) == 0 {
		return r
	}
	tagSet := map[string]struct{}{}
	for _, det := range All() {
		if enabled != nil && !enabled[det.Kind] {
			continue
		}
		hits := 0
		for _, v := range samples {
			v = trim(v)
			if v == "" {
				continue
			}
			if det.Match(v) {
				hits++
			}
		}
		r.Hits[det.Kind] = hits
		fraction := float64(hits) / float64(len(samples))
		if hits > 0 && fraction >= det.Threshold {
			for _, t := range Tags(det.Kind) {
				tagSet[t] = struct{}{}
			}
		}
	}
	for t := range tagSet {
		r.Tags = append(r.Tags, t)
	}
	return r
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

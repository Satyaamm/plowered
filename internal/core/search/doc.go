// Package search wraps the search backend (an embedded full-text index by
// default, with an optional external search service swappable behind the
// same interface). It listens on the event bus for asset mutations and keeps
// the index in sync.
package search

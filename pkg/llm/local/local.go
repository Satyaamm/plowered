// Package local implements a zero-dependency llm.Provider whose embeddings
// are deterministic bag-of-words hashes. It exists so Plowered demos and
// tests have a working semantic-search pipeline without paying for or
// configuring a real embedding API. The vectors are not as expressive as
// real models — synonyms don't cluster — but they're stable, fast, and
// match exact-keyword queries perfectly, which is the most common
// search pattern users actually try.
//
// Switch to a real provider for production: same Provider interface,
// same EmbedResponse shape, callers don't change.
package local

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"strings"
	"unicode"

	"github.com/Satyaamm/plowered/pkg/llm"
)

const (
	// Name is the identifier this provider returns from Provider.Name().
	// asset_embeddings rows store this in the `model` column so a future
	// reindex run with a different model is a clean swap.
	Name = "local-bow-256"
	// Dim is the vector dimension. 256 is enough headroom that hash
	// collisions stay rare across catalogs in the low-thousands without
	// blowing up the row size.
	Dim = 256
)

// Provider is a deterministic, offline llm.Provider. Embed() hashes
// tokens into a fixed-dim vector, normalises L2; Generate() refuses
// because we don't want to pretend to be a chat model.
type Provider struct{}

// New returns a ready provider. Stateless; one shared instance is fine.
func New() *Provider { return &Provider{} }

func (Provider) Name() string { return Name }

func (Provider) Generate(_ context.Context, _ llm.GenerateRequest) (llm.GenerateResponse, error) {
	return llm.GenerateResponse{}, errors.New("local provider does not support generation; configure a real LLM provider")
}

// Embed produces one vector per input text. The algorithm is
// straightforward bag-of-words → hashed buckets → L2-normalize, with a
// 2-gram pass added so multi-word phrases get distinguishing signal.
func (Provider) Embed(_ context.Context, req llm.EmbedRequest) (llm.EmbedResponse, error) {
	out := make([][]float32, len(req.Texts))
	tokens := 0
	for i, text := range req.Texts {
		v, n := embedText(text)
		out[i] = v
		tokens += n
	}
	return llm.EmbedResponse{Vectors: out, Model: Name, Tokens: tokens}, nil
}

func embedText(text string) ([]float32, int) {
	v := make([]float32, Dim)
	toks := tokenize(text)
	if len(toks) == 0 {
		return v, 0
	}
	for _, t := range toks {
		v[hashBucket(t)] += 1
	}
	// 2-grams: small weight; helps distinguish "user email" from "email user"
	// only when both tokens appear in the same window.
	for i := 0; i+1 < len(toks); i++ {
		bg := toks[i] + " " + toks[i+1]
		v[hashBucket(bg)] += 0.5
	}
	normalize(v)
	return v, len(toks)
}

// tokenize lowercases and strips non-letter/digit runes; splits on
// whitespace + dots/underscores so qualified-name tokens become individual
// terms ("loopback.public.users.email" → loopback / public / users / email).
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	low := strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r):
			return unicode.ToLower(r)
		case unicode.IsDigit(r):
			return r
		default:
			return ' '
		}
	}, s)
	return strings.Fields(low)
}

func hashBucket(s string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32() % uint32(Dim))
}

func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1.0 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

// Cosine returns a · b for two L2-normalised vectors of equal length.
// Returns 0 on dimension mismatch so callers get a deterministic
// "no match" rather than an error.
func Cosine(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

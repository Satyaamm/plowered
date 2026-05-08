// Package mock is an in-process LLM provider for tests. It returns
// programmable responses without making any network call.
package mock

import (
	"context"
	"errors"
	"sync"

	"github.com/Satyaamm/plowered/pkg/llm"
)

// Provider is a controllable Provider implementation for unit tests.
//
//	p := mock.New()
//	p.Queue(mock.Reply{Content: "hello", InputTokens: 10, OutputTokens: 2})
//	resp, _ := p.Generate(ctx, req)
type Provider struct {
	mu      sync.Mutex
	queue   []Reply
	calls   []llm.GenerateRequest
	embeds  [][]float32
	failNext error
}

type Reply struct {
	Content      string
	InputTokens  int
	OutputTokens int
	StopReason   string
}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string { return "mock" }

// Queue appends a reply that will be returned by the next Generate call.
func (p *Provider) Queue(r Reply) {
	p.mu.Lock()
	p.queue = append(p.queue, r)
	p.mu.Unlock()
}

// FailNext makes the next Generate call return err.
func (p *Provider) FailNext(err error) {
	p.mu.Lock()
	p.failNext = err
	p.mu.Unlock()
}

// Calls returns a copy of all GenerateRequests the provider has seen.
func (p *Provider) Calls() []llm.GenerateRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]llm.GenerateRequest, len(p.calls))
	copy(out, p.calls)
	return out
}

func (p *Provider) Generate(_ context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, req)
	if p.failNext != nil {
		err := p.failNext
		p.failNext = nil
		return llm.GenerateResponse{}, err
	}
	if len(p.queue) == 0 {
		return llm.GenerateResponse{}, errors.New("mock: no queued reply")
	}
	r := p.queue[0]
	p.queue = p.queue[1:]
	return llm.GenerateResponse{
		Content:      r.Content,
		Model:        req.Model,
		InputTokens:  r.InputTokens,
		OutputTokens: r.OutputTokens,
		StopReason:   defaultStr(r.StopReason, "end_turn"),
	}, nil
}

// SetEmbed configures the next embedding response (one vector per input).
func (p *Provider) SetEmbed(vec []float32) {
	p.mu.Lock()
	p.embeds = append(p.embeds, vec)
	p.mu.Unlock()
}

func (p *Provider) Embed(_ context.Context, req llm.EmbedRequest) (llm.EmbedResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.embeds) == 0 {
		return llm.EmbedResponse{}, llm.ErrEmbedUnsupported
	}
	vec := p.embeds[0]
	p.embeds = p.embeds[1:]
	out := make([][]float32, len(req.Texts))
	for i := range out {
		out[i] = vec
	}
	return llm.EmbedResponse{Vectors: out, Model: req.Model}, nil
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

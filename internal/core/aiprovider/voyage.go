package aiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Satyaamm/plowered/pkg/llm"
)

// voyageProvider wraps Voyage AI's embedding API. Voyage is embed-only
// today (no chat); Generate returns llm.ErrChatUnsupported so the
// resolver routes chat to a different config.
//
//	POST /v1/embeddings       — embeddings
//	GET  /v1/models           — credential probe (returns 200 on valid key)
//
// Reference: https://docs.voyageai.com/reference

type voyageProvider struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func (p *voyageProvider) Name() string { return "voyage:" + p.model }

func (p *voyageProvider) Generate(_ context.Context, _ llm.GenerateRequest) (llm.GenerateResponse, error) {
	return llm.GenerateResponse{}, llm.ErrChatUnsupported
}

func (p *voyageProvider) Embed(ctx context.Context, req llm.EmbedRequest) (llm.EmbedResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	body := map[string]any{
		"model":      model,
		"input":      req.Texts,
		"input_type": "document",
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.baseURL, "/")+"/v1/embeddings", bytes.NewReader(raw))
	if err != nil {
		return llm.EmbedResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return llm.EmbedResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return llm.EmbedResponse{}, errFromHTTP(resp)
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Model string `json:"model"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return llm.EmbedResponse{}, err
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return llm.EmbedResponse{Vectors: vecs, Model: out.Model, Tokens: out.Usage.TotalTokens}, nil
}

func testVoyage(ctx context.Context, baseURL, apiKey string) error {
	// Voyage doesn't expose a public list-models endpoint; the cheapest
	// probe is a 1-token embedding on the smallest model. Burns a tiny
	// quota slot, but the test should only run on Save / Test click.
	body := map[string]any{
		"model":      "voyage-3-lite",
		"input":      []string{"ping"},
		"input_type": "document",
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/embeddings", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := defaultHTTP().Do(req)
	if err != nil {
		return fmt.Errorf("voyage: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

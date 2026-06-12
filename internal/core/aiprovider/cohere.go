package aiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Satyaamm/plowered/pkg/llm"
)

// cohereProvider wraps the Cohere v2 chat + v1 embed surface.
//
//	POST /v2/chat             — chat completion (preferred)
//	POST /v1/embed            — embeddings
//	GET  /v1/models           — credential probe
//
// Reference: https://docs.cohere.com/reference

type cohereProvider struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func (p *cohereProvider) Name() string { return "cohere:" + p.model }

func (p *cohereProvider) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	msgs := make([]map[string]string, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	body := map[string]any{"model": model, "messages": msgs}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	raw, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.baseURL, "/")+"/v2/chat", bytes.NewReader(raw))
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return llm.GenerateResponse{}, errFromHTTP(resp)
	}
	var out struct {
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
		Usage        struct {
			Tokens struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return llm.GenerateResponse{}, err
	}
	if len(out.Message.Content) == 0 {
		return llm.GenerateResponse{}, errors.New("cohere: empty content")
	}
	var text strings.Builder
	for _, c := range out.Message.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return llm.GenerateResponse{
		Content:      text.String(),
		Model:        model,
		InputTokens:  out.Usage.Tokens.InputTokens,
		OutputTokens: out.Usage.Tokens.OutputTokens,
		StopReason:   out.FinishReason,
	}, nil
}

func (p *cohereProvider) Embed(ctx context.Context, req llm.EmbedRequest) (llm.EmbedResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	body := map[string]any{
		"model":           model,
		"texts":           req.Texts,
		"input_type":      "search_document",
		"embedding_types": []string{"float"},
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.baseURL, "/")+"/v1/embed", bytes.NewReader(raw))
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
		Embeddings struct {
			Float [][]float32 `json:"float"`
		} `json:"embeddings"`
		Meta struct {
			BilledUnits struct {
				InputTokens int `json:"input_tokens"`
			} `json:"billed_units"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return llm.EmbedResponse{}, err
	}
	return llm.EmbedResponse{
		Vectors: out.Embeddings.Float,
		Model:   model,
		Tokens:  out.Meta.BilledUnits.InputTokens,
	}, nil
}

func testCohere(ctx context.Context, baseURL, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := defaultHTTP().Do(req)
	if err != nil {
		return fmt.Errorf("cohere: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

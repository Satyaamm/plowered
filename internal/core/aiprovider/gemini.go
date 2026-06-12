package aiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Satyaamm/plowered/pkg/llm"
)

// geminiProvider wraps Google's "Generative Language" REST API
// (https://generativelanguage.googleapis.com). The auth model is the
// API-key one — service-account / OAuth2 Vertex AI auth lives in the
// vertex provider instead. Wire shapes are documented at:
//
//	POST /v1beta/models/{model}:generateContent
//	POST /v1beta/models/{model}:embedContent
//	GET  /v1beta/models           (the cheap credential probe)
//
// Reference: https://ai.google.dev/api/rest

type geminiProvider struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func (p *geminiProvider) Name() string { return "gemini:" + p.model }

func (p *geminiProvider) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	contents := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := string(m.Role)
		// Gemini uses "model" for assistant turns; system turns are
		// promoted to a separate top-level systemInstruction.
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": m.Content}},
		})
	}

	body := map[string]any{"contents": contents}
	if req.System != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": req.System}},
		}
	}
	gen := map[string]any{}
	if req.MaxTokens > 0 {
		gen["maxOutputTokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		gen["temperature"] = req.Temperature
	}
	if len(gen) > 0 {
		body["generationConfig"] = gen
	}
	raw, _ := json.Marshal(body)

	u := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		strings.TrimRight(p.baseURL, "/"),
		url.PathEscape(model),
		url.QueryEscape(p.apiKey),
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return llm.GenerateResponse{}, errFromHTTP(resp)
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return llm.GenerateResponse{}, err
	}
	if len(out.Candidates) == 0 {
		return llm.GenerateResponse{}, errors.New("gemini: empty candidates")
	}
	var text strings.Builder
	for _, p := range out.Candidates[0].Content.Parts {
		text.WriteString(p.Text)
	}
	return llm.GenerateResponse{
		Content:      text.String(),
		Model:        model,
		InputTokens:  out.UsageMetadata.PromptTokenCount,
		OutputTokens: out.UsageMetadata.CandidatesTokenCount,
		StopReason:   out.Candidates[0].FinishReason,
	}, nil
}

func (p *geminiProvider) Embed(ctx context.Context, req llm.EmbedRequest) (llm.EmbedResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	// Gemini's embedContent takes one input per request; batch by hand.
	out := llm.EmbedResponse{
		Vectors: make([][]float32, 0, len(req.Texts)),
		Model:   model,
	}
	for _, txt := range req.Texts {
		body := map[string]any{
			"content": map[string]any{
				"parts": []map[string]string{{"text": txt}},
			},
		}
		raw, _ := json.Marshal(body)
		u := fmt.Sprintf("%s/v1beta/models/%s:embedContent?key=%s",
			strings.TrimRight(p.baseURL, "/"),
			url.PathEscape(model),
			url.QueryEscape(p.apiKey),
		)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
		if err != nil {
			return llm.EmbedResponse{}, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := p.client.Do(httpReq)
		if err != nil {
			return llm.EmbedResponse{}, err
		}
		var decoded struct {
			Embedding struct {
				Values []float32 `json:"values"`
			} `json:"embedding"`
		}
		if resp.StatusCode >= 400 {
			err = errFromHTTP(resp)
			resp.Body.Close()
			return llm.EmbedResponse{}, err
		}
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			resp.Body.Close()
			return llm.EmbedResponse{}, err
		}
		resp.Body.Close()
		out.Vectors = append(out.Vectors, decoded.Embedding.Values)
	}
	return out, nil
}

func testGemini(ctx context.Context, baseURL, apiKey string) error {
	u := fmt.Sprintf("%s/v1beta/models?key=%s",
		strings.TrimRight(baseURL, "/"),
		url.QueryEscape(apiKey),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := defaultHTTP().Do(req)
	if err != nil {
		return fmt.Errorf("gemini: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

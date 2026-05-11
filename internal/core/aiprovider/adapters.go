package aiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/pkg/llm"
)

// Build constructs an llm.Provider from a (Config, apiKey) pair. The
// secrets.Vault sits one layer above — the caller resolves cfg.SecretURN
// into bytes and hands them here.
//
// This is the only place provider Kind translates into a concrete HTTP
// client; the rest of the codebase touches only the llm.Provider
// interface so swapping providers is a config change.
func Build(cfg *Config, apiKey []byte) (llm.Provider, error) {
	if cfg == nil {
		return nil, errors.New("aiprovider: nil config")
	}
	// Use the SSRF-aware client for every outbound request so a custom
	// base_url can't reach 127.0.0.1, the cloud metadata endpoint, or
	// any private network. The check is bypassed when the operator sets
	// PLOWERED_ALLOW_PRIVATE_AI_HOSTS=1 (dev convenience for Ollama).
	client := SafeHTTPClient(60 * time.Second)
	switch cfg.Kind {
	case KindAnthropic:
		return &anthropicProvider{baseURL: pick(cfg.BaseURL, "https://api.anthropic.com"), model: cfg.Model, apiKey: string(apiKey), client: client}, nil
	case KindOpenAI:
		return &openaiProvider{baseURL: pick(cfg.BaseURL, "https://api.openai.com"), model: cfg.Model, apiKey: string(apiKey), name: "openai:" + cfg.Model, client: client}, nil
	case KindDeepSeek:
		return &openaiProvider{baseURL: pick(cfg.BaseURL, "https://api.deepseek.com"), model: cfg.Model, apiKey: string(apiKey), name: "deepseek:" + cfg.Model, client: client}, nil
	case KindCustom:
		if cfg.BaseURL == "" {
			return nil, errors.New("aiprovider: custom provider requires base_url")
		}
		return &openaiProvider{baseURL: cfg.BaseURL, model: cfg.Model, apiKey: string(apiKey), name: "custom:" + cfg.Model, client: client}, nil
	default:
		return nil, fmt.Errorf("aiprovider: unknown kind %q", cfg.Kind)
	}
}

// Test runs a cheap credential probe against the provider so the
// settings UI can show green/red on save. Implementation strategy per
// kind:
//
//	Anthropic: GET /v1/models (200 ⇒ key valid)
//	OpenAI:    GET /v1/models
//	DeepSeek:  GET /v1/models (OpenAI-compatible)
//	Custom:    GET {base}/v1/models
//
// We deliberately don't consume tokens on test — listing models burns
// at most a request quota slot, not a generation bill. Timeout is held
// at 10s so a misconfigured endpoint fails fast.
func Test(ctx context.Context, cfg *Config, apiKey []byte) error {
	if cfg == nil {
		return errors.New("aiprovider: nil config")
	}
	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	switch cfg.Kind {
	case KindAnthropic:
		return testAnthropic(tctx, pick(cfg.BaseURL, "https://api.anthropic.com"), string(apiKey))
	case KindOpenAI:
		return testOpenAI(tctx, pick(cfg.BaseURL, "https://api.openai.com"), string(apiKey))
	case KindDeepSeek:
		return testOpenAI(tctx, pick(cfg.BaseURL, "https://api.deepseek.com"), string(apiKey))
	case KindCustom:
		if cfg.BaseURL == "" {
			return errors.New("aiprovider: custom provider requires base_url")
		}
		return testOpenAI(tctx, cfg.BaseURL, string(apiKey))
	default:
		return fmt.Errorf("aiprovider: unknown kind %q", cfg.Kind)
	}
}

func pick(a, fallback string) string {
	if strings.TrimSpace(a) == "" {
		return fallback
	}
	return strings.TrimRight(a, "/")
}

// defaultHTTP is the SSRF-aware client used by the cheap credential
// probe (testAnthropic / testOpenAI). 10s timeout is enough for a
// single GET /v1/models against any real upstream and short enough to
// fail fast on a black-holed config.
func defaultHTTP() *http.Client {
	return SafeHTTPClient(10 * time.Second)
}

// ----- Anthropic -----

type anthropicProvider struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func (p *anthropicProvider) Name() string { return "anthropic:" + p.model }

func (p *anthropicProvider) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	msgs := make([]map[string]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = 1024
	}
	body := map[string]any{
		"model":      model,
		"max_tokens": maxTok,
		"messages":   msgs,
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	raw, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return llm.GenerateResponse{}, errFromHTTP(resp)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return llm.GenerateResponse{}, err
	}
	var text strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return llm.GenerateResponse{
		Content:      text.String(),
		Model:        out.Model,
		InputTokens:  out.Usage.InputTokens,
		OutputTokens: out.Usage.OutputTokens,
		StopReason:   out.StopReason,
	}, nil
}

func (p *anthropicProvider) Embed(ctx context.Context, req llm.EmbedRequest) (llm.EmbedResponse, error) {
	// Anthropic has no first-party embedding endpoint as of writing —
	// surface that explicitly so the caller can route embeddings to a
	// different config.
	return llm.EmbedResponse{}, llm.ErrEmbedUnsupported
}

func testAnthropic(ctx context.Context, baseURL, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := defaultHTTP().Do(req)
	if err != nil {
		return fmt.Errorf("anthropic: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

// ----- OpenAI-compatible (OpenAI / DeepSeek / Custom) -----

type openaiProvider struct {
	baseURL string
	model   string
	apiKey  string
	name    string
	client  *http.Client
}

func (p *openaiProvider) Name() string { return p.name }

func (p *openaiProvider) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
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
	body := map[string]any{
		"model":    model,
		"messages": msgs,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(raw))
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
		Choices []struct {
			Message      struct{ Content string } `json:"message"`
			FinishReason string                   `json:"finish_reason"`
		} `json:"choices"`
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return llm.GenerateResponse{}, err
	}
	if len(out.Choices) == 0 {
		return llm.GenerateResponse{}, errors.New("openai: empty choices")
	}
	return llm.GenerateResponse{
		Content:      out.Choices[0].Message.Content,
		Model:        out.Model,
		InputTokens:  out.Usage.PromptTokens,
		OutputTokens: out.Usage.CompletionTokens,
		StopReason:   out.Choices[0].FinishReason,
	}, nil
}

func (p *openaiProvider) Embed(ctx context.Context, req llm.EmbedRequest) (llm.EmbedResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	body := map[string]any{"model": model, "input": req.Texts}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/embeddings", bytes.NewReader(raw))
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

func testOpenAI(ctx context.Context, baseURL, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := defaultHTTP().Do(req)
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

// errFromHTTP collapses an upstream non-2xx into a clean error with a
// truncated body so we don't dump 5kb of HTML into logs on a misrouted
// request.
func errFromHTTP(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	body := strings.TrimSpace(string(b))
	if body == "" {
		body = resp.Status
	}
	return fmt.Errorf("upstream %d: %s", resp.StatusCode, body)
}

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

// azureOAIProvider wraps Azure OpenAI Service.
//
// Azure OpenAI differs from OpenAI in three ways: the request URL is
// {base}/openai/deployments/{deployment}/chat/completions?api-version={v},
// the auth header is "api-key" not "Authorization: Bearer", and the
// model is the *deployment name* — the underlying OpenAI model id lives
// on the Azure side. We keep cfg.Model as the deployment name for
// consistency with the rest of the platform but expose it as
// "Deployment" in the UI for clarity.
//
// Reference: https://learn.microsoft.com/azure/ai-services/openai/reference

type azureOAIProvider struct {
	baseURL    string // https://<resource>.openai.azure.com
	deployment string
	apiVersion string
	model      string // kept for telemetry / log lines only
	apiKey     string
	client     *http.Client
}

func (p *azureOAIProvider) Name() string {
	return "azure-openai:" + p.deployment
}

func (p *azureOAIProvider) chatURL() string {
	return fmt.Sprintf(
		"%s/openai/deployments/%s/chat/completions?api-version=%s",
		p.baseURL,
		url.PathEscape(p.deployment),
		url.QueryEscape(p.apiVersion),
	)
}

func (p *azureOAIProvider) embedURL() string {
	return fmt.Sprintf(
		"%s/openai/deployments/%s/embeddings?api-version=%s",
		p.baseURL,
		url.PathEscape(p.deployment),
		url.QueryEscape(p.apiVersion),
	)
}

func (p *azureOAIProvider) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	msgs := make([]map[string]string, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	body := map[string]any{"messages": msgs}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	raw, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.chatURL(), bytes.NewReader(raw))
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("api-key", p.apiKey)

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
		return llm.GenerateResponse{}, errors.New("azure-openai: empty choices")
	}
	return llm.GenerateResponse{
		Content:      out.Choices[0].Message.Content,
		Model:        out.Model,
		InputTokens:  out.Usage.PromptTokens,
		OutputTokens: out.Usage.CompletionTokens,
		StopReason:   out.Choices[0].FinishReason,
	}, nil
}

func (p *azureOAIProvider) Embed(ctx context.Context, req llm.EmbedRequest) (llm.EmbedResponse, error) {
	body := map[string]any{"input": req.Texts}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.embedURL(), bytes.NewReader(raw))
	if err != nil {
		return llm.EmbedResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("api-key", p.apiKey)
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

// testAzureOAI probes the listed-deployments endpoint. The shape is
// {base}/openai/deployments?api-version={v}; a 200 confirms the key +
// resource URL are both valid even if the deployment name is wrong.
func testAzureOAI(ctx context.Context, cfg *Config, apiKey string) error {
	u := fmt.Sprintf(
		"%s/openai/deployments?api-version=%s",
		strings.TrimRight(cfg.BaseURL, "/"),
		url.QueryEscape(cfg.APIVersion),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("api-key", apiKey)
	resp, err := defaultHTTP().Do(req)
	if err != nil {
		return fmt.Errorf("azure-openai: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

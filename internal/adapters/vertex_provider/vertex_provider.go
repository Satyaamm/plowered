// Package vertex_provider wires GCP Vertex AI into the aiprovider seam.
// Register it from cmd/plowered/main.go via:
//
//	import _ "github.com/Satyaamm/plowered/internal/adapters/vertex_provider"
//
// We talk to Vertex over its REST API rather than pulling the full
// cloud.google.com/go/aiplatform SDK — the SDK adds ~12MB to the
// binary and we only need the predict + countTokens endpoints. The
// OAuth2 access token comes from cloud.google.com/go/auth, which
// transparently honours the Application Default Credentials chain
// (service-account JSON via GOOGLE_APPLICATION_CREDENTIALS, gcloud
// user creds, GKE workload identity).
//
// Operators who want explicit credentials paste a service-account JSON
// blob into the wizard; we store it in the vault under cfg.SecretURN
// and pass it through DetectDefault's CredentialsJSON option.
//
// Wire-format dispatch by model id prefix:
//
//	gemini-*        → publishers/google/models/{model}:generateContent
//	text-bison*     → publishers/google/models/{model}:predict (palm-2)
//	textembedding-* → publishers/google/models/{model}:predict (embed)
//	claude-*        → publishers/anthropic/models/{model}:rawPredict
//
// Reference: https://cloud.google.com/vertex-ai/docs/reference/rest
package vertex_provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"

	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/pkg/llm"
)

func init() {
	aiprovider.SetVertexDriver(driver{})
}

// vertexScope is the OAuth2 scope every Vertex predict + embed call
// uses. The compute scope alone is insufficient for predict.
const vertexScope = "https://www.googleapis.com/auth/cloud-platform"

type driver struct{}

func (driver) Build(cfg *aiprovider.Config, secret []byte) (llm.Provider, error) {
	if err := requireConfig(cfg); err != nil {
		return nil, err
	}
	creds, err := loadCredentials(secret)
	if err != nil {
		return nil, err
	}
	return &provider{
		cfg:   cfg,
		creds: creds,
		http:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (driver) Test(ctx context.Context, cfg *aiprovider.Config, secret []byte) error {
	if err := requireConfig(cfg); err != nil {
		return err
	}
	creds, err := loadCredentials(secret)
	if err != nil {
		return err
	}
	// Probe: a 1-token generate against the configured model. Vertex
	// has no list-models endpoint that's cheap enough to use here, and
	// :countTokens is gated by the same auth so it works as a probe
	// but doesn't validate the model id exists in the project.
	p := &provider{cfg: cfg, creds: creds, http: &http.Client{Timeout: 10 * time.Second}}
	_, err = p.Generate(ctx, llm.GenerateRequest{
		Messages:  []llm.Message{{Role: "user", Content: "ping"}},
		MaxTokens: 1,
	})
	return err
}

func requireConfig(cfg *aiprovider.Config) error {
	if cfg == nil {
		return errors.New("vertex: nil config")
	}
	if cfg.Project == "" {
		return errors.New("vertex: project required")
	}
	if cfg.Location == "" {
		return errors.New("vertex: location required (e.g. us-central1)")
	}
	if cfg.Model == "" {
		return errors.New("vertex: model required")
	}
	return nil
}

// loadCredentials returns the OAuth2 credentials. If `secret` carries a
// service-account JSON blob, it overrides the default chain; otherwise
// Application Default Credentials apply (workload identity on GKE,
// gcloud user creds, GOOGLE_APPLICATION_CREDENTIALS, etc.).
func loadCredentials(secret []byte) (*auth.Credentials, error) {
	opts := &credentials.DetectOptions{Scopes: []string{vertexScope}}
	if looksLikeServiceAccountJSON(secret) {
		opts.CredentialsJSON = secret
	}
	c, err := credentials.DetectDefault(opts)
	if err != nil {
		return nil, fmt.Errorf("vertex: detect credentials: %w", err)
	}
	return c, nil
}

func looksLikeServiceAccountJSON(b []byte) bool {
	s := strings.TrimSpace(string(b))
	return strings.HasPrefix(s, "{") &&
		strings.Contains(s, `"type"`) &&
		strings.Contains(s, `"service_account"`)
}

type provider struct {
	cfg   *aiprovider.Config
	creds *auth.Credentials
	http  *http.Client
}

func (p *provider) Name() string { return "vertex:" + p.cfg.Model }

func (p *provider) endpointFor(action string) string {
	publisher := publisherFor(p.cfg.Model)
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/%s/models/%s:%s",
		p.cfg.Location, p.cfg.Project, p.cfg.Location,
		publisher, p.cfg.Model, action,
	)
}

// publisherFor picks the right "publishers/X" segment for the model id.
// Vertex routes Anthropic models under publishers/anthropic, Google
// models under publishers/google, Meta under publishers/meta, etc.
func publisherFor(model string) string {
	switch {
	case strings.HasPrefix(model, "claude"):
		return "anthropic"
	case strings.HasPrefix(model, "llama"):
		return "meta"
	case strings.HasPrefix(model, "mistral"), strings.HasPrefix(model, "mixtral"):
		return "mistralai"
	}
	return "google"
}

func (p *provider) authHeader(ctx context.Context) (string, error) {
	tok, err := p.creds.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("vertex: token: %w", err)
	}
	return "Bearer " + tok.Value, nil
}

func (p *provider) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}
	switch {
	case strings.HasPrefix(model, "gemini"):
		return p.generateGemini(ctx, req)
	case strings.HasPrefix(model, "claude"):
		return p.generateClaude(ctx, req)
	}
	return llm.GenerateResponse{}, fmt.Errorf("vertex: chat unsupported for model %q (gemini-* / claude-* only)", model)
}

func (p *provider) generateGemini(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	contents := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := string(m.Role)
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

	resp, err := p.post(ctx, p.endpointFor("generateContent"), raw)
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
		return llm.GenerateResponse{}, errors.New("vertex: empty candidates")
	}
	var text strings.Builder
	for _, part := range out.Candidates[0].Content.Parts {
		text.WriteString(part.Text)
	}
	return llm.GenerateResponse{
		Content:      text.String(),
		Model:        p.cfg.Model,
		InputTokens:  out.UsageMetadata.PromptTokenCount,
		OutputTokens: out.UsageMetadata.CandidatesTokenCount,
		StopReason:   out.Candidates[0].FinishReason,
	}, nil
}

// generateClaude hits the rawPredict endpoint that Vertex exposes for
// the Anthropic publisher — the request body matches Anthropic's
// Messages API except for an extra anthropic_version field.
func (p *provider) generateClaude(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	msgs := make([]map[string]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]string{
			"role":    string(m.Role),
			"content": m.Content,
		})
	}
	body := map[string]any{
		"anthropic_version": "vertex-2023-10-16",
		"messages":          msgs,
		"max_tokens":        maxTokensOrDefault(req, 1024),
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	raw, _ := json.Marshal(body)

	resp, err := p.post(ctx, p.endpointFor("rawPredict"), raw)
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
		Model:        p.cfg.Model,
		InputTokens:  out.Usage.InputTokens,
		OutputTokens: out.Usage.OutputTokens,
		StopReason:   out.StopReason,
	}, nil
}

func (p *provider) Embed(ctx context.Context, req llm.EmbedRequest) (llm.EmbedResponse, error) {
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}
	if !strings.HasPrefix(model, "textembedding") && !strings.HasPrefix(model, "text-embedding") {
		return llm.EmbedResponse{}, fmt.Errorf("vertex: model %q is not an embedding model", model)
	}
	instances := make([]map[string]any, 0, len(req.Texts))
	for _, t := range req.Texts {
		instances = append(instances, map[string]any{"content": t})
	}
	body, _ := json.Marshal(map[string]any{"instances": instances})

	resp, err := p.post(ctx, p.endpointFor("predict"), body)
	if err != nil {
		return llm.EmbedResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return llm.EmbedResponse{}, errFromHTTP(resp)
	}
	var out struct {
		Predictions []struct {
			Embeddings struct {
				Values     []float32 `json:"values"`
				Statistics struct {
					TokenCount int `json:"token_count"`
				} `json:"statistics"`
			} `json:"embeddings"`
		} `json:"predictions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return llm.EmbedResponse{}, err
	}
	vecs := make([][]float32, 0, len(out.Predictions))
	tokens := 0
	for _, p := range out.Predictions {
		vecs = append(vecs, p.Embeddings.Values)
		tokens += p.Embeddings.Statistics.TokenCount
	}
	return llm.EmbedResponse{Vectors: vecs, Model: model, Tokens: tokens}, nil
}

func (p *provider) post(ctx context.Context, url string, body []byte) (*http.Response, error) {
	authHdr, err := p.authHeader(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHdr)
	return p.http.Do(req)
}

func errFromHTTP(resp *http.Response) error {
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	body := strings.TrimSpace(string(buf[:n]))
	if body == "" {
		body = resp.Status
	}
	return fmt.Errorf("vertex upstream %d: %s", resp.StatusCode, body)
}

func maxTokensOrDefault(req llm.GenerateRequest, def int) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return def
}

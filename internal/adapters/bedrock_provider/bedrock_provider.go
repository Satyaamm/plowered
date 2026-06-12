// Package bedrock_provider wires AWS Bedrock Runtime into the
// aiprovider seam. Register it from cmd/plowered/main.go via:
//
//	import _ "github.com/Satyaamm/plowered/internal/adapters/bedrock_provider"
//
// Credential resolution follows the AWS SDK default chain:
// environment vars → ~/.aws/credentials → EC2 instance profile → IRSA
// on EKS. The aiprovider.Config.SecretURN may carry a JSON blob
// {"access_key_id":"...","secret_access_key":"...","session_token":"..."}
// for explicit static credentials — the wizard offers this when the
// operator doesn't want to rely on the deployment's role.
//
// Wire-format dispatch by model id (Bedrock multiplexes many model
// families behind one Invoke endpoint):
//
//	anthropic.*          → Anthropic Messages API shape
//	amazon.titan-*       → Titan Generate API
//	meta.llama*          → Llama Chat API
//	mistral.*            → Mistral instruct API
//	cohere.*             → Cohere Generate API
package bedrock_provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/pkg/llm"
)

func init() {
	aiprovider.SetBedrockDriver(driver{})
}

type driver struct{}

func (driver) Build(cfg *aiprovider.Config, secret []byte) (llm.Provider, error) {
	if cfg == nil {
		return nil, errors.New("bedrock: nil config")
	}
	if cfg.Region == "" {
		return nil, errors.New("bedrock: region required")
	}
	client, err := newClient(context.Background(), cfg, secret)
	if err != nil {
		return nil, err
	}
	return &provider{client: client, model: cfg.Model}, nil
}

func (driver) Test(ctx context.Context, cfg *aiprovider.Config, secret []byte) error {
	if cfg == nil {
		return errors.New("bedrock: nil config")
	}
	if cfg.Region == "" {
		return errors.New("bedrock: region required")
	}
	client, err := newClient(ctx, cfg, secret)
	if err != nil {
		return err
	}
	// Cheapest live probe: a single-token Invoke against the configured
	// model. There is no list-models endpoint on Bedrock Runtime; the
	// control-plane (bedrock, not bedrockruntime) has one but pulling
	// it in just for a probe doubles the SDK weight.
	body, err := encodeRequest(cfg.Model, llm.GenerateRequest{
		Messages:  []llm.Message{{Role: "user", Content: "ping"}},
		MaxTokens: 1,
	})
	if err != nil {
		return err
	}
	_, err = client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(cfg.Model),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        body,
	})
	return err
}

// staticCreds is the JSON we accept on cfg.SecretURN when the operator
// pastes explicit creds instead of relying on the SDK's default chain.
type staticCreds struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token,omitempty"`
}

func newClient(ctx context.Context, cfg *aiprovider.Config, secret []byte) (*bedrockruntime.Client, error) {
	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}

	// If the operator pasted explicit creds we honour them; otherwise
	// the SDK falls through env / ~/.aws / instance profile / IRSA.
	if len(secret) > 0 {
		var sc staticCreds
		if err := json.Unmarshal(secret, &sc); err == nil && sc.AccessKeyID != "" {
			loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(
					sc.AccessKeyID, sc.SecretAccessKey, sc.SessionToken,
				),
			))
		}
	}

	awscfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("bedrock: load aws config: %w", err)
	}
	return bedrockruntime.NewFromConfig(awscfg), nil
}

type provider struct {
	client *bedrockruntime.Client
	model  string
}

func (p *provider) Name() string { return "bedrock:" + p.model }

func (p *provider) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	body, err := encodeRequest(model, req)
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	out, err := p.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(model),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	return decodeResponse(model, out.Body)
}

func (p *provider) Embed(ctx context.Context, req llm.EmbedRequest) (llm.EmbedResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	// Bedrock embedding models all take one input per Invoke; batch by
	// hand. Titan v2 + Cohere embed-english-v3 both follow this shape.
	out := llm.EmbedResponse{
		Vectors: make([][]float32, 0, len(req.Texts)),
		Model:   model,
	}
	for _, txt := range req.Texts {
		var body []byte
		switch {
		case strings.HasPrefix(model, "amazon.titan-embed"):
			body, _ = json.Marshal(map[string]any{"inputText": txt})
		case strings.HasPrefix(model, "cohere.embed"):
			body, _ = json.Marshal(map[string]any{
				"texts":      []string{txt},
				"input_type": "search_document",
			})
		default:
			return llm.EmbedResponse{}, fmt.Errorf("bedrock: unknown embed model family %q", model)
		}
		resp, err := p.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String(model),
			ContentType: aws.String("application/json"),
			Accept:      aws.String("application/json"),
			Body:        body,
		})
		if err != nil {
			return llm.EmbedResponse{}, err
		}
		vec, err := decodeEmbedding(model, resp.Body)
		if err != nil {
			return llm.EmbedResponse{}, err
		}
		out.Vectors = append(out.Vectors, vec)
	}
	return out, nil
}

// encodeRequest produces the model-family-specific wire JSON for an
// Invoke. We multiplex by model-id prefix; each family is documented at
// https://docs.aws.amazon.com/bedrock/latest/userguide/model-parameters.html.
func encodeRequest(model string, req llm.GenerateRequest) ([]byte, error) {
	switch {
	case strings.HasPrefix(model, "anthropic."):
		msgs := make([]map[string]string, 0, len(req.Messages))
		for _, m := range req.Messages {
			msgs = append(msgs, map[string]string{
				"role":    string(m.Role),
				"content": m.Content,
			})
		}
		body := map[string]any{
			"anthropic_version": "bedrock-2023-05-31",
			"messages":          msgs,
			"max_tokens":        maxTokensOrDefault(req, 1024),
		}
		if req.System != "" {
			body["system"] = req.System
		}
		if req.Temperature > 0 {
			body["temperature"] = req.Temperature
		}
		return json.Marshal(body)
	case strings.HasPrefix(model, "amazon.titan-text"):
		var sb strings.Builder
		if req.System != "" {
			sb.WriteString(req.System)
			sb.WriteString("\n\n")
		}
		for _, m := range req.Messages {
			sb.WriteString(string(m.Role))
			sb.WriteString(": ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
		return json.Marshal(map[string]any{
			"inputText": sb.String(),
			"textGenerationConfig": map[string]any{
				"maxTokenCount": maxTokensOrDefault(req, 1024),
				"temperature":   req.Temperature,
			},
		})
	case strings.HasPrefix(model, "meta.llama"):
		var sb strings.Builder
		if req.System != "" {
			sb.WriteString("<<SYS>>\n")
			sb.WriteString(req.System)
			sb.WriteString("\n<</SYS>>\n\n")
		}
		for _, m := range req.Messages {
			sb.WriteString("[INST] ")
			sb.WriteString(m.Content)
			sb.WriteString(" [/INST]\n")
		}
		return json.Marshal(map[string]any{
			"prompt":      sb.String(),
			"max_gen_len": maxTokensOrDefault(req, 1024),
			"temperature": req.Temperature,
		})
	case strings.HasPrefix(model, "mistral."):
		msgs := make([]map[string]string, 0, len(req.Messages)+1)
		if req.System != "" {
			msgs = append(msgs, map[string]string{"role": "system", "content": req.System})
		}
		for _, m := range req.Messages {
			msgs = append(msgs, map[string]string{"role": string(m.Role), "content": m.Content})
		}
		return json.Marshal(map[string]any{
			"messages":    msgs,
			"max_tokens":  maxTokensOrDefault(req, 1024),
			"temperature": req.Temperature,
		})
	case strings.HasPrefix(model, "cohere."):
		var sb strings.Builder
		for _, m := range req.Messages {
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
		return json.Marshal(map[string]any{
			"prompt":      sb.String(),
			"max_tokens":  maxTokensOrDefault(req, 1024),
			"temperature": req.Temperature,
		})
	}
	return nil, fmt.Errorf("bedrock: unknown model family %q (no encoder)", model)
}

func decodeResponse(model string, body []byte) (llm.GenerateResponse, error) {
	switch {
	case strings.HasPrefix(model, "anthropic."):
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
		if err := json.Unmarshal(body, &out); err != nil {
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
			Model:        model,
			InputTokens:  out.Usage.InputTokens,
			OutputTokens: out.Usage.OutputTokens,
			StopReason:   out.StopReason,
		}, nil
	case strings.HasPrefix(model, "amazon.titan-text"):
		var out struct {
			Results []struct {
				OutputText       string `json:"outputText"`
				CompletionReason string `json:"completionReason"`
				TokenCount       int    `json:"tokenCount"`
			} `json:"results"`
			InputTextTokenCount int `json:"inputTextTokenCount"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return llm.GenerateResponse{}, err
		}
		if len(out.Results) == 0 {
			return llm.GenerateResponse{}, errors.New("bedrock: empty titan response")
		}
		return llm.GenerateResponse{
			Content:      out.Results[0].OutputText,
			Model:        model,
			InputTokens:  out.InputTextTokenCount,
			OutputTokens: out.Results[0].TokenCount,
			StopReason:   out.Results[0].CompletionReason,
		}, nil
	case strings.HasPrefix(model, "meta.llama"):
		var out struct {
			Generation       string `json:"generation"`
			PromptTokenCount int    `json:"prompt_token_count"`
			GenerationTokens int    `json:"generation_token_count"`
			StopReason       string `json:"stop_reason"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return llm.GenerateResponse{}, err
		}
		return llm.GenerateResponse{
			Content:      out.Generation,
			Model:        model,
			InputTokens:  out.PromptTokenCount,
			OutputTokens: out.GenerationTokens,
			StopReason:   out.StopReason,
		}, nil
	case strings.HasPrefix(model, "mistral."):
		var out struct {
			Choices []struct {
				Message      struct{ Content string } `json:"message"`
				StopReason   string                   `json:"stop_reason"`
				FinishReason string                   `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return llm.GenerateResponse{}, err
		}
		if len(out.Choices) == 0 {
			return llm.GenerateResponse{}, errors.New("bedrock: empty mistral response")
		}
		stop := out.Choices[0].FinishReason
		if stop == "" {
			stop = out.Choices[0].StopReason
		}
		return llm.GenerateResponse{
			Content:    out.Choices[0].Message.Content,
			Model:      model,
			StopReason: stop,
		}, nil
	case strings.HasPrefix(model, "cohere."):
		var out struct {
			Generations []struct {
				Text         string `json:"text"`
				FinishReason string `json:"finish_reason"`
			} `json:"generations"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return llm.GenerateResponse{}, err
		}
		if len(out.Generations) == 0 {
			return llm.GenerateResponse{}, errors.New("bedrock: empty cohere response")
		}
		return llm.GenerateResponse{
			Content:    out.Generations[0].Text,
			Model:      model,
			StopReason: out.Generations[0].FinishReason,
		}, nil
	}
	return llm.GenerateResponse{}, fmt.Errorf("bedrock: unknown model family %q (no decoder)", model)
}

func decodeEmbedding(model string, body []byte) ([]float32, error) {
	switch {
	case strings.HasPrefix(model, "amazon.titan-embed"):
		var out struct {
			Embedding []float32 `json:"embedding"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, err
		}
		return out.Embedding, nil
	case strings.HasPrefix(model, "cohere.embed"):
		var out struct {
			Embeddings [][]float32 `json:"embeddings"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, err
		}
		if len(out.Embeddings) == 0 {
			return nil, errors.New("bedrock: empty cohere embed response")
		}
		return out.Embeddings[0], nil
	}
	return nil, fmt.Errorf("bedrock: unknown embed model %q", model)
}

func maxTokensOrDefault(req llm.GenerateRequest, def int) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return def
}

// Bytes / reader helpers — keep encode/decode swappable for tests.
var _ = bytes.NewReader

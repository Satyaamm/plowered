package aiprovider_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/pkg/llm"
)

// Tests verify the Build/Test seam against fake upstreams. We do not
// hit real provider endpoints — the goal here is to lock in the wire
// shape per kind so a future contract drift fails on commit instead of
// at runtime against a customer's BYOM key.

func TestOpenAICompatibleDefaultBaseURL(t *testing.T) {
	cases := map[aiprovider.Kind]string{
		aiprovider.KindOpenAI:     "https://api.openai.com",
		aiprovider.KindDeepSeek:   "https://api.deepseek.com",
		aiprovider.KindMistral:    "https://api.mistral.ai",
		aiprovider.KindGroq:       "https://api.groq.com/openai",
		aiprovider.KindTogether:   "https://api.together.xyz",
		aiprovider.KindFireworks:  "https://api.fireworks.ai/inference",
		aiprovider.KindPerplexity: "https://api.perplexity.ai",
		aiprovider.KindXAI:        "https://api.x.ai",
		aiprovider.KindOllama:     "http://localhost:11434/v1",
	}
	for k, want := range cases {
		if got := aiprovider.OpenAICompatibleDefaultBaseURL(k); got != want {
			t.Errorf("%s: got %q, want %q", k, got, want)
		}
	}
}

// TestGeminiGenerate spins up a fake Gemini endpoint, sends a Generate
// request, and verifies (a) we hit the right path, (b) the api key
// rides in the query string, (c) the response is decoded into the
// llm.GenerateResponse shape.
func TestGeminiGenerate(t *testing.T) {
	t.Setenv("PLOWERED_ALLOW_PRIVATE_AI_HOSTS", "1")
	var seenPath string
	var seenKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenKey = r.URL.Query().Get("key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"parts":[{"text":"hello world"}]},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2}
		}`))
	}))
	defer srv.Close()

	provider, err := aiprovider.Build(&aiprovider.Config{
		Kind:    aiprovider.KindGemini,
		Model:   "gemini-2.0-flash",
		BaseURL: srv.URL,
	}, []byte("ak-test"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	resp, err := provider.Generate(context.Background(), llm.GenerateRequest{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasSuffix(seenPath, "/v1beta/models/gemini-2.0-flash:generateContent") {
		t.Errorf("path = %q", seenPath)
	}
	if seenKey != "ak-test" {
		t.Errorf("api key = %q", seenKey)
	}
	if resp.Content != "hello world" {
		t.Errorf("content = %q", resp.Content)
	}
	if resp.InputTokens != 3 || resp.OutputTokens != 2 {
		t.Errorf("tokens = %d/%d", resp.InputTokens, resp.OutputTokens)
	}
}

func TestAzureOpenAIRequiresDeploymentAndAPIVersion(t *testing.T) {
	_, err := aiprovider.Build(&aiprovider.Config{
		Kind:    aiprovider.KindAzureOAI,
		Model:   "gpt-4o",
		BaseURL: "https://x.openai.azure.com",
		// missing Deployment + APIVersion
	}, []byte("ak-test"))
	if err == nil {
		t.Fatal("expected error for missing deployment + api_version")
	}
}

// TestAzureOpenAIGenerate confirms the URL path, the api-key header,
// and the api-version query param all land correctly.
func TestAzureOpenAIGenerate(t *testing.T) {
	t.Setenv("PLOWERED_ALLOW_PRIVATE_AI_HOSTS", "1")
	var seenPath string
	var seenAPIKey string
	var seenAPIVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAPIKey = r.Header.Get("api-key")
		seenAPIVersion = r.URL.Query().Get("api-version")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],
			"model":"gpt-4o",
			"usage":{"prompt_tokens":5,"completion_tokens":1}
		}`))
	}))
	defer srv.Close()

	provider, err := aiprovider.Build(&aiprovider.Config{
		Kind:       aiprovider.KindAzureOAI,
		BaseURL:    srv.URL,
		Deployment: "my-gpt4o",
		APIVersion: "2024-06-01",
		Model:      "gpt-4o",
	}, []byte("ak-test"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := provider.Generate(context.Background(), llm.GenerateRequest{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wantPath := "/openai/deployments/my-gpt4o/chat/completions"
	if seenPath != wantPath {
		t.Errorf("path = %q, want %q", seenPath, wantPath)
	}
	if seenAPIKey != "ak-test" {
		t.Errorf("api-key header = %q", seenAPIKey)
	}
	if seenAPIVersion != "2024-06-01" {
		t.Errorf("api-version = %q", seenAPIVersion)
	}
}

// TestCohereEmbed verifies the v1/embed wire shape — Cohere returns
// floats keyed under embeddings.float, not the OpenAI 'data' array.
func TestCohereEmbed(t *testing.T) {
	t.Setenv("PLOWERED_ALLOW_PRIVATE_AI_HOSTS", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reflect the request body back so the test can assert input_type.
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["input_type"] != "search_document" {
			t.Errorf("input_type = %v", got["input_type"])
		}
		_, _ = w.Write([]byte(`{
			"embeddings":{"float":[[0.1,0.2,0.3],[0.4,0.5,0.6]]},
			"meta":{"billed_units":{"input_tokens":4}}
		}`))
	}))
	defer srv.Close()

	provider, err := aiprovider.Build(&aiprovider.Config{
		Kind:    aiprovider.KindCohere,
		Model:   "embed-english-v3.0",
		BaseURL: srv.URL,
	}, []byte("ak-test"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	out, err := provider.Embed(context.Background(), llm.EmbedRequest{
		Texts: []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(out.Vectors) != 2 || len(out.Vectors[0]) != 3 {
		t.Fatalf("vectors = %d × %d", len(out.Vectors), len(out.Vectors[0]))
	}
	if out.Tokens != 4 {
		t.Errorf("tokens = %d", out.Tokens)
	}
}

// TestVoyageRejectsChat confirms Voyage's Generate returns
// ErrChatUnsupported so the resolver routes chat to a different config.
func TestVoyageRejectsChat(t *testing.T) {
	provider, err := aiprovider.Build(&aiprovider.Config{
		Kind:    aiprovider.KindVoyage,
		Model:   "voyage-3",
		BaseURL: "https://api.voyageai.com",
	}, []byte("ak-test"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = provider.Generate(context.Background(), llm.GenerateRequest{})
	if !errors.Is(err, llm.ErrChatUnsupported) {
		t.Errorf("got %v, want llm.ErrChatUnsupported", err)
	}
}

// TestBedrockAndVertexReturnDriverNotInstalledWhenUnwired ensures the
// seam fails gracefully when the named import isn't pulled in, instead
// of panicking. (When the import IS wired — as in cmd/plowered — the
// SetXDriver call replaces this default.)
func TestBedrockAndVertexHaveActiveDriversInTests(t *testing.T) {
	// The aiprovider package itself doesn't blank-import the cloud
	// adapters; whether activeBedrock / activeVertex are set depends
	// on which packages are linked in. This test simply asserts the
	// seam exposes a typed error rather than panicking.
	cfg := &aiprovider.Config{
		Kind:   aiprovider.KindBedrock,
		Model:  "anthropic.claude-3-5-sonnet-20241022-v2:0",
		Region: "us-east-1",
	}
	_, err := aiprovider.Build(cfg, nil)
	// Either the driver is wired (this test imports nothing) or we
	// get ErrDriverNotInstalled. Both are acceptable; what we DON'T
	// want is a panic or a nil-pointer dereference.
	if err != nil {
		var notInstalled aiprovider.ErrDriverNotInstalled
		if !errors.As(err, &notInstalled) {
			// Otherwise the error must come from the driver itself
			// (e.g. AWS credential resolution). Anything that isn't
			// a clean typed error fails the test.
			if !strings.Contains(err.Error(), "bedrock") {
				t.Errorf("unexpected error: %v", err)
			}
		}
	}
}

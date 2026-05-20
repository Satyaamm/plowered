package cost

import (
	"context"
	"strings"
	"time"
)

// RecordAI is the call-site helper for AI completion costs. It looks
// up the per-model rate, computes the USD cost, and writes the row.
// Recorder may be nil — that's a no-op so call-sites don't need a
// guard.
func RecordAI(ctx context.Context, r Recorder, tenantID, model string, inputTokens, outputTokens int, attrs map[string]any) {
	if r == nil || tenantID == "" {
		return
	}
	usd := EstimateAICost(model, inputTokens, outputTokens)
	a := mergeAttrs(attrs, map[string]any{
		"model":         model,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
	})
	_ = r.Record(ctx, Record{
		TenantID:   tenantID,
		TS:         time.Now().UTC(),
		Kind:       KindAICompletion,
		Provider:   providerFromModel(model),
		CostUSD:    usd,
		Attributes: a,
	})
}

// RecordWarehouseQuery is the call-site helper for warehouse query
// costs. provider is the warehouse type (postgres, snowflake, ...);
// elapsed is the wall-clock the query took.
func RecordWarehouseQuery(ctx context.Context, r Recorder, tenantID, provider string, elapsed time.Duration, rowCount int64, attrs map[string]any) {
	if r == nil || tenantID == "" {
		return
	}
	elapsedMs := elapsed.Milliseconds()
	usd := EstimateWarehouseCost(provider, elapsedMs, rowCount)
	a := mergeAttrs(attrs, map[string]any{
		"elapsed_ms": elapsedMs,
		"row_count":  rowCount,
	})
	_ = r.Record(ctx, Record{
		TenantID:   tenantID,
		TS:         time.Now().UTC(),
		Kind:       KindWarehouseQuery,
		Provider:   provider,
		CostUSD:    usd,
		Attributes: a,
	})
}

// providerFromModel maps a model name to the vendor that runs it. We
// don't carry vendor explicitly on the LLM response so this gives the
// dashboard a sensible group-by axis.
func providerFromModel(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.HasPrefix(lower, "gpt"), strings.HasPrefix(lower, "o1"), strings.HasPrefix(lower, "o3"):
		return "openai"
	case strings.HasPrefix(lower, "claude"):
		return "anthropic"
	case strings.HasPrefix(lower, "gemini"):
		return "google"
	case strings.HasPrefix(lower, "llama"), strings.HasPrefix(lower, "mistral"), strings.HasPrefix(lower, "local"):
		return "local"
	}
	return "unknown"
}

func mergeAttrs(base, extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

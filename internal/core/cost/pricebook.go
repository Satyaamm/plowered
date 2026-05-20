package cost

import "strings"

// AIPrice describes a model's per-1K-token rates in USD. Values are
// the vendor's published rates; update when the price book changes.
// Anything not in the table returns ZeroPrice, which records a row
// with cost_usd=0 so the volume is still visible.
type AIPrice struct {
	InputPer1K  float64
	OutputPer1K float64
}

// ZeroPrice is the fallback when a model isn't in the price book.
var ZeroPrice = AIPrice{}

// aiPriceBook is the static lookup. Keys are matched case-insensitively
// and by prefix so "gpt-4o-mini-2024-07-18" resolves the same row as
// "gpt-4o-mini". Add new entries as providers publish them.
var aiPriceBook = map[string]AIPrice{
	// OpenAI (USD per 1K tokens, as of late 2025 publication)
	"gpt-4o":         {InputPer1K: 0.0025, OutputPer1K: 0.01},
	"gpt-4o-mini":    {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"gpt-4-turbo":    {InputPer1K: 0.01, OutputPer1K: 0.03},
	"gpt-4":          {InputPer1K: 0.03, OutputPer1K: 0.06},
	"gpt-3.5-turbo":  {InputPer1K: 0.0005, OutputPer1K: 0.0015},

	// Anthropic
	"claude-opus":    {InputPer1K: 0.015, OutputPer1K: 0.075},
	"claude-sonnet":  {InputPer1K: 0.003, OutputPer1K: 0.015},
	"claude-haiku":   {InputPer1K: 0.00025, OutputPer1K: 0.00125},
}

// PriceFor returns the AIPrice for a model name. Matching is case-
// insensitive prefix: "GPT-4o-Mini-2024" matches the "gpt-4o-mini" row.
// Caller gets ZeroPrice + false when no entry matches.
func PriceFor(model string) (AIPrice, bool) {
	if model == "" {
		return ZeroPrice, false
	}
	lower := strings.ToLower(model)
	// Walk keys longest-first so "gpt-4o-mini" beats "gpt-4o" for
	// "gpt-4o-mini-2024-07-18".
	best := ""
	var bestPrice AIPrice
	for k, p := range aiPriceBook {
		if strings.HasPrefix(lower, k) && len(k) > len(best) {
			best = k
			bestPrice = p
		}
	}
	if best == "" {
		return ZeroPrice, false
	}
	return bestPrice, true
}

// EstimateAICost returns the USD cost for a completion at the model's
// published rates. Unknown models return 0.
func EstimateAICost(model string, inputTokens, outputTokens int) float64 {
	p, ok := PriceFor(model)
	if !ok {
		return 0
	}
	return (float64(inputTokens)/1000.0)*p.InputPer1K +
		(float64(outputTokens)/1000.0)*p.OutputPer1K
}

// EstimateWarehouseCost converts (elapsed_ms, row_count) to a coarse
// USD figure. v0 uses a flat rate per second of query wall-clock —
// directionally correct (longer queries = more expensive) without
// claiming billing accuracy. Per-warehouse models (Snowflake credits,
// BigQuery $/TB scanned) plug in here later.
func EstimateWarehouseCost(provider string, elapsedMs int64, _ int64) float64 {
	if elapsedMs <= 0 {
		return 0
	}
	ratePerSec := warehouseRatePerSec[strings.ToLower(provider)]
	if ratePerSec == 0 {
		ratePerSec = 0.000050 // safe default: $0.18/h compute
	}
	return (float64(elapsedMs) / 1000.0) * ratePerSec
}

// Per-second compute rate. Snowflake is the outlier because compute
// is billed by warehouse size; the figure here is a small-warehouse
// approximation. Operators who care about exact attribution should
// wire vendor billing exports instead of this estimator.
var warehouseRatePerSec = map[string]float64{
	"postgres":  0.000028, // ~$0.10/h matched RDS micro
	"redshift":  0.000139, // ~$0.50/h dc2.large
	"mysql":     0.000028,
	"snowflake": 0.000694, // ~$2.50/h XS warehouse
	"bigquery":  0.000050, // approximation — real billing is $/TB scanned
	"athena":    0.000050, // approximation — real billing is $/TB scanned
}

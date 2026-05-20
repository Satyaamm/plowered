package cost

import (
	"math"
	"testing"
)

func TestPriceForExactMatch(t *testing.T) {
	p, ok := PriceFor("gpt-4o")
	if !ok || p.InputPer1K == 0 {
		t.Errorf("gpt-4o should be priced, got %+v ok=%v", p, ok)
	}
}

func TestPriceForLongestPrefixWins(t *testing.T) {
	mini, _ := PriceFor("gpt-4o-mini-2024-07-18")
	want, _ := PriceFor("gpt-4o-mini")
	if mini.InputPer1K != want.InputPer1K {
		t.Errorf("dated model should resolve to mini, got %+v", mini)
	}
}

func TestPriceForUnknownReturnsZero(t *testing.T) {
	p, ok := PriceFor("imaginary-llm-9001")
	if ok || p != ZeroPrice {
		t.Errorf("unknown model should be zero, got %+v ok=%v", p, ok)
	}
}

func TestEstimateAICost(t *testing.T) {
	// 1000 input + 500 output on gpt-4o = 1*0.0025 + 0.5*0.01 = 0.0075
	got := EstimateAICost("gpt-4o", 1000, 500)
	if math.Abs(got-0.0075) > 1e-9 {
		t.Errorf("got %f want 0.0075", got)
	}
}

func TestEstimateWarehouseCostScalesWithTime(t *testing.T) {
	a := EstimateWarehouseCost("snowflake", 1000, 0)
	b := EstimateWarehouseCost("snowflake", 2000, 0)
	if b <= a {
		t.Errorf("doubling elapsed should ≥ double cost: a=%f b=%f", a, b)
	}
}

func TestEstimateWarehouseCostUnknownProviderUsesDefault(t *testing.T) {
	if got := EstimateWarehouseCost("imaginary-warehouse", 1000, 0); got <= 0 {
		t.Errorf("unknown provider should still record nonzero, got %f", got)
	}
}

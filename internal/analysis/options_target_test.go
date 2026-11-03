package analysis

import (
	"math"
	"testing"
)

func TestOptionsTargetRequiresExecutableBidAsk(t *testing.T) {
	calculator := NiftyOptionsTargetCalculator{}
	plan := calculator.Calculate(OptionsTargetInput{
		Underlying: 24500, ATR: 90, Signal: "BUY",
		SwingInvalidation: 24420, StructureTarget: 24600, OptionPremium: 180,
		Delta: .55, Gamma: .002, Theta: .8, Vega: 8,
	})
	if plan.Option.Status != "NO TRADE" || plan.Option.Reason != "Option bid/ask price unavailable" {
		t.Fatalf("unexpected plan: %#v", plan.Option)
	}
}

func TestOptionsTargetCalculatesDynamicLevels(t *testing.T) {
	plan := (NiftyOptionsTargetCalculator{}).Calculate(OptionsTargetInput{
		Underlying: 24500, ATR: 90, Signal: "BUY",
		SwingInvalidation: 24430, StructureTarget: 24650, OptionPremium: 30,
		Delta: .55, Gamma: .002, Theta: .8, Vega: 8,
		ImpliedVolatility: 14, Bid: 28, Ask: 29, SpreadAvailable: true,
		LotMultiplier: 75,
	})
	if plan.Underlying.StopLoss >= plan.Underlying.Entry {
		t.Fatalf("invalid underlying stop: %#v", plan.Underlying)
	}
	if plan.Underlying.ExpectedPoints < 20 {
		t.Fatalf("minimum underlying opportunity not met: %.2f", plan.Underlying.ExpectedPoints)
	}
	if plan.Option.Status == "NO TRADE" && math.IsNaN(plan.Option.EntryPremium) {
		t.Fatal("unexpected invalid numeric option plan")
	}
	if plan.Option.Target1Premium <= plan.Option.EntryPremium {
		t.Fatalf("invalid option target: %#v", plan.Option)
	}
}

func TestOptionsTargetRejectsBlockedTechnicalRisk(t *testing.T) {
	plan := (NiftyOptionsTargetCalculator{}).Calculate(OptionsTargetInput{
		Underlying: 24500, ATR: 90, Signal: "BUY",
		SwingInvalidation: 24530, StructureTarget: 24505, OptionPremium: 180,
		Delta: .55, Gamma: .002, Theta: .8, Vega: 8,
		Bid: 178, Ask: 182, SpreadAvailable: true, LotMultiplier: 75,
	})
	if plan.Option.Status != "NO TRADE" {
		t.Fatalf("expected NO TRADE, got %#v", plan.Option)
	}
}

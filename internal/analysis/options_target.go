package analysis

import (
	"math"
	"strings"

	"github.com/nageshgarje/trader/internal/market"
)

const (
	NiftyMinimumUnderlyingPoints = 20.0
	DefaultMaximumRiskRupees     = 2000.0
)

type OptionsTargetInput struct {
	Underlying        float64
	ATR               float64
	Signal            string
	SwingInvalidation float64
	StructureTarget   float64
	OptionPremium     float64
	Delta             float64
	Gamma             float64
	Theta             float64
	Vega              float64
	ImpliedVolatility float64
	Bid               float64
	Ask               float64
	SpreadAvailable   bool
	LotMultiplier     float64
}

type UnderlyingPlan struct {
	Entry          float64 `json:"entry"`
	Invalidation   float64 `json:"invalidation"`
	StopLoss       float64 `json:"stopLoss"`
	Target1        float64 `json:"target1"`
	Target2        float64 `json:"target2"`
	ExpectedPoints float64 `json:"expectedPoints"`
	RiskPoints     float64 `json:"riskPoints"`
	RiskReward     float64 `json:"riskReward"`
	Method         string  `json:"method"`
}

type OptionTargetPlan struct {
	Status          string  `json:"status"`
	Reason          string  `json:"reason"`
	EntryPremium    float64 `json:"entryPremium"`
	StopLossPremium float64 `json:"stopLossPremium"`
	Target1Premium  float64 `json:"target1Premium"`
	Target2Premium  float64 `json:"target2Premium"`
	RiskPerUnit     float64 `json:"riskPerUnit"`
	MaximumUnits    int     `json:"maximumUnits"`
	EstimatedTheta  float64 `json:"estimatedTheta"`
	AdverseIVImpact float64 `json:"adverseIVImpact"`
	ExpectedPAndL   float64 `json:"expectedPAndL"`
	DataQuality     string  `json:"dataQuality"`
	PremiumMethod   string  `json:"premiumMethod"`
}

type NiftyOptionsTargetPlan struct {
	Underlying UnderlyingPlan   `json:"underlying"`
	Option     OptionTargetPlan `json:"option"`
}

type OptionsTargetCalculator interface {
	Calculate(input OptionsTargetInput) NiftyOptionsTargetPlan
}

type NiftyOptionsTargetCalculator struct{}

func (NiftyOptionsTargetCalculator) Calculate(input OptionsTargetInput) NiftyOptionsTargetPlan {
	underlying := calculateUnderlyingPlan(input)
	option := calculateOptionPlan(input, underlying)
	return NiftyOptionsTargetPlan{Underlying: underlying, Option: option}
}

func calculateUnderlyingPlan(input OptionsTargetInput) UnderlyingPlan {
	direction := strings.ToUpper(input.Signal)
	atrBuffer := input.ATR
	if atrBuffer <= 0 {
		atrBuffer = input.Underlying * 0.0015
	}
	if direction == "BUY" {
		stopStructure := input.SwingInvalidation
		if stopStructure >= input.Underlying || stopStructure <= 0 {
			stopStructure = input.Underlying - atrBuffer
		}
		atrStop := input.Underlying - atrBuffer
		stop := math.Max(stopStructure, atrStop)
		target1 := math.Max(input.StructureTarget, input.Underlying+NiftyMinimumUnderlyingPoints)
		target1 = nearestVolatilityTarget(input.Underlying, stop, target1, atrBuffer)
		target2 := input.Underlying + math.Max(target1-input.Underlying+0.5*atrBuffer, 2*atrBuffer)
		risk := input.Underlying - stop
		reward := target1 - input.Underlying
		return UnderlyingPlan{
			Entry: input.Underlying, Invalidation: stopStructure, StopLoss: stop, Target1: target1, Target2: target2,
			ExpectedPoints: reward, RiskPoints: risk, RiskReward: reward / math.Max(risk, 0.01),
			Method: "Technical invalidation and ATR buffer selected for the nearer defensible stop; resistance/structure level controls target room",
		}
	}
	stopStructure := input.SwingInvalidation
	if stopStructure <= input.Underlying || stopStructure <= 0 {
		stopStructure = input.Underlying + atrBuffer
	}
	atrStop := input.Underlying + atrBuffer
	stop := math.Min(stopStructure, atrStop)
	target1 := math.Min(input.StructureTarget, input.Underlying-NiftyMinimumUnderlyingPoints)
	target1 = nearestVolatilityTargetSell(input.Underlying, stop, target1, atrBuffer)
	target2 := input.Underlying - math.Max(input.Underlying-target1+0.5*atrBuffer, 2*atrBuffer)
	risk := stop - input.Underlying
	reward := input.Underlying - target1
	return UnderlyingPlan{
		Entry: input.Underlying, Invalidation: stopStructure, StopLoss: stop, Target1: target1, Target2: target2,
		ExpectedPoints: reward, RiskPoints: risk, RiskReward: reward / math.Max(risk, 0.01),
		Method: "Technical invalidation and ATR buffer selected for the farther defensible stop; support/structure level controls target room",
	}
}

func nearestVolatilityTarget(entry, stop, structureTarget, atr float64) float64 {
	_ = stop
	_ = atr
	minimum := entry + NiftyMinimumUnderlyingPoints
	if structureTarget <= entry {
		return 0
	}
	if structureTarget-entry < NiftyMinimumUnderlyingPoints {
		return 0
	}
	return math.Max(minimum, structureTarget)
}

func nearestVolatilityTargetSell(entry, stop, structureTarget, atr float64) float64 {
	_ = stop
	_ = atr
	minimum := entry - NiftyMinimumUnderlyingPoints
	if structureTarget >= entry {
		return 0
	}
	if entry-structureTarget < NiftyMinimumUnderlyingPoints {
		return 0
	}
	return math.Min(minimum, structureTarget)
}

func calculateOptionPlan(input OptionsTargetInput, underlying UnderlyingPlan) OptionTargetPlan {
	if !input.SpreadAvailable || input.OptionPremium <= 0 || input.Bid <= 0 || input.Ask <= 0 || input.Ask < input.Bid {
		return OptionTargetPlan{
			Status: "NO TRADE", Reason: "Option bid/ask price unavailable", DataQuality: "incomplete",
			PremiumMethod: "No premium levels calculated without executable bid/ask data",
		}
	}
	if underlying.ExpectedPoints < NiftyMinimumUnderlyingPoints || underlying.RiskReward < 2 {
		return OptionTargetPlan{
			Status: "NO TRADE", Reason: "Underlying opportunity or risk/reward insufficient",
			DataQuality: "available", PremiumMethod: "Underlying opportunity gate failed",
		}
	}

	delta := input.Delta
	gamma := input.Gamma
	theta := input.Theta
	marketMove := underlying.ExpectedPoints
	repricedTarget := input.OptionPremium + delta*marketMove + 0.5*gamma*marketMove*marketMove - 2*theta
	repricedStop := input.OptionPremium - delta*math.Abs(underlying.RiskPoints) - 0.5*math.Abs(gamma)*underlying.RiskPoints*underlying.RiskPoints - 2*math.Abs(theta)
	repricedStop = math.Max(repricedStop, input.Bid*0.65)

	spread := input.Ask - input.Bid
	entry := input.Ask
	stop := math.Min(repricedStop, entry)
	target1 := math.Max(repricedTarget, entry+math.Abs(entry-stop)*2)
	target2 := entry + 3*(target1-entry)
	riskPerUnit := math.Max(entry-stop+spread, 0.01)
	multiplier := input.LotMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	maxUnits := int(math.Floor(DefaultMaximumRiskRupees / (riskPerUnit*multiplier + 25)))
	if maxUnits <= 0 {
		return OptionTargetPlan{
			Status: "NO TRADE", Reason: "₹2,000 maximum-risk budget cannot accommodate the technical premium stop",
			DataQuality: "available", EntryPremium: entry, StopLossPremium: stop, RiskPerUnit: riskPerUnit,
			PremiumMethod: "Risk-sized Greeks scenario after bid/ask spread",
		}
	}
	adverseIV := adverseIVImpact(input, multiplier)
	return OptionTargetPlan{
		Status: "VALID", EntryPremium: entry, StopLossPremium: stop, Target1Premium: target1, Target2Premium: target2,
		RiskPerUnit: riskPerUnit, MaximumUnits: maxUnits, EstimatedTheta: -2 * theta,
		AdverseIVImpact: adverseIV, ExpectedPAndL: (target1-entry)*multiplier*float64(maxUnits) - estimatedCharges(entry, multiplier),
		DataQuality: "available", PremiumMethod: "Delta/gamma/theta scenario anchored to live bid/ask; not a direct spot-to-premium conversion",
	}
}

func adverseIVImpact(input OptionsTargetInput, multiplier float64) float64 {
	if input.Vega <= 0 {
		return 0
	}
	return -0.03 * input.ImpliedVolatility * input.Vega * multiplier
}

func estimatedCharges(entry, multiplier float64) float64 {
	return math.Max(entry*multiplier*0.001, 25)
}

func OptionStrikeAvailable(chain market.OptionChain, signal string) (market.OptionStrike, bool) {
	for _, strike := range chain.Strikes {
		if strings.EqualFold(signal, "BUY") {
			continue
		}
		_ = strike
	}
	return market.OptionStrike{}, false
}

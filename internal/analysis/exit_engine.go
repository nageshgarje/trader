package analysis

import "time"

const (
	ExitTarget                 = "TARGET_HIT"
	ExitPremiumStop            = "PREMIUM_STOP_LOSS"
	ExitUnderlyingInvalidation = "UNDERLYING_STRUCTURE_INVALIDATED"
	ExitMomentumDeterioration  = "MOMENTUM_DETERIORATION"
	ExitIVCollapse             = "IV_COLLAPSE"
	ExitMaximumHoldingPeriod   = "MAXIMUM_HOLDING_PERIOD"
	ExitOvernightGap           = "OVERNIGHT_GAP_RISK"
	ExitExpiryRisk             = "EXPIRY_RISK"
	ExitTrailingStop           = "TRAILING_STOP_LOSS"
	ExitOvernightBlocked       = "OVERNIGHT_HOLD_BLOCKED"
)

type ExitPolicy struct {
	PremiumStopPercent       float64
	MinimumRiskReward        float64
	MaximumHoldingDays       int
	ExpiryProtectionDays     int
	IVCollapsePercent        float64
	TrailingActivationPoints float64
	TrailingATRMultiple      float64
}

func DefaultExitPolicy() ExitPolicy {
	return ExitPolicy{
		PremiumStopPercent:       35,
		MinimumRiskReward:        2,
		MaximumHoldingDays:       2,
		ExpiryProtectionDays:     2,
		IVCollapsePercent:        20,
		TrailingActivationPoints: 20,
		TrailingATRMultiple:      1.25,
	}
}

type PositionState struct {
	Direction               string
	EntryPremium            float64
	CurrentPremium          float64
	CurrentBid              float64
	InitialPremiumStop      float64
	HighestPremium          float64
	LowestPremium           float64
	UnderlyingInvalidated   bool
	MomentumDeteriorated    bool
	EntryIV                 float64
	CurrentIV               float64
	EntryDelta              float64
	CurrentDelta            float64
	EntryTheta              float64
	CurrentTheta            float64
	DaysToExpiry            int
	EntryTime               time.Time
	Now                     time.Time
	Overnight               bool
	TargetReached           bool
	GapThroughStop          bool
	TrendApprovedOvernight  bool
	OptionApprovedOvernight bool
	GlobalRiskApproved      bool
	EventRisk               string
}

type ExitDecision struct {
	Exit      bool    `json:"exit"`
	Reason    string  `json:"reason"`
	Premium   float64 `json:"premium"`
	Protected bool    `json:"protected"`
}

func EvaluateExit(policy ExitPolicy, state PositionState) ExitDecision {
	executionPremium := state.CurrentBid
	if executionPremium <= 0 {
		executionPremium = state.CurrentPremium
	}

	// Conservative execution: if an overnight/gap candle jumps through stop,
	// exit at the gap-through price and record slippage separately.
	if state.GapThroughStop {
		return ExitDecision{true, ExitOvernightGap, executionPremium, true}
	}
	if state.TargetReached {
		return ExitDecision{true, ExitTarget, executionPremium, true}
	}
	if state.UnderlyingInvalidated {
		return ExitDecision{true, ExitUnderlyingInvalidation, executionPremium, true}
	}
	if state.MomentumDeteriorated {
		return ExitDecision{true, ExitMomentumDeterioration, executionPremium, true}
	}
	if state.DaysToExpiry <= policy.ExpiryProtectionDays {
		return ExitDecision{true, ExitExpiryRisk, executionPremium, true}
	}
	if state.EntryIV > 0 && state.CurrentIV > 0 {
		ivLoss := (state.EntryIV - state.CurrentIV) / state.EntryIV * 100
		if ivLoss >= policy.IVCollapsePercent {
			return ExitDecision{true, ExitIVCollapse, executionPremium, true}
		}
	}
	if state.InitialPremiumStop > 0 && executionPremium <= state.InitialPremiumStop {
		return ExitDecision{true, ExitPremiumStop, executionPremium, true}
	}
	if state.Overnight && !overnightApproved(state) {
		return ExitDecision{true, ExitOvernightBlocked, executionPremium, false}
	}
	if holdingDays(state.EntryTime, state.Now) >= policy.MaximumHoldingDays {
		return ExitDecision{true, ExitMaximumHoldingPeriod, executionPremium, true}
	}
	return ExitDecision{Exit: false, Premium: executionPremium}
}

func overnightApproved(state PositionState) bool {
	return state.TrendApprovedOvernight &&
		state.OptionApprovedOvernight &&
		state.GlobalRiskApproved &&
		state.EventRisk == ""
}

func holdingDays(entry, now time.Time) int {
	if entry.IsZero() || now.Before(entry) {
		return 0
	}
	return int(now.Sub(entry)/(24*time.Hour)) + 1
}

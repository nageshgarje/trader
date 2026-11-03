package analysis

import (
	"testing"
	"time"
)

func overnightPosition() PositionState {
	now := time.Now()
	return PositionState{
		Direction: "BUY", EntryPremium: 100, CurrentPremium: 115, CurrentBid: 114,
		InitialPremiumStop: 65, HighestPremium: 120, EntryIV: 18, CurrentIV: 17,
		DaysToExpiry: 8, EntryTime: now.Add(-24 * time.Hour), Now: now,
		TrendApprovedOvernight: true, OptionApprovedOvernight: true, GlobalRiskApproved: true,
	}
}

func TestExitEngineUsesTargetBeforeMaximumHoldingPeriod(t *testing.T) {
	state := overnightPosition()
	state.TargetReached = true
	decision := EvaluateExit(DefaultExitPolicy(), state)
	if !decision.Exit || decision.Reason != ExitTarget {
		t.Fatalf("decision = %#v, want target exit", decision)
	}
}

func TestExitEngineExitsWhenUnderlyingIsInvalidated(t *testing.T) {
	state := overnightPosition()
	state.UnderlyingInvalidated = true
	decision := EvaluateExit(DefaultExitPolicy(), state)
	if !decision.Exit || decision.Reason != ExitUnderlyingInvalidation {
		t.Fatalf("decision = %#v, want underlying invalidation exit", decision)
	}
}

func TestExitEngineUsesPremiumBidStop(t *testing.T) {
	state := overnightPosition()
	state.CurrentBid = 64
	state.CurrentPremium = 64
	decision := EvaluateExit(DefaultExitPolicy(), state)
	if !decision.Exit || decision.Reason != ExitPremiumStop || decision.Premium != 64 {
		t.Fatalf("decision = %#v, want premium stop at bid", decision)
	}
}

func TestExitEngineBlocksUnapprovedOvernightHolding(t *testing.T) {
	state := overnightPosition()
	state.Overnight = true
	state.GlobalRiskApproved = false
	decision := EvaluateExit(DefaultExitPolicy(), state)
	if !decision.Exit || decision.Reason != ExitOvernightBlocked {
		t.Fatalf("decision = %#v, want overnight hold blocked", decision)
	}
}

func TestExitEngineProtectsExpiry(t *testing.T) {
	state := overnightPosition()
	state.DaysToExpiry = 1
	decision := EvaluateExit(DefaultExitPolicy(), state)
	if !decision.Exit || decision.Reason != ExitExpiryRisk {
		t.Fatalf("decision = %#v, want expiry risk exit", decision)
	}
}

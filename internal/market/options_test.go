package market

import (
	"math"
	"testing"
)

func TestBlackScholesGreeksAreFinite(t *testing.T) {
	delta, gamma, theta := blackScholesGreeks(24500, 24500, 10.0/365, 0.15, true, 300)
	if delta < 0 || delta > 1 || gamma < 0 || math.IsNaN(theta) {
		t.Fatalf("unexpected Greeks: delta=%v gamma=%v theta=%v", delta, gamma, theta)
	}
}

func TestOptionChainCalculatesPCRAndMaxPain(t *testing.T) {
	rows := []nseStrike{
		{ExpiryDate: "31-Dec-2026", Strike: 24400,
			CALL: nseOption{OpenInterest: 100000, LastPrice: 200, ImpliedVolatility: 15},
			PUT:  nseOption{OpenInterest: 180000, LastPrice: 100, ImpliedVolatility: 16}},
		{ExpiryDate: "31-Dec-2026", Strike: 24500,
			CALL: nseOption{OpenInterest: 150000, LastPrice: 150, ImpliedVolatility: 14},
			PUT:  nseOption{OpenInterest: 140000, LastPrice: 150, ImpliedVolatility: 15}},
	}
	chain := calculateOptionChain(24500, "31-Dec-2026", rows)
	if math.Abs(chain.PCR-320000.0/250000.0) > 0.01 {
		t.Fatalf("PCR = %v", chain.PCR)
	}
	if chain.MaxPain <= 0 || len(chain.Strikes) != 2 {
		t.Fatalf("unexpected option chain: %+v", chain)
	}
}

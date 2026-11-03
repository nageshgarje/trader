package analysis

import (
	"testing"

	"github.com/nageshgarje/trader/internal/market"
)

func TestAggregateCandles(t *testing.T) {
	points := []market.MinutePoint{
		{Time: 300, Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 10},
		{Time: 301, Open: 100.5, High: 102, Low: 100, Close: 101, Volume: 20},
		{Time: 600, Open: 101, High: 103, Low: 100.5, Close: 102, Volume: 30},
	}

	candles := aggregateCandles(points, 5*60)

	if len(candles) != 2 {
		t.Fatalf("got %d candles, want 2", len(candles))
	}
	if candles[0].High != 102 || candles[0].Low != 99 || candles[0].Volume != 30 {
		t.Fatalf("unexpected aggregated candle: %+v", candles[0])
	}
	if candles[0].Close != 101 {
		t.Fatalf("close = %.2f, want 101", candles[0].Close)
	}
}

func TestTradeLevelsForBuyAndSell(t *testing.T) {
	candles := []market.MinutePoint{{Time: 1, High: 105, Low: 99, Close: 104}}

	entry, stop, target := tradeLevels("BUY", 104, candles, 3)
	if entry != 105 || stop != 102 || target != 111 {
		t.Fatalf("BUY levels = %.2f/%.2f/%.2f", entry, stop, target)
	}

	entry, stop, target = tradeLevels("SELL", 104, candles, 3)
	if entry != 99 || stop != 102 || target != 93 {
		t.Fatalf("SELL levels = %.2f/%.2f/%.2f", entry, stop, target)
	}
}

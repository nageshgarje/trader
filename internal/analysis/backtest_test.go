package analysis

import (
	"math"
	"testing"
	"time"

	"github.com/nageshgarje/trader/internal/market"
)

func TestBacktestRunsWithTransactionCosts(t *testing.T) {
	points := make([]market.MinutePoint, 180)
	for i := range points {
		price := 25000 + float64(i%20)*8 + math.Sin(float64(i)/8)*50
		points[i] = market.MinutePoint{Time: time.Now().Unix() + int64(i*60), Open: price, High: price + 12, Low: price - 12, Close: price + 2, Volume: 1000 + float64(i%9)*100}
	}
	result := RunBacktest(points, BacktestConfig{Spread: 1, Slippage: .5, Brokerage: 1.5, Capital: 100000})
	if result.Trades < 0 {
		t.Fatal("backtest returned an invalid trade count")
	}
	if math.IsNaN(result.NetProfit) || math.IsInf(result.NetProfit, 0) {
		t.Fatal("backtest returned invalid P&L")
	}
}

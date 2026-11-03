package analysis

import (
	"math"

	"github.com/nageshgarje/trader/internal/market"
)

type BacktestConfig struct {
	Spread        float64
	Slippage      float64
	Brokerage     float64
	Capital       float64
	MinimumPoints float64
}

type BacktestResult struct {
	Trades          int     `json:"trades"`
	Winners         int     `json:"winners"`
	Losers          int     `json:"losers"`
	WinRate         float64 `json:"winRate"`
	NetProfit       float64 `json:"netProfit"`
	MaximumDrawdown float64 `json:"maximumDrawdown"`
	ProfitFactor    float64 `json:"profitFactor"`
	Expectancy      float64 `json:"expectancy"`
	Trades20Plus    int     `json:"trades20Plus"`
	TargetHitRate   float64 `json:"targetHitRate"`
	StopLossHitRate float64 `json:"stopLossHitRate"`
}

func RunBacktest(points []market.MinutePoint, config BacktestConfig) BacktestResult {
	if config.Capital <= 0 {
		config.Capital = 100000
	}
	if config.MinimumPoints <= 0 {
		config.MinimumPoints = MinimumOpportunityPoints
	}
	cost := config.Spread + config.Slippage + config.Brokerage
	equity := config.Capital
	peak := equity
	maxDrawdown := 0.0
	grossProfit, grossLoss := 0.0, 0.0
	result := BacktestResult{}
	for i := 60; i < len(points)-2; i++ {
		window := recentPoints(points[:i+1], 220)
		analyzed, err := (&VolumeAnalyzer{}).Analyze(market.NSE, window)
		if err != nil || analyzed.TradePlan.Signal == "NO TRADE" {
			continue
		}
		plan := analyzed.TradePlan
		potential := math.Abs(plan.Target1 - plan.Entry)
		if potential < config.MinimumPoints+cost {
			continue
		}
		candle := points[i+1]
		pnl := 0.0
		outcome := "stop"
		if plan.Signal == "BUY" {
			if candle.High >= plan.Target1 {
				pnl = potential - cost
				outcome = "target"
			} else if candle.Low <= plan.StopLoss {
				pnl = -(plan.Entry - plan.StopLoss) - cost
			}
		} else {
			if candle.Low <= plan.Target1 {
				pnl = potential - cost
				outcome = "target"
			} else if candle.High >= plan.StopLoss {
				pnl = -(plan.StopLoss - plan.Entry) - cost
			}
		}
		if pnl == 0 {
			continue
		}
		result.Trades++
		equity += pnl
		if equity > peak {
			peak = equity
		}
		if dd := (peak - equity) / peak; dd > maxDrawdown {
			maxDrawdown = dd
		}
		if pnl > 0 {
			result.Winners++
			grossProfit += pnl
		} else {
			result.Losers++
			grossLoss += -pnl
		}
		if outcome == "target" {
			result.TargetHitRate++
		} else {
			result.StopLossHitRate++
		}
		if potential >= 20 {
			result.Trades20Plus++
		}
	}
	if result.Trades > 0 {
		result.WinRate = float64(result.Winners) / float64(result.Trades) * 100
		result.NetProfit = equity - config.Capital
		result.Expectancy = result.NetProfit / float64(result.Trades)
		result.TargetHitRate = result.TargetHitRate / float64(result.Trades) * 100
		result.StopLossHitRate = result.StopLossHitRate / float64(result.Trades) * 100
	}
	if grossLoss > 0 {
		result.ProfitFactor = grossProfit / grossLoss
	}
	result.MaximumDrawdown = maxDrawdown * 100
	return result
}

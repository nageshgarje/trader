package analysis

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type ExecutedTrade struct {
	ID             string    `json:"id"`
	Direction      string    `json:"direction"`
	OptionType     string    `json:"optionType"`
	Strike         float64   `json:"strike"`
	Expiry         string    `json:"expiry"`
	EntryPremium   float64   `json:"entryPremium"`
	ExitPremium    float64   `json:"exitPremium"`
	PlannedRisk    float64   `json:"plannedRisk"`
	Costs          float64   `json:"costs"`
	EntryTime      time.Time `json:"entryTime"`
	ExitTime       time.Time `json:"exitTime"`
	IVRegime       string    `json:"ivRegime"`
	TrendRegime    string    `json:"trendRegime"`
	UnderlyingMove float64   `json:"underlyingMove"`
	StopLossHit    bool      `json:"stopLossHit"`
	TargetHit      bool      `json:"targetHit"`
	OvernightGap   bool      `json:"overnightGap"`
	Slippage       float64   `json:"slippage"`
	Status         string    `json:"status"`
}

type PerformanceGroup struct {
	Trades    int     `json:"trades"`
	Winners   int     `json:"winners"`
	Losers    int     `json:"losers"`
	WinRate   float64 `json:"winRate"`
	NetProfit float64 `json:"netProfit"`
}

type PerformanceReport struct {
	Status                          string                      `json:"status"`
	GeneratedAt                     time.Time                   `json:"generatedAt"`
	DataComplete                    bool                        `json:"dataComplete"`
	DataLimitation                  string                      `json:"dataLimitation"`
	TotalSignals                    int                         `json:"totalSignals"`
	TotalExecutedTrades             int                         `json:"totalExecutedTrades"`
	CETrades                        int                         `json:"ceTrades"`
	PETrades                        int                         `json:"peTrades"`
	WinningTrades                   int                         `json:"winningTrades"`
	LosingTrades                    int                         `json:"losingTrades"`
	WinRate                         float64                     `json:"winRate"`
	GrossProfit                     float64                     `json:"grossProfit"`
	GrossLoss                       float64                     `json:"grossLoss"`
	NetProfitAfterCosts             float64                     `json:"netProfitAfterCosts"`
	AveragePremiumGain              float64                     `json:"averagePremiumGain"`
	AveragePremiumLoss              float64                     `json:"averagePremiumLoss"`
	NetPnLPerTrade                  float64                     `json:"netPnLPerTrade"`
	MaximumDrawdown                 float64                     `json:"maximumDrawdown"`
	ProfitFactor                    float64                     `json:"profitFactor"`
	Expectancy                      float64                     `json:"expectancy"`
	SharpeRatio                     float64                     `json:"sharpeRatio"`
	SortinoRatio                    float64                     `json:"sortinoRatio"`
	AverageHoldingDurationMinutes   float64                     `json:"averageHoldingDurationMinutes"`
	OvernightGapLosses              int                         `json:"overnightGapLosses"`
	StopLossSlippage                float64                     `json:"stopLossSlippage"`
	TargetHitRate                   float64                     `json:"targetHitRate"`
	UnderlyingOpportunities20Plus   float64                     `json:"underlyingOpportunities20PlusPercent"`
	OptionTradesProfitableAfterCost float64                     `json:"optionTradesProfitableAfterCostsPercent"`
	TradesExceedingRiskBudget       float64                     `json:"tradesExceedingRiskBudgetPercent"`
	ByStrategy                      map[string]PerformanceGroup `json:"byStrategy"`
	ByExpiry                        map[string]PerformanceGroup `json:"byExpiry"`
	ByStrike                        map[string]PerformanceGroup `json:"byStrike"`
	ByIVRegime                      map[string]PerformanceGroup `json:"byIVRegime"`
	ByTrendRegime                   map[string]PerformanceGroup `json:"byTrendRegime"`
}

func BuildPerformanceReport(signals int, trades []ExecutedTrade) PerformanceReport {
	report := PerformanceReport{
		Status: "insufficient_data", GeneratedAt: time.Now(), TotalSignals: signals,
		ByStrategy: make(map[string]PerformanceGroup), ByExpiry: make(map[string]PerformanceGroup),
		ByStrike: make(map[string]PerformanceGroup), ByIVRegime: make(map[string]PerformanceGroup),
		ByTrendRegime: make(map[string]PerformanceGroup),
	}
	complete := len(trades) > 0
	pnls := make([]float64, 0, len(trades))
	gains, losses := make([]float64, 0), make([]float64, 0)
	durations := make([]float64, 0, len(trades))
	equity, peak, drawdown := 0.0, 0.0, 0.0
	targets, opportunities20 := 0, 0
	riskExceeded := 0

	for _, trade := range trades {
		if trade.EntryTime.IsZero() || trade.ExitTime.IsZero() || trade.Status != "completed" {
			continue
		}
		complete = true
		report.TotalExecutedTrades++
		if trade.OptionType == "CE" {
			report.CETrades++
		} else if trade.OptionType == "PE" {
			report.PETrades++
		}
		pnl := (trade.ExitPremium - trade.EntryPremium) - trade.Costs
		pnls = append(pnls, pnl)
		if trade.OptionType == "CE" || trade.OptionType == "PE" {
			if pnl > 0 {
				report.WinningTrades++
				report.GrossProfit += pnl
				gains = append(gains, pnl)
			} else {
				report.LosingTrades++
				report.GrossLoss += -pnl
				losses = append(losses, pnl)
			}
		}
		report.NetProfitAfterCosts += pnl
		equity += pnl
		peak = math.Max(peak, equity)
		drawdown = math.Max(drawdown, peak-equity)
		durations = append(durations, trade.ExitTime.Sub(trade.EntryTime).Minutes())
		report.StopLossSlippage += trade.Slippage
		if trade.TargetHit {
			targets++
		}
		if trade.OvernightGap && pnl < 0 {
			report.OvernightGapLosses++
		}
		if math.Abs(trade.UnderlyingMove) >= 20 {
			opportunities20++
		}
		if trade.PlannedRisk > 2000 && trade.PlannedRisk > 0 {
			riskExceeded++
		}
		addGroupedResult(report.ByExpiry, trade.Expiry, pnl)
		addGroupedResult(report.ByStrike, formatStrike(trade.Strike), pnl)
		addGroupedResult(report.ByIVRegime, trade.IVRegime, pnl)
		addGroupedResult(report.ByTrendRegime, trade.TrendRegime, pnl)
	}
	report.DataComplete = complete
	if complete {
		report.Status = "validated"
		report.DataLimitation = ""
	} else {
		report.DataLimitation = "No completed option executions are stored. Underlying signals, premium P&L, strike, expiry, IV, and strategy grouping cannot be reported as performance."
	}
	if report.TotalExecutedTrades > 0 {
		report.WinRate = float64(report.WinningTrades) / float64(report.TotalExecutedTrades) * 100
		report.NetPnLPerTrade = report.NetProfitAfterCosts / float64(report.TotalExecutedTrades)
		report.TargetHitRate = float64(targets) / float64(report.TotalExecutedTrades) * 100
		report.UnderlyingOpportunities20Plus = float64(opportunities20) / float64(report.TotalExecutedTrades) * 100
		report.TradesExceedingRiskBudget = float64(riskExceeded) / float64(report.TotalExecutedTrades) * 100
		report.StopLossSlippage /= float64(report.TotalExecutedTrades)
		report.OptionTradesProfitableAfterCost = float64(report.WinningTrades) / float64(report.TotalExecutedTrades) * 100
		report.Expectancy = report.NetPnLPerTrade
		report.MaximumDrawdown = drawdown
		for _, duration := range durations {
			report.AverageHoldingDurationMinutes += duration
		}
		report.AverageHoldingDurationMinutes /= float64(len(durations))
	}
	if len(gains) > 0 {
		report.AveragePremiumGain = average(gains)
	}
	if len(losses) > 0 {
		report.AveragePremiumLoss = average(losses)
	}
	if report.GrossLoss > 0 {
		report.ProfitFactor = report.GrossProfit / report.GrossLoss
	}
	report.SharpeRatio = ratioStatistics(pnls, false)
	report.SortinoRatio = ratioStatistics(pnls, true)
	return report
}

func addGroupedResult(groups map[string]PerformanceGroup, key string, pnl float64) {
	if key == "" {
		key = "Unknown"
	}
	group := groups[key]
	group.Trades++
	group.NetProfit += pnl
	if pnl > 0 {
		group.Winners++
	} else if pnl < 0 {
		group.Losers++
	}
	if group.Trades > 0 {
		group.WinRate = float64(group.Winners) / float64(group.Trades) * 100
	}
	groups[key] = group
}

func formatStrike(strike float64) string {
	if strike <= 0 {
		return "Unknown"
	}
	return strconvFormat(strike)
}

func average(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func ratioStatistics(values []float64, downsideOnly bool) float64 {
	if len(values) == 0 {
		return 0
	}
	mean := average(values)
	var sum, sumDown float64
	for _, value := range values {
		diff := value - mean
		sum += diff * diff
		if value < 0 {
			sumDown += value * value
		}
	}
	denominator := math.Sqrt(sum / float64(len(values)))
	if downsideOnly {
		denominator = math.Sqrt(sumDown / float64(len(values)))
	}
	if denominator == 0 {
		return 0
	}
	return mean / denominator
}

func strconvFormat(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), ".")
}

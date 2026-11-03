package analysis

import (
	"math"

	"github.com/nageshgarje/trader/internal/market"
)

const (
	MinimumOpportunityPoints = 20.0
	MinimumSignalScore       = 70.0
	MinimumRiskReward        = 2.0
	EstimatedTransactionCost = 3.0
)

type TradePlan struct {
	Signal               string   `json:"signal"`
	Entry                float64  `json:"entry"`
	StopLoss             float64  `json:"stopLoss"`
	Target1              float64  `json:"target1"`
	Target2              float64  `json:"target2"`
	Target3              float64  `json:"target3"`
	PotentialPoints      float64  `json:"potentialPoints"`
	RiskPoints           float64  `json:"riskPoints"`
	RiskReward           float64  `json:"riskReward"`
	ATR                  float64  `json:"atr"`
	SignalScore          float64  `json:"signalScore"`
	TrailingStop         float64  `json:"trailingStop"`
	MarketRegime         string   `json:"marketRegime"`
	Invalidation         string   `json:"invalidation"`
	SupportingTimeframes []string `json:"supportingTimeframes"`
}

func buildTradePlan(points []market.MinutePoint, result Result) TradePlan {
	if len(points) == 0 || result.Last <= 0 {
		return TradePlan{Signal: "NO TRADE", MarketRegime: "Insufficient data"}
	}
	currentATR := math.Max(atr(points, 14), averageRange(recentPoints(points, 5)))
	trendScore := trendAlignmentScore(points, result)
	structureScore := marketStructureScore(points, result)
	momentumScore := momentumScore(result)
	volumeScore := volumeAnalysisScore(result)
	vwapScore := vwapAlignmentScore(result)
	volatilityScore := volatilityScore(currentATR, result.Last)

	score := 20*trendScore + 20*structureScore + 15*momentumScore +
		15*volumeScore + 10*vwapScore + 10*volatilityScore
	if result.Accuracy > 0 {
		score = math.Min(100, score*0.85+result.Accuracy*0.15)
	}
	score /= 100

	signal := directionalConsensus(result)
	if signal == "HOLD" || score < MinimumSignalScore/100*100 {
		return TradePlan{Signal: "NO TRADE", SignalScore: score, ATR: currentATR, MarketRegime: detectMarketRegime(points, currentATR)}
	}

	stop, target1, target2, target3, valid := dynamicLevels(signal, result.Last, points, currentATR)
	if !valid {
		return TradePlan{Signal: "NO TRADE", SignalScore: score, ATR: currentATR, MarketRegime: detectMarketRegime(points, currentATR)}
	}
	risk := math.Abs(result.Last - stop)
	potential := math.Abs(target1 - result.Last)
	rr := potential / math.Max(risk, 0.01)
	if potential < MinimumOpportunityPoints+EstimatedTransactionCost || rr < MinimumRiskReward {
		return TradePlan{Signal: "NO TRADE", SignalScore: score, PotentialPoints: potential, RiskPoints: risk, ATR: currentATR, MarketRegime: detectMarketRegime(points, currentATR)}
	}
	trailing := stop
	if signal == "BUY" {
		trailing = result.Last - 1.25*currentATR
	} else {
		trailing = result.Last + 1.25*currentATR
	}
	return TradePlan{
		Signal: signal, Entry: result.Last, StopLoss: stop, Target1: target1, Target2: target2, Target3: target3,
		PotentialPoints: potential, RiskPoints: risk, RiskReward: rr, ATR: currentATR, SignalScore: score,
		TrailingStop: trailing, MarketRegime: detectMarketRegime(points, currentATR),
		Invalidation:         marketStructureInvalidation(signal, points),
		SupportingTimeframes: supportingTimeframes(result.Timeframes, signal),
	}
}

func trendAlignmentScore(points []market.MinutePoint, result Result) float64 {
	if len(points) == 0 || result.Indicators.EMA20 == 0 {
		return 0
	}
	count := 0
	if result.Last > result.VWAP {
		count++
	} else {
		count--
	}
	if result.Last > result.Indicators.EMA20 {
		count++
	} else {
		count--
	}
	if result.Last > result.Indicators.SMA20 {
		count++
	} else {
		count--
	}
	if result.Indicators.ADX14 >= 20 {
		count++
	}
	return normalizedScore(count, 4)
}

func marketStructureScore(points []market.MinutePoint, result Result) float64 {
	_ = result
	if len(points) < 20 {
		return 0
	}
	recent := recentPoints(points, 20)
	highs, lows := swingPoints(recent)
	if len(highs) >= 2 && highs[len(highs)-1] > highs[len(highs)-2] {
		return .85
	}
	if len(lows) >= 2 && lows[len(lows)-1] < lows[len(lows)-2] {
		return .85
	}
	return .45
}

func momentumScore(result Result) float64 {
	score := 0.0
	for _, item := range []IndicatorSignal{
		{Signal: signalForValue(result.Indicators.RSI14, 55, 45)},
		result.IndicatorSignalsValue("MACD"),
	} {
		if item.Signal == "BUY" || item.Signal == "SELL" {
			score += .35
		}
	}
	if result.Indicators.RSI14 >= 55 || result.Indicators.RSI14 <= 45 {
		score += .30
	}
	return math.Min(score, 1)
}

func volumeAnalysisScore(result Result) float64 {
	total := result.BuyVolume + result.SellVolume
	if total == 0 {
		return 0
	}
	flow := math.Abs(result.BuyVolume-result.SellVolume) / total
	return math.Min(1, flow+math.Min(result.VolumeAcceleration/3, .35))
}

func vwapAlignmentScore(result Result) float64 {
	if result.VWAP == 0 {
		return 0
	}
	distance := math.Abs(result.Last-result.VWAP) / result.VWAP
	if distance < .0005 || result.Indicators.EMA20 == 0 {
		return .75
	}
	if result.Last > result.VWAP && result.Last > result.Indicators.EMA20 || result.Last < result.VWAP && result.Last < result.Indicators.EMA20 {
		return 1
	}
	return .30
}

func volatilityScore(atr, price float64) float64 {
	if price <= 0 {
		return 0
	}
	ratio := atr / price
	switch {
	case ratio >= .0005 && ratio <= .025:
		return 1
	case ratio > .025:
		return .65
	default:
		return .35
	}
}

func dynamicLevels(signal string, last float64, points []market.MinutePoint, atrValue float64) (stop, t1, t2, t3 float64, valid bool) {
	recent := recentPoints(points, 20)
	high, low := candleHigh(recent), candleLow(recent)
	structureBuffer := math.Max(atrValue*0.5, 0.01)
	if signal == "BUY" {
		stop = low - structureBuffer
		room := 2*math.Max(atrValue, MinimumOpportunityPoints) + EstimatedTransactionCost
		t1 = math.Max(last+MinimumOpportunityPoints+EstimatedTransactionCost, high+2*atrValue)
		t2 = last + math.Max(room, 1.5*(last-stop))
		t3 = last + math.Max(2.5*(last-stop), t2-last+atrValue)
		valid = t1 > last && last-stop > 0 && t1-last >= MinimumOpportunityPoints+EstimatedTransactionCost
	} else if signal == "SELL" {
		stop = high + structureBuffer
		room := 2*math.Max(atrValue, MinimumOpportunityPoints) + EstimatedTransactionCost
		t1 = math.Min(last-MinimumOpportunityPoints-EstimatedTransactionCost, low-2*atrValue)
		t2 = last - math.Max(room, 1.5*(stop-last))
		t3 = last - math.Max(2.5*(stop-last), last-t2+atrValue)
		valid = t1 < last && stop-last > 0 && last-t1 >= MinimumOpportunityPoints+EstimatedTransactionCost
	} else {
		return 0, 0, 0, 0, false
	}
	return
}

func detectMarketRegime(points []market.MinutePoint, atrValue float64) string {
	if len(points) < 50 {
		return "Insufficient history"
	}
	last := points[len(points)-1].Close
	ma200 := simpleAverage(recentPoints(points, minInt(len(points), 200)), false)
	atrRatio := atrValue / max(last, 1)
	if atrRatio > .0025 {
		return "High volatility"
	}
	if atrRatio < .0006 {
		return "Low volatility"
	}
	if last > ma200 && atrRatio >= .0006 {
		return "Strong bullish trend"
	}
	if last < ma200 && atrRatio >= .0006 {
		return "Strong bearish trend"
	}
	return "Sideways market"
}

func simpleAverage(points []market.MinutePoint, _ bool) float64 {
	if len(points) == 0 {
		return 0
	}
	total := 0.0
	for _, point := range points {
		total += point.Close
	}
	return total / float64(len(points))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func signalForValue(value, bullish, bearish float64) string {
	if value >= bullish {
		return "BUY"
	}
	if value <= bearish {
		return "SELL"
	}
	return "HOLD"
}

func normalizedScore(value, maximum int) float64 {
	if maximum <= 0 {
		return 0
	}
	return math.Max(0, math.Min(1, (float64(value)/float64(maximum)+1)/2))
}

func directionalConsensus(result Result) string {
	bullish := result.Last > result.VWAP
	bearish := result.Last < result.VWAP
	if result.Indicators.MACD > result.Indicators.SignalLine {
		bullish = true
	}
	if result.Indicators.MACD < result.Indicators.SignalLine {
		bearish = true
	}
	if result.Indicators.RSI14 >= 55 {
		bullish = true
	}
	if result.Indicators.RSI14 <= 45 {
		bearish = true
	}
	if bullish == bearish {
		return "HOLD"
	}
	if bullish {
		return "BUY"
	}
	return "SELL"
}

func supportingTimeframes(frames []TimeframeResult, signal string) []string {
	names := make([]string, 0)
	for _, frame := range frames {
		if frame.Signal == signal {
			names = append(names, frame.Timeframe)
		}
	}
	return names
}

func marketStructureInvalidation(signal string, points []market.MinutePoint) string {
	if len(points) == 0 {
		return "Strategy invalidated if current market data becomes unavailable"
	}
	if signal == "BUY" {
		return "Cancel below the latest confirmed swing low"
	}
	return "Cancel above the latest confirmed swing high"
}

func swingPoints(points []market.MinutePoint) (highs, lows []float64) {
	for i := 2; i < len(points)-2; i++ {
		if points[i].High > points[i-1].High && points[i].High >= points[i-2].High && points[i].High > points[i+1].High {
			highs = append(highs, points[i].High)
		}
		if points[i].Low < points[i-1].Low && points[i].Low <= points[i-2].Low && points[i].Low < points[i+1].Low {
			lows = append(lows, points[i].Low)
		}
	}
	return
}

func (r Result) IndicatorSignalsValue(_ string) IndicatorSignal {
	return IndicatorSignal{}
}

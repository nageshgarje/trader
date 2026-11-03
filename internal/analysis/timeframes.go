package analysis

import (
	"math"

	"github.com/nageshgarje/trader/internal/market"
)

var supportedTimeframes = []struct {
	name     string
	interval int64
	minimum  int
}{
	{name: "5m", interval: 5 * 60, minimum: 20},
	{name: "15m", interval: 15 * 60, minimum: 14},
	{name: "1h", interval: 60 * 60, minimum: 10},
}

func analyzeTimeframes(_ market.Exchange, points []market.MinutePoint) []TimeframeResult {
	results := make([]TimeframeResult, 0, len(supportedTimeframes))
	for _, timeframe := range supportedTimeframes {
		candles := aggregateCandles(points, timeframe.interval)
		if len(candles) < timeframe.minimum {
			results = append(results, TimeframeResult{
				Timeframe: timeframe.name,
				Reason:    "Not enough candles for analysis",
			})
			continue
		}

		last := candles[len(candles)-1].Close
		vwap := volumeWeightedAverage(candles)
		buy, sell := estimatedFlow(recentPoints(candles, 20))
		acceleration := 1.0
		if previous := sumVolume(recentWindow(candles, 40, 20)); previous > 0 {
			acceleration = sumVolume(recentPoints(candles, 20)) / previous
		}
		signal, confidence, reasons := classify(candles, last, vwap, buy, sell, acceleration)
		indicators := calculateIndicators(candles, vwap)
		signals := indicatorSignals(candles, indicators, buy, sell)
		if signal == "HOLD" {
			signal, confidence = indicatorConsensus(signals, signal, confidence)
		}

		atRisk := math.Max(atr(candles, 14), averageRange(recentPoints(candles, 5)))
		entry, stopLoss, target := tradeLevels(signal, last, candles, atRisk)
		reason := "Mixed indicator confirmation"
		if len(reasons) > 0 {
			reason = reasons[0]
		}
		results = append(results, TimeframeResult{
			Timeframe: timeframe.name, Last: last, Signal: signal,
			Confidence: confidence, Entry: entry, StopLoss: stopLoss, Target: target,
			Indicators: signals, Reason: reason,
		})
	}
	return results
}

func aggregateCandles(points []market.MinutePoint, intervalSeconds int64) []market.MinutePoint {
	if len(points) == 0 || intervalSeconds <= 0 {
		return nil
	}
	candles := make([]market.MinutePoint, 0, len(points)/int(intervalSeconds/60)+1)
	for _, point := range points {
		if point.Close <= 0 {
			continue
		}
		bucket := point.Time / intervalSeconds * intervalSeconds
		if len(candles) == 0 || candles[len(candles)-1].Time != bucket {
			candles = append(candles, market.MinutePoint{
				Time: bucket, Open: point.Open, High: point.High,
				Low: point.Low, Close: point.Close, Volume: point.Volume,
			})
			continue
		}
		current := &candles[len(candles)-1]
		current.High = math.Max(current.High, point.High)
		current.Low = math.Min(current.Low, point.Low)
		current.Close = point.Close
		current.Volume += point.Volume
	}
	return candles
}

func AggregateCandles(points []market.MinutePoint, intervalSeconds int64) []market.MinutePoint {
	return aggregateCandles(points, intervalSeconds)
}

func indicatorConsensus(signals []IndicatorSignal, fallback string, fallbackConfidence float64) (string, float64) {
	buyVotes, sellVotes := 0, 0
	buyStrength, sellStrength := 0.0, 0.0
	for _, signal := range signals {
		switch signal.Signal {
		case "BUY":
			buyVotes++
			buyStrength += signal.Strength
		case "SELL":
			sellVotes++
			sellStrength += signal.Strength
		}
	}
	if buyVotes >= 3 && buyVotes > sellVotes {
		return "BUY", clamp(50+buyStrength/float64(buyVotes)*0.45, 50, 85)
	}
	if sellVotes >= 3 && sellVotes > buyVotes {
		return "SELL", clamp(50+sellStrength/float64(sellVotes)*0.45, 50, 85)
	}
	return fallback, fallbackConfidence
}

func tradeLevels(signal string, last float64, candles []market.MinutePoint, atRisk float64) (entry, stopLoss, target float64) {
	if atRisk <= 0 {
		atRisk = math.Max(last*0.001, 0.01)
	}
	recent := recentPoints(candles, 5)
	entry = last
	switch signal {
	case "BUY":
		entry = last
		if len(recent) > 0 {
			entry = math.Max(entry, candleHigh(recent))
		}
		stopLoss = entry - atRisk
		target = entry + 2*atRisk
	case "SELL":
		entry = last
		if len(recent) > 0 {
			entry = math.Min(entry, candleLow(recent))
		}
		stopLoss = entry + atRisk
		target = entry - 2*atRisk
	default:
		stopLoss, target = 0, 0
	}
	return entry, stopLoss, target
}

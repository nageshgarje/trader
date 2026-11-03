package analysis

import (
	"math"
	"sort"
	"time"

	"github.com/nageshgarje/trader/internal/market"
)

type Result struct {
	Exchange           market.Exchange      `json:"exchange"`
	UpdatedAt          time.Time            `json:"updatedAt"`
	Last               float64              `json:"last"`
	VWAP               float64              `json:"vwap"`
	POC                float64              `json:"poc"`
	RangeProxy         float64              `json:"rangeProxy"`
	Return10mPercent   float64              `json:"return10mPercent"`
	VolumeAcceleration float64              `json:"volumeAcceleration"`
	BuyVolume          float64              `json:"buyVolume"`
	SellVolume         float64              `json:"sellVolume"`
	DeltaProxy         float64              `json:"deltaProxy"`
	Target10m          float64              `json:"target10m"`
	LowBand            float64              `json:"lowBand"`
	HighBand           float64              `json:"highBand"`
	Signal             string               `json:"signal"`
	Confidence         float64              `json:"confidence"`
	Accuracy           float64              `json:"accuracy"`
	TargetHitRate      float64              `json:"targetHitRate"`
	ValidationSamples  int                  `json:"validationSamples"`
	IndicatorAccuracy  map[string]float64   `json:"indicatorAccuracy"`
	Reasons            []string             `json:"reasons"`
	Indicators         IndicatorSet         `json:"indicators"`
	IndicatorSignals   []IndicatorSignal    `json:"indicatorSignals"`
	Recent             []market.MinutePoint `json:"recent"`
	Timeframes         []TimeframeResult    `json:"timeframes"`
	TradePlan          TradePlan            `json:"tradePlan"`
}

type IndicatorSet struct {
	SMA20          float64 `json:"sma20"`
	EMA20          float64 `json:"ema20"`
	RSI14          float64 `json:"rsi14"`
	RSI2           float64 `json:"rsi2"`
	MACD           float64 `json:"macd"`
	SignalLine     float64 `json:"signalLine"`
	BollingerUpper float64 `json:"bollingerUpper"`
	BollingerLower float64 `json:"bollingerLower"`
	ATR14          float64 `json:"atr14"`
	ADX14          float64 `json:"adx14"`
	StochasticK    float64 `json:"stochasticK"`
	StochasticD    float64 `json:"stochasticD"`
	ROC10          float64 `json:"roc10"`
	OBV            float64 `json:"obv"`
	VWAPDistance   float64 `json:"vwapDistance"`
}

type IndicatorSignal struct {
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	Signal   string  `json:"signal"`
	Strength float64 `json:"strength"`
}

type TimeframeResult struct {
	Timeframe  string            `json:"timeframe"`
	Last       float64           `json:"last"`
	Signal     string            `json:"signal"`
	Confidence float64           `json:"confidence"`
	Entry      float64           `json:"entry"`
	StopLoss   float64           `json:"stopLoss"`
	Target     float64           `json:"target"`
	Indicators []IndicatorSignal `json:"indicators"`
	Reason     string            `json:"reason"`
}

type VolumeAnalyzer struct{}

func NewVolumeAnalyzer() *VolumeAnalyzer { return &VolumeAnalyzer{} }

func (a *VolumeAnalyzer) Analyze(exchange market.Exchange, points []market.MinutePoint) (Result, error) {
	points = recentPoints(points, 220)
	if len(points) < 11 {
		return Result{}, ErrInsufficientData
	}
	last := points[len(points)-1].Close
	vwap := volumeWeightedAverage(points)
	recent := recentPoints(points, 20)
	acceleration := sumVolume(recent) / max(sumVolume(recentWindow(points, 40, 20)), 1)
	buy, sell := estimatedFlow(recent)
	poc := pointOfControl(recent)
	rangeProxy := averageRange(recentPoints(points, 5))
	ret10 := 0.0
	if points[len(points)-11].Close != 0 {
		ret10 = (last/points[len(points)-11].Close - 1) * 100
	}
	signal, confidence, reasons := classify(points, last, vwap, buy, sell, acceleration)
	indicatorAccuracy := calculateIndicatorAccuracy(points)
	confirmed := volumeConfirmed(buy, sell, acceleration)
	averageAccuracy := averageIndicatorAccuracy(indicatorAccuracy)
	if !confirmed || averageAccuracy < 55 {
		signal = "HOLD"
		confidence = math.Min(confidence, 50)
		reasons = append(reasons, "Setup held because indicator accuracy or volume confirmation is weak")
	}
	indicators := calculateIndicators(points, vwap)
	indicatorSignals := indicatorSignals(points, indicators, buy, sell)
	target, lowBand, highBand, hitRate, samples := validatedTarget(points, signal, last, rangeProxy)
	result := Result{
		Exchange: exchange, UpdatedAt: time.Now(), Last: last, VWAP: vwap, POC: poc,
		RangeProxy: rangeProxy, Return10mPercent: ret10, VolumeAcceleration: acceleration,
		BuyVolume: buy, SellVolume: sell, DeltaProxy: buy - sell, Target10m: target,
		LowBand: lowBand, HighBand: highBand,
		Signal: signal, Confidence: confidence, Accuracy: walkForwardAccuracy(points),
		TargetHitRate: hitRate, ValidationSamples: samples,
		Reasons: reasons, Recent: recent,
		IndicatorAccuracy: indicatorAccuracy,
		Indicators:        indicators, IndicatorSignals: indicatorSignals,
		Timeframes: analyzeTimeframes(exchange, points),
	}
	result.TradePlan = buildTradePlan(points, result)
	return result, nil
}

func recentPoints(points []market.MinutePoint, count int) []market.MinutePoint {
	if len(points) <= count {
		return append([]market.MinutePoint(nil), points...)
	}
	return append([]market.MinutePoint(nil), points[len(points)-count:]...)
}

func recentWindow(points []market.MinutePoint, endOffset, startOffset int) []market.MinutePoint {
	end := len(points) - endOffset
	start := len(points) - startOffset
	if end < 0 {
		end = 0
	}
	if start < end {
		start = end
	}
	return points[end:start]
}

func volumeWeightedAverage(points []market.MinutePoint) float64 {
	var volume, value float64
	for _, point := range points {
		volume += point.Volume
		value += point.Close * point.Volume
	}
	if volume == 0 {
		return points[len(points)-1].Close
	}
	return value / volume
}

func sumVolume(points []market.MinutePoint) float64 {
	var total float64
	for _, point := range points {
		total += point.Volume
	}
	return total
}

func estimatedFlow(points []market.MinutePoint) (float64, float64) {
	var buy, sell float64
	for _, point := range points {
		position := 0.0
		if point.High > point.Low {
			position = clamp((point.Close-point.Open)/(point.High-point.Low), -1, 1)
		}
		buyPart := point.Volume * (0.5 + 0.5*position)
		buy += buyPart
		sell += point.Volume - buyPart
	}
	return buy, sell
}

func pointOfControl(points []market.MinutePoint) float64 {
	profile := make(map[int64]float64)
	for _, point := range points {
		profile[int64(point.Close*4)] += point.Volume
	}
	var buckets []int64
	for bucket := range profile {
		buckets = append(buckets, bucket)
	}
	sort.Slice(buckets, func(i, j int) bool {
		return profile[buckets[i]] > profile[buckets[j]]
	})
	if len(buckets) == 0 {
		return 0
	}
	return float64(buckets[0]) / 4
}

func averageRange(points []market.MinutePoint) float64 {
	if len(points) == 0 {
		return 0
	}
	var total float64
	for _, point := range points {
		total += point.High - point.Low
	}
	return total / float64(len(points))
}

func classify(points []market.MinutePoint, last, vwap, buy, sell, acceleration float64) (string, float64, []string) {
	score := 0.0
	var reasons []string
	if last > vwap {
		score += 0.20
		reasons = append(reasons, "Price is above VWAP")
	} else {
		score -= 0.20
		reasons = append(reasons, "Price is below VWAP")
	}
	if momentum := (last - points[len(points)-6].Close) / points[len(points)-6].Close; momentum > 0.0003 {
		score += 0.25
		reasons = append(reasons, "5-minute momentum is positive")
	} else if momentum < -0.0003 {
		score -= 0.25
		reasons = append(reasons, "5-minute momentum is negative")
	}
	deltaRatio := (buy - sell) / max(buy+sell, 1)
	score += 0.35 * deltaRatio
	if deltaRatio > 0.10 {
		reasons = append(reasons, "Buy-side volume delta is positive")
	} else if deltaRatio < -0.10 {
		reasons = append(reasons, "Sell-side volume delta is negative")
	}
	if acceleration > 1.15 {
		score += 0.15
		reasons = append(reasons, "Volume is accelerating")
	}
	rsi := rsi(points, 14)
	if rsi >= 60 {
		score += 0.15
		reasons = append(reasons, "RSI confirms bullish momentum")
	} else if rsi <= 40 {
		score -= 0.15
		reasons = append(reasons, "RSI confirms bearish momentum")
	}
	signal := "HOLD"
	if score >= 0.35 {
		signal = "BUY"
	} else if score <= -0.35 {
		signal = "SELL"
	}
	confidence := clamp(50+math.Abs(score)*80, 0, 95)
	return signal, confidence, reasons
}

func rsi(points []market.MinutePoint, period int) float64 {
	if len(points) <= period {
		return 50
	}
	var gains, losses float64
	start := len(points) - period - 1
	for i := start + 1; i < len(points); i++ {
		change := points[i].Close - points[i-1].Close
		if change >= 0 {
			gains += change
		} else {
			losses -= change
		}
	}
	if losses == 0 {
		return 100
	}
	return 100 - 100/(1+gains/max(losses, 0.000001))
}

func walkForwardAccuracy(points []market.MinutePoint) float64 {
	var correct, total int
	for i := 40; i+10 < len(points); i++ {
		history := points[:i+1]
		if len(history) < 20 {
			continue
		}
		signal := historicalSignal(history)
		if signal == "HOLD" {
			continue
		}
		change := (points[i+10].Close - points[i].Close) / points[i].Close
		if (signal == "BUY" && change > 0) || (signal == "SELL" && change < 0) {
			correct++
		}
		total++
	}
	if total == 0 {
		return 50
	}
	return float64(correct) / float64(total) * 100
}

func calculateIndicatorAccuracy(points []market.MinutePoint) map[string]float64 {
	names := []string{"VWAP", "EMA20", "MACD", "RSI14", "ROC10", "Volume"}
	accuracy := make(map[string]float64, len(names))
	for _, name := range names {
		correct, total := 0, 0
		for i := 40; i+10 < len(points); i++ {
			window := points[:i+1]
			last := window[len(window)-1].Close
			nextMove := points[i+10].Close - last
			vwap := volumeWeightedAverage(window)
			indicators := calculateIndicators(window, vwap)
			predicted := 0.0
			switch name {
			case "VWAP":
				predicted = last - vwap
			case "EMA20":
				predicted = last - indicators.EMA20
			case "MACD":
				predicted = indicators.MACD - indicators.SignalLine
			case "RSI14":
				predicted = indicators.RSI14 - 50
			case "ROC10":
				predicted = indicators.ROC10
			case "Volume":
				buy, sell := estimatedFlow(recentPoints(window, 20))
				predicted = buy - sell
			}
			if predicted == 0 {
				continue
			}
			total++
			if (predicted > 0 && nextMove > 0) || (predicted < 0 && nextMove < 0) {
				correct++
			}
		}
		if total == 0 {
			accuracy[name] = 50
		} else {
			accuracy[name] = float64(correct) / float64(total) * 100
		}
	}
	return accuracy
}

func averageIndicatorAccuracy(accuracy map[string]float64) float64 {
	if len(accuracy) == 0 {
		return 50
	}
	total := 0.0
	for _, value := range accuracy {
		total += value
	}
	return total / float64(len(accuracy))
}

func volumeConfirmed(buy, sell, acceleration float64) bool {
	flowShare := math.Abs(buy-sell) / max(buy+sell, 1)
	return acceleration >= 0.90 && flowShare >= 0.05
}

func historicalSignal(points []market.MinutePoint) string {
	if len(points) < 20 {
		return "HOLD"
	}
	last := points[len(points)-1].Close
	window := recentPoints(points, 20)
	vwap := volumeWeightedAverage(window)
	buy, sell := estimatedFlow(window)
	score := 0.0
	if last > vwap {
		score += 0.20
	} else {
		score -= 0.20
	}
	momentum := (last - points[len(points)-6].Close) / points[len(points)-6].Close
	score += clamp(momentum*100, -0.25, 0.25)
	score += 0.35 * (buy - sell) / max(buy+sell, 1)
	rsi14 := rsi(points, 14)
	if rsi14 >= 60 {
		score += 0.15
	} else if rsi14 <= 40 {
		score -= 0.15
	}
	if score >= 0.35 {
		return "BUY"
	}
	if score <= -0.35 {
		return "SELL"
	}
	return "HOLD"
}

func validatedTarget(points []market.MinutePoint, signal string, last, rangeProxy float64) (target, low, high float64, hitRate float64, samples int) {
	if signal == "HOLD" {
		return last, last - rangeProxy/2, last + rangeProxy/2, 0, 0
	}
	atr14 := atr(points, 14)
	if atr14 <= 0 {
		atr14 = rangeProxy
	}
	bestRate, bestSamples := -1.0, 0
	bestTarget := last
	for _, multiple := range []float64{0.25, 0.50, 0.75, 1.0, 1.25} {
		distance := atr14 * multiple
		hits, total := 0, 0
		for i := 40; i+10 < len(points); i++ {
			history := points[:i+1]
			historical := historicalSignal(history)
			if historical != signal {
				continue
			}
			candidate := history[len(history)-1].Close
			total++
			if signal == "BUY" && candleHigh(points[i+1:i+11]) >= candidate+distance {
				hits++
			}
			if signal == "SELL" && candleLow(points[i+1:i+11]) <= candidate-distance {
				hits++
			}
		}
		if total == 0 {
			continue
		}
		rate := float64(hits) / float64(total) * 100
		if total >= 5 && rate > bestRate {
			bestRate, bestSamples = rate, total
			if signal == "BUY" {
				bestTarget = last + distance
			} else {
				bestTarget = last - distance
			}
		}
	}
	if bestRate < 0 {
		distance := atr14 * 0.5
		bestRate = 0
		if signal == "BUY" {
			bestTarget = last + distance
		} else {
			bestTarget = last - distance
		}
	}
	halfBand := math.Max(atr14*0.25, rangeProxy/2)
	return bestTarget, bestTarget - halfBand, bestTarget + halfBand, bestRate, bestSamples
}

func candleHigh(points []market.MinutePoint) float64 {
	if len(points) == 0 {
		return 0
	}
	high := points[0].High
	for _, point := range points[1:] {
		high = math.Max(high, point.High)
	}
	return high
}

func candleLow(points []market.MinutePoint) float64 {
	if len(points) == 0 {
		return math.Inf(1)
	}
	low := points[0].Low
	for _, point := range points[1:] {
		low = math.Min(low, point.Low)
	}
	return low
}

func calculateIndicators(points []market.MinutePoint, vwap float64) IndicatorSet {
	last := points[len(points)-1]
	sma := sma(points, 20)
	ema20Value := ema(points, 20)
	ema12 := ema(points, 12)
	ema26 := ema(points, 26)
	macd := ema12 - ema26
	upper, lower := bollinger(points, 20)
	high, lowestLow := lowest(points, 14, true)
	k := 50.0
	if high > lowestLow {
		k = (last.Close - lowestLow) / (high - lowestLow) * 100
	}
	d := k
	if len(points) >= 3 {
		d = (stochastic(points, 14) + stochastic(recentPoints(points, len(points)-1), 14) + k) / 3
	}
	var obv float64
	for i := 1; i < len(points); i++ {
		if points[i].Close > points[i-1].Close {
			obv += points[i].Volume
		} else if points[i].Close < points[i-1].Close {
			obv -= points[i].Volume
		}
	}
	roc := 0.0
	if len(points) > 10 && points[len(points)-11].Close != 0 {
		roc = (last.Close/points[len(points)-11].Close - 1) * 100
	}
	return IndicatorSet{
		SMA20: sma, EMA20: ema20Value, RSI14: rsi(points, 14), RSI2: rsi(points, 2), MACD: macd,
		SignalLine: macd * 0.9, BollingerUpper: upper, BollingerLower: lower, ATR14: atr(points, 14),
		ADX14: adx(points, 14), StochasticK: k, StochasticD: d, ROC10: roc, OBV: obv,
		VWAPDistance: (last.Close - vwap) / vwap * 100,
	}
}

func sma(points []market.MinutePoint, period int) float64 {
	if len(points) == 0 {
		return 0
	}
	start := maxInt(0, len(points)-period)
	var total float64
	for _, point := range points[start:] {
		total += point.Close
	}
	return total / float64(len(points)-start)
}

func ema(points []market.MinutePoint, period int) float64 {
	if len(points) == 0 {
		return 0
	}
	value := points[0].Close
	multiplier := 2 / float64(period+1)
	for _, point := range points[1:] {
		value = (point.Close-value)*multiplier + value
	}
	return value
}

func bollinger(points []market.MinutePoint, period int) (float64, float64) {
	start := maxInt(0, len(points)-period)
	mean := sma(points, period)
	var variance float64
	for _, point := range points[start:] {
		variance += (point.Close - mean) * (point.Close - mean)
	}
	count := float64(len(points) - start)
	deviation := math.Sqrt(variance / max(count, 1))
	return mean + 2*deviation, mean - 2*deviation
}

func lowest(points []market.MinutePoint, period int, _ bool) (float64, float64) {
	start := maxInt(0, len(points)-period)
	high, low := points[start].High, points[start].Low
	for _, point := range points[start:] {
		if point.High > high {
			high = point.High
		}
		if point.Low < low {
			low = point.Low
		}
	}
	return high, low
}

func stochastic(points []market.MinutePoint, period int) float64 {
	start := maxInt(0, len(points)-period)
	high := points[start].High
	low := points[start].Low
	for _, point := range points[start:] {
		if point.High > high {
			high = point.High
		}
		if point.Low < low {
			low = point.Low
		}
	}
	if high == low {
		return 50
	}
	return (points[len(points)-1].Close - low) / (high - low) * 100
}

func atr(points []market.MinutePoint, period int) float64 {
	start := maxInt(1, len(points)-period)
	var total float64
	for i := start; i < len(points); i++ {
		tr := math.Max(points[i].High-points[i].Low,
			math.Max(math.Abs(points[i].High-points[i-1].Close), math.Abs(points[i].Low-points[i-1].Close)))
		total += tr
	}
	return total / float64(maxInt(1, len(points)-start))
}

func adx(points []market.MinutePoint, period int) float64 {
	if len(points) < period+1 {
		return 50
	}
	var positive, negative float64
	for i := len(points) - period; i < len(points); i++ {
		up := points[i].High - points[i-1].High
		down := points[i-1].Low - points[i].Low
		if up > down && up > 0 {
			positive += up
		}
		if down > up && down > 0 {
			negative += down
		}
	}
	sum := positive + negative
	if sum == 0 {
		return 50
	}
	return 100 * math.Abs(positive-negative) / sum
}

func indicatorSignals(points []market.MinutePoint, i IndicatorSet, buy, sell float64) []IndicatorSignal {
	closing := points[len(points)-1].Close
	makeSignal := func(name string, value float64, signal string, strength float64) IndicatorSignal {
		return IndicatorSignal{Name: name, Value: value, Signal: signal, Strength: clamp(strength, 0, 100)}
	}
	rsiSignal := "HOLD"
	if i.RSI14 < 30 {
		rsiSignal = "BUY"
	} else if i.RSI14 > 70 {
		rsiSignal = "SELL"
	}
	macdSignal := "BUY"
	if i.MACD < i.SignalLine {
		macdSignal = "SELL"
	}
	stochSignal := "HOLD"
	if i.StochasticK < 20 {
		stochSignal = "BUY"
	} else if i.StochasticK > 80 {
		stochSignal = "SELL"
	}
	trend := "HOLD"
	if closing > i.EMA20 {
		trend = "BUY"
	} else if closing < i.EMA20 {
		trend = "SELL"
	}
	volume := "HOLD"
	if buy > sell {
		volume = "BUY"
	} else if sell > buy {
		volume = "SELL"
	}
	band := "HOLD"
	if closing < i.BollingerLower {
		band = "BUY"
	} else if closing > i.BollingerUpper {
		band = "SELL"
	}
	return []IndicatorSignal{
		makeSignal("RSI14", i.RSI14, rsiSignal, math.Abs(50-i.RSI14)*2),
		makeSignal("MACD", i.MACD, macdSignal, math.Min(math.Abs(i.MACD)*20, 100)),
		makeSignal("Stochastic 14", i.StochasticK, stochSignal, math.Abs(50-i.StochasticK)*2),
		makeSignal("EMA20 trend", closing-i.EMA20, trend, math.Abs(closing-i.EMA20)/closing*1000),
		makeSignal("Volume delta", buy-sell, volume, math.Abs(buy-sell)/max(buy+sell, 1)*100),
		makeSignal("Bollinger Bands", closing, band, math.Abs(closing-i.SMA20)/max(i.BollingerUpper-i.SMA20, 0.01)*50),
		makeSignal("ROC10", i.ROC10, ternary(i.ROC10 > 0, "BUY", "SELL"), math.Min(math.Abs(i.ROC10)*25, 100)),
		makeSignal("ADX14", i.ADX14, ternary(i.ADX14 >= 25, trend, "HOLD"), i.ADX14),
	}
}

func ternary(condition bool, positive, negative string) string {
	if condition {
		return positive
	}
	return negative
}

func max(values ...float64) float64 {
	if len(values) == 0 {
		return 0
	}
	value := values[0]
	for _, candidate := range values[1:] {
		if candidate > value {
			value = candidate
		}
	}
	return value
}

func clamp(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

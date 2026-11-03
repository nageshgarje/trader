package analysis

import (
	"math"

	"github.com/nageshgarje/trader/internal/market"
)

type VolumeTargetInput struct {
	Instrument string `json:"instrument"`
	Source     string `json:"source"`
}

type VolumeTargetAnalysis struct {
	Instrument          string  `json:"instrument"`
	Status              string  `json:"status"`
	Source              string  `json:"source"`
	VolumeAvailable     bool    `json:"volumeAvailable"`
	RelativeVolume      float64 `json:"relativeVolume"`
	VolumeAcceleration  float64 `json:"volumeAcceleration"`
	OBV                 float64 `json:"obv"`
	Accumulation        float64 `json:"accumulationDistribution"`
	VWAP                float64 `json:"vwap"`
	POC                 float64 `json:"poc"`
	ValueAreaHigh       float64 `json:"valueAreaHigh"`
	ValueAreaLow        float64 `json:"valueAreaLow"`
	BreakoutVolumeScore float64 `json:"breakoutVolumeScore"`
	TargetConfirmed     bool    `json:"targetConfirmed"`
	Reason              string  `json:"reason"`
}

func AnalyzeVolumeTargets(points []market.MinutePoint, instrument string) VolumeTargetAnalysis {
	result := VolumeTargetAnalysis{
		Instrument: instrument,
		Source:     "Unavailable",
		Status:     "NO TRADE",
	}
	available := instrument != "" && instrument != "NIFTY" &&
		instrument != "NIFTY50" && instrument != "^NSEI"
	total := sumVolume(points)
	if total <= 0 || len(points) < 20 {
		result.Reason = "No reliable traded volume for this instrument"
		return result
	}

	// NIFTY index volume must never be treated as traded instrument volume.
	if instrument == "NIFTY" || instrument == "NIFTY50" || instrument == "^NSEI" {
		result.Reason = "NIFTY spot volume is not available; use NIFTY futures or breadth from a reliable source"
		return result
	}
	available = true
	result.VolumeAvailable = available
	result.Source = "instrument OHLCV"
	result.VWAP = volumeWeightedAverage(points)
	result.POC = pointOfControl(recentPoints(points, 20))
	result.RelativeVolume = relativeVolume(points)
	result.VolumeAcceleration = volumeAcceleration(points)
	result.OBV = onBalanceVolume(points)
	result.Accumulation = accumulationDistribution(points)
	result.ValueAreaHigh, result.ValueAreaLow = valueArea(points)

	recent := recentPoints(points, 5)
	breakoutVolume := result.RelativeVolume >= 1.20 && result.VolumeAcceleration >= 1.10
	directionalConfirmation := false
	if len(recent) > 0 {
		change := recent[len(recent)-1].Close - recent[0].Close
		directionalConfirmation = math.Abs(change) > 0 &&
			(change > 0 && result.OBV > 0 || change < 0 && result.OBV < 0)
	}
	result.BreakoutVolumeScore = normalizedVolumeScore(result.RelativeVolume, result.VolumeAcceleration)
	result.TargetConfirmed = breakoutVolume && directionalConfirmation
	if result.TargetConfirmed {
		result.Status = "VALID"
		result.Reason = "Breakout volume, acceleration, and directional flow confirm target room"
	} else {
		result.Reason = "Volume does not confirm a breakout target"
	}
	return result
}

func relativeVolume(points []market.MinutePoint) float64 {
	if len(points) < 21 {
		return 0
	}
	recent := sumVolume(recentPoints(points, 5))
	baseline := sumVolume(recentPoints(points, 20))
	baseline -= recent
	if baseline <= 0 {
		return 0
	}
	return (recent / 5) / (baseline / 15)
}

func volumeAcceleration(points []market.MinutePoint) float64 {
	if len(points) < 40 {
		return 0
	}
	recent := sumVolume(recentPoints(points, 20))
	previous := sumVolume(points[len(points)-40 : len(points)-20])
	if previous <= 0 {
		return 0
	}
	return recent / previous
}

func onBalanceVolume(points []market.MinutePoint) float64 {
	var obv float64
	for i := 1; i < len(points); i++ {
		if points[i].Close > points[i-1].Close {
			obv += points[i].Volume
		} else if points[i].Close < points[i-1].Close {
			obv -= points[i].Volume
		}
	}
	return obv
}

func accumulationDistribution(points []market.MinutePoint) float64 {
	var ad float64
	for _, point := range points {
		rangeValue := point.High - point.Low
		if rangeValue <= 0 {
			continue
		}
		moneyFlowMultiplier := ((point.Close - point.Low) - (point.High - point.Close)) / rangeValue
		ad += moneyFlowMultiplier * point.Volume
	}
	return ad
}

func valueArea(points []market.MinutePoint) (high, low float64) {
	profile := make(map[int64]float64)
	for _, point := range points {
		profile[int64(point.Close/5)*5] += point.Volume
	}
	if len(profile) == 0 {
		return 0, 0
	}
	var center int64
	var mostVolume float64
	for price, volume := range profile {
		if volume > mostVolume {
			mostVolume = volume
			center = price
		}
	}
	high = float64(center + 25)
	low = float64(center - 25)
	return
}

func normalizedVolumeScore(relative, acceleration float64) float64 {
	return clamp((relative/2+acceleration/2)*50, 0, 100)
}

package analysis

import (
	"testing"

	"github.com/nageshgarje/trader/internal/market"
)

func TestNiftySpotVolumeIsNotInvented(t *testing.T) {
	points := makeVolumeTestPoints()
	analysis := AnalyzeVolumeTargets(points, "NIFTY")
	if analysis.VolumeAvailable {
		t.Fatal("NIFTY spot volume marked available")
	}
	if analysis.Source != "Unavailable" {
		t.Fatalf("source = %q, want unavailable", analysis.Source)
	}
}

func TestVolumeConfirmsDirectionalBreakout(t *testing.T) {
	points := makeVolumeTestPoints()
	for i := len(points) - 5; i < len(points); i++ {
		points[i].Volume *= 2
		points[i].Close += float64(i-len(points)+6) * 2
		points[i].High = points[i].Close + 2
		points[i].Low = points[i].Close - 2
	}
	analysis := AnalyzeVolumeTargets(points, "NIFTY FUT")
	if !analysis.VolumeAvailable || analysis.RelativeVolume <= 0 {
		t.Fatalf("unexpected volume result: %#v", analysis)
	}
}

func TestDryVolumeTargetRun(t *testing.T) {
	points := makeVolumeTestPoints()
	for i := range points {
		points[i].Close = 24500 + float64(i)*0.5
		points[i].High = points[i].Close + 3
		points[i].Low = points[i].Close - 3
		points[i].Volume = 10000 + float64(i%7)*1000
	}
	analysis := AnalyzeVolumeTargets(points, "NIFTY FUT")
	if analysis.Status != "VALID" && analysis.Status != "NO TRADE" {
		t.Fatalf("invalid dry-run status: %q", analysis.Status)
	}
}

func makeVolumeTestPoints() []market.MinutePoint {
	points := make([]market.MinutePoint, 50)
	for i := range points {
		price := 24500 + float64(i%10)*2
		points[i] = market.MinutePoint{
			Time: int64(i * 60), Open: price, High: price + 4, Low: price - 4,
			Close: price + 1, Volume: 10000 + float64(i%20)*100,
		}
	}
	return points
}

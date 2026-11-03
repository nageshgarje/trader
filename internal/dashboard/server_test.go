package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nageshgarje/trader/internal/analysis"
)

func TestProductionMiddlewareAddsSecurityHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	response := httptest.NewRecorder()
	handler := withProductionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(response, request)

	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := response.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
}

func TestNiftyTargetDistanceHasMinimum(t *testing.T) {
	result := analysis.Result{
		Last: 23300, VWAP: 23280, Target10m: 23310,
		RangeProxy: 4,
		Indicators: analysis.IndicatorSet{ATR14: 8},
	}
	if distance := niftyTargetDistance(result); distance < 50 {
		t.Fatalf("target distance = %.2f, want at least 50", distance)
	}
}

func TestNiftyDirectionConsensus(t *testing.T) {
	result := analysis.Result{
		Last: 23300, VWAP: 23290, Target10m: 23320,
		Indicators: analysis.IndicatorSet{
			EMA20: 23285, MACD: 1, SignalLine: 0.5, RSI14: 60, ROC10: 0.2,
		},
	}
	bullish, bearish := niftyDirectionVotes(result)
	if bullish <= bearish {
		t.Fatalf("expected bullish consensus, got bullish=%d bearish=%d", bullish, bearish)
	}
}

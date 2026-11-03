package analysis

import "testing"

func TestOptionsBacktestRequiresHistoricalContracts(t *testing.T) {
	report, err := RunOptionsBacktest(nil, BacktestResult{})
	if err == nil {
		t.Fatal("expected missing historical options data error")
	}
	if report.Status != "not_run" || report.CanRun || report.DataComplete {
		t.Fatalf("unexpected report state: %#v", report)
	}
	if len(report.Limitations) == 0 {
		t.Fatal("missing data limitations were not reported")
	}
}

package analysis

import (
	"testing"
	"time"
)

func TestPerformanceReportDoesNotInventMissingExecutions(t *testing.T) {
	report := BuildPerformanceReport(12, nil)
	if report.Status != "insufficient_data" {
		t.Fatalf("status = %q, want insufficient_data", report.Status)
	}
	if report.DataComplete {
		t.Fatal("missing trades marked complete")
	}
	if report.TotalExecutedTrades != 0 {
		t.Fatalf("executed trades = %d, want 0", report.TotalExecutedTrades)
	}
	if report.DataLimitation == "" {
		t.Fatal("missing-data limitation was not reported")
	}
}

func TestPerformanceReportCalculatesOptionResults(t *testing.T) {
	now := time.Now()
	trades := []ExecutedTrade{
		{ID: "1", OptionType: "CE", EntryPremium: 120, ExitPremium: 165, Costs: 8, EntryTime: now, ExitTime: now.Add(2 * time.Hour), Status: "completed", Expiry: "weekly", Strike: 24000, IVRegime: "low", TrendRegime: "bullish", UnderlyingMove: 35},
		{ID: "2", OptionType: "PE", EntryPremium: 100, ExitPremium: 75, Costs: 8, EntryTime: now, ExitTime: now.Add(time.Hour), Status: "completed", Expiry: "monthly", Strike: 23900, IVRegime: "high", TrendRegime: "bearish", UnderlyingMove: -28},
	}
	report := BuildPerformanceReport(4, trades)
	if report.TotalExecutedTrades != 2 {
		t.Fatalf("executed trades = %d, want 2", report.TotalExecutedTrades)
	}
	if report.GrossProfit != 37 || report.GrossLoss != 33 {
		t.Fatalf("gross profit/loss = %.2f/%.2f, want 37/33", report.GrossProfit, report.GrossLoss)
	}
	if report.NetProfitAfterCosts != 4 {
		t.Fatalf("net profit = %.2f, want 4", report.NetProfitAfterCosts)
	}
	if report.WinRate != 50 {
		t.Fatalf("win rate = %.2f, want 50", report.WinRate)
	}
}

package analysis

import (
	"errors"
	"time"
)

type HistoricalOptionContract struct {
	Symbol           string    `json:"symbol"`
	Underlying       string    `json:"underlying"`
	Strike           float64   `json:"strike"`
	OptionType       string    `json:"optionType"`
	Expiry           time.Time `json:"expiry"`
	Timestamp        time.Time `json:"timestamp"`
	Bid              float64   `json:"bid"`
	Ask              float64   `json:"ask"`
	Last             float64   `json:"last"`
	Open             float64   `json:"open"`
	High             float64   `json:"high"`
	Low              float64   `json:"low"`
	Close            float64   `json:"close"`
	Volume           float64   `json:"volume"`
	OpenInterest     float64   `json:"openInterest"`
	IV               float64   `json:"iv"`
	ExecutionQuality string    `json:"executionQuality"`
}

type OptionsBacktestReport struct {
	Status       string         `json:"status"`
	AsOf         time.Time      `json:"asOf"`
	DataComplete bool           `json:"dataComplete"`
	CanRun       bool           `json:"canRun"`
	Results      BacktestResult `json:"results"`
	Limitations  []string       `json:"limitations"`
}

var ErrHistoricalOptionsUnavailable = errors.New("historical option contract data unavailable")

func RunOptionsBacktest(_ []HistoricalOptionContract, underlying BacktestResult) (OptionsBacktestReport, error) {
	report := OptionsBacktestReport{
		Status: "not_run",
		AsOf:   time.Now(),
		Limitations: []string{
			"NIFTY index OHLC data does not provide executable option premiums.",
			"CE/PE profitability and strategy comparisons require timestamped contract bid, ask, volume, open interest, expiry, strike, and IV data.",
			"The repository must not substitute modeled premium values for historical option execution.",
			"Underlying-only backtest results are separate from option profitability.",
		},
	}
	if underlying.Trades >= 0 {
		report.Results = underlying
	}
	return report, ErrHistoricalOptionsUnavailable
}

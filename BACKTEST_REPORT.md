# Strategy Backtest Report

## Validation status

A production performance claim cannot be made from the current repository.

The repository contains live prediction ledgers and daily target ledgers, but no clean
chronological historical OHLCV dataset suitable for walk-forward, out-of-sample,
cost-inclusive simulation. The existing ledgers do not provide reliable multi-day
OHLCV candles, volume quality, or chronological train/validation/test partitions.

The market provider currently supplies only the live Yahoo intraday window configured
by the application. Therefore Yahoo live candles were not treated as historical
backtest evidence, and no win rate or profitability was invented.

## Executable validation

The Go backtest harness is implemented in
`internal/analysis/backtest.go`. Its contract test in
`internal/analysis/backtest_test.go` executed successfully as part of
`go test ./...`. That test validates numerical stability and transaction-cost
simulation mechanics with synthetic candles; it is not evidence of trading edge.

The harness requires:

- chronologically ordered OHLCV;
- spread, slippage, and brokerage costs;
- capital constraints;
- target and stop-loss execution;
- a minimum 20-point opportunity filter.

## Required evidence before production activation

Supply a reproducible historical dataset containing timestamp, open, high, low,
close, and volume for each instrument/timeframe, plus a documented market calendar
and corporate-action policy. Then run chronological train, validation, and unseen
test partitions, compare the existing, EMA/VWAP, Supertrend, market-structure,
multi-timeframe, and weighted-score strategies, and publish the resulting metrics
without parameter selection on the final test partition.

package market

import (
	"math"
	"sort"
	"time"
)

const invSqrt2Pi = 0.3989422804014327

func calculateOptionChain(underlying float64, expiry string, rows []nseStrike) OptionChain {
	expiryTime, _ := time.Parse("02-Jan-2006", expiry)
	years := timeUntilExpiry(expiryTime)
	chain := OptionChain{
		Exchange: "NSE", Symbol: "NIFTY", Underlying: underlying,
		Expiry: expiry, ExpiresAt: expiryTime, UpdatedAt: time.Now(),
	}
	var supportCand []float64
	var resistanceCand []float64
	for _, row := range rows {
		if row.ExpiryDate != expiry {
			continue
		}
		callIV := row.CALL.ImpliedVolatility / 100
		putIV := row.PUT.ImpliedVolatility / 100
		callDelta, callGamma, callTheta := blackScholesGreeks(underlying, row.Strike, years, callIV, true, row.CALL.LastPrice)
		putDelta, putGamma, putTheta := blackScholesGreeks(underlying, row.Strike, years, putIV, false, row.PUT.LastPrice)
		callOI, putOI := row.CALL.OpenInterest, row.PUT.OpenInterest
		chain.CallOI += callOI
		chain.PutOI += putOI
		chain.CallVolume += row.CALL.TotalTraded
		chain.PutVolume += row.PUT.TotalTraded
		chain.CallDelta += callDelta * callOI
		chain.PutDelta += putDelta * putOI
		chain.CallGamma += callGamma * callOI
		chain.PutGamma += putGamma * putOI
		chain.CallTheta += callTheta * callOI
		chain.PutTheta += putTheta * putOI
		chain.Strikes = append(chain.Strikes, OptionStrike{
			Strike: row.Strike, Expiry: row.ExpiryDate,
			CallIV: callIV, PutIV: putIV, CallOI: callOI, PutOI: putOI,
			CallVolume: row.CALL.TotalTraded, PutVolume: row.PUT.TotalTraded,
			CallDelta: callDelta, PutDelta: putDelta, CallGamma: callGamma, PutGamma: putGamma,
			CallTheta: callTheta, PutTheta: putTheta, CallIVBidAsk: row.CALL.LastPrice, PutIVBidAsk: row.PUT.LastPrice,
		})
		if row.Strike <= underlying && putOI > 0 {
			supportCand = append(supportCand, row.Strike)
		}
		if row.Strike >= underlying && callOI > 0 {
			resistanceCand = append(resistanceCand, row.Strike)
		}
	}
	sort.Slice(chain.Strikes, func(i, j int) bool { return chain.Strikes[i].Strike < chain.Strikes[j].Strike })
	if len(supportCand) > 0 {
		chain.Support = maxStrike(supportCand, func(strike float64) float64 { return oiAt(chain.Strikes, strike, false) })
	}
	if len(resistanceCand) > 0 {
		chain.Resistance = maxStrike(resistanceCand, func(strike float64) float64 { return oiAt(chain.Strikes, strike, true) })
	}
	chain.MaxPain = calculateMaxPain(chain.Strikes)
	chain.PCR = divide(chain.PutOI, chain.CallOI)
	chain.PCRVolume = divide(chain.PutVolume, chain.CallVolume)
	chain.NetDelta = chain.PutDelta + chain.CallDelta
	chain.NetGamma = chain.CallGamma + chain.PutGamma
	chain.NetTheta = chain.CallTheta + chain.PutTheta
	applyOptionBias(&chain)
	return chain
}

func blackScholesGreeks(spot, strike, years, volatility float64, call bool, marketPrice float64) (delta, gamma, theta float64) {
	if years <= 0 {
		return 0, 0, 0
	}
	volatility = impliedVolatility(spot, strike, years, volatility, call, marketPrice)
	if volatility <= 0.0001 {
		return 0, 0, 0
	}
	d1 := (math.Log(spot/strike) + 0.5*volatility*volatility*years) / (volatility * math.Sqrt(years))
	d2 := d1 - volatility*math.Sqrt(years)
	gamma = invSqrt2Pi * math.Exp(-0.5*d1*d1) / (spot * volatility * math.Sqrt(years))
	if call {
		delta = normalCDF(d1)
		theta = -(spot*invSqrt2Pi*math.Exp(-0.5*d1*d1)*volatility)/(2*math.Sqrt(years)) + 0.01*strike*math.Exp(-0.01*years)*normalCDF(-d2)
	} else {
		delta = normalCDF(d1) - 1
		theta = -(spot*invSqrt2Pi*math.Exp(-0.5*d1*d1)*volatility)/(2*math.Sqrt(years)) - 0.01*strike*math.Exp(-0.01*years)*normalCDF(d2)
	}
	return delta, gamma, theta / 365
}

func impliedVolatility(spot, strike, years, fallback float64, call bool, marketPrice float64) float64 {
	if marketPrice <= 0 || spot <= 0 || strike <= 0 {
		return fallback
	}
	low, high := 0.0001, 5.0
	for i := 0; i < 40; i++ {
		mid := (low + high) / 2
		price := blackScholesPrice(spot, strike, years, mid, call)
		if math.Abs(price-marketPrice) < 0.01 {
			return mid
		}
		if price > marketPrice {
			high = mid
		} else {
			low = mid
		}
	}
	return (low + high) / 2
}

func blackScholesPrice(spot, strike, years, volatility float64, call bool) float64 {
	if years <= 0 {
		if call {
			return math.Max(spot-strike, 0)
		}
		return math.Max(strike-spot, 0)
	}
	d1 := (math.Log(spot/strike) + 0.5*volatility*volatility*years) / (volatility * math.Sqrt(years))
	d2 := d1 - volatility*math.Sqrt(years)
	if call {
		return spot*normalCDF(d1) - strike*math.Exp(-0.01*years)*normalCDF(d2)
	}
	return strike*math.Exp(-0.01*years)*normalCDF(-d2) - spot*normalCDF(-d1)
}

func normalCDF(value float64) float64 {
	return 0.5 * math.Erfc(-value/math.Sqrt2)
}

func timeUntilExpiry(expiry time.Time) float64 {
	if expiry.IsZero() {
		return 1.0 / 365
	}
	now := time.Now()
	if expiry.Before(now) {
		return 1.0 / 365
	}
	return expiry.Sub(now).Hours() / 24 / 365
}

func applyOptionBias(chain *OptionChain) {
	score := 0.0
	reason := "Balanced derivatives positioning"
	if chain.PCR >= 1.15 {
		score += 1
		reason = "Put-call OI ratio is bullish"
	} else if chain.PCR <= 0.70 {
		score -= 1
		reason = "Call-heavy positioning is bearish"
	}
	deltaScale := divide(math.Abs(chain.NetDelta), maxFloat64(chain.CallOI, chain.PutOI))
	if deltaScale < 1 {
		if chain.NetDelta > 0 {
			score += 0.35
		} else if chain.NetDelta < 0 {
			score -= 0.35
		}
	}
	if chain.NetGamma > 0 && chain.Underlying > chain.MaxPain {
		score += 0.15
	} else if chain.NetGamma < 0 && chain.Underlying < chain.MaxPain {
		score -= 0.15
	}
	if chain.Resistance > chain.Underlying {
		if chain.Resistance-chain.Underlying < chain.Underlying*0.015 {
			reason = "Near resistance call wall"
		}
	}
	switch {
	case score >= 0.5:
		chain.Bias, chain.Confidence = "BUY", clampPercent(55+score*12)
	case score <= -0.5:
		chain.Bias, chain.Confidence = "SELL", clampPercent(55+math.Abs(score)*12)
	default:
		chain.Bias, chain.Confidence = "HOLD", clampPercent(50+math.Abs(score)*8)
	}
	chain.Reason = reason
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 95 {
		return 95
	}
	return value
}

func divide(a, b float64) float64 {
	if b <= 0 {
		return 0
	}
	return a / b
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func oiAt(strikes []OptionStrike, strike float64, calls bool) float64 {
	for _, item := range strikes {
		if item.Strike == strike {
			if calls {
				return item.CallOI
			}
			return item.PutOI
		}
	}
	return 0
}

func maxStrike(strikes []float64, weight func(float64) float64) float64 {
	best, bestWeight := 0.0, -1.0
	for _, strike := range strikes {
		value := weight(strike)
		if value > bestWeight {
			best, bestWeight = strike, value
		}
	}
	return best
}

func calculateMaxPain(strikes []OptionStrike) float64 {
	if len(strikes) == 0 {
		return 0
	}
	bestStrike, bestPain := 0.0, math.MaxFloat64
	for _, candidate := range strikes {
		pain := 0.0
		for _, strike := range strikes {
			if candidate.Strike > strike.Strike {
				pain += strike.PutOI * (candidate.Strike - strike.Strike)
			}
			if candidate.Strike < strike.Strike {
				pain += strike.CallOI * (strike.Strike - candidate.Strike)
			}
		}
		if pain < bestPain {
			bestStrike, bestPain = candidate.Strike, pain
		}
	}
	return bestStrike
}

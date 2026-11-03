package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nageshgarje/trader/internal/analysis"
	"github.com/nageshgarje/trader/internal/market"
)

const (
	DefaultHTMLPath                        = "web/dist/index.html"
	DefaultRelianceSymbol                  = "RELIANCE"
	DefaultNiftySymbol                     = "NIFTY"
	DefaultNSEDashboardHTMLPath            = "web/dist/nse-dashboard.html"
	DefaultAddress                         = "127.0.0.1:8080"
	DefaultNiftyAddress                    = "127.0.0.1:8081"
	DefaultNSEDashboardAddress             = "127.0.0.1:8082"
	AddressEnvironmentVariable             = "MARKET_DASHBOARD_ADDR"
	NiftyAddressEnvironmentVariable        = "NIFTY_DASHBOARD_ADDR"
	NSEDashboardAddressEnvironmentVariable = "NSE_DASHBOARD_ADDR"
	NSEMCPURLEnvironmentVariable           = "NSE_MCP_URL"
)

type Assets struct {
	HTMLPath string
	Symbol   string
}

func handleNSEMCPTools(w http.ResponseWriter, r *http.Request) {
	result, err := callNSEJSONRPC(nseMCPURL(), "tools/list", nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "NSE MCP server unavailable: "+safeMCPError(err))
		return
	}
	writeJSON(w, result)
}

func handleNSEMCPCall(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil || strings.TrimSpace(request.Name) == "" {
		writeError(w, http.StatusBadRequest, "name and arguments are required")
		return
	}
	result, err := callNSEJSONRPC(nseMCPURL(), "tools/call", map[string]any{
		"name": request.Name, "arguments": request.Arguments,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "NSE MCP tool call failed: "+safeMCPError(err))
		return
	}
	writeJSON(w, result)
}

func nseMCPURL() string {
	if endpoint := strings.TrimSpace(os.Getenv(NSEMCPURLEnvironmentVariable)); endpoint != "" {
		return endpoint
	}
	return "http://localhost:8000/jsonrpc"
}

func callNSEJSONRPC(endpoint, method string, params map[string]any) (map[string]any, error) {
	payload := map[string]any{"jsonrpc": "2.0", "id": time.Now().UnixNano(), "method": method}
	if params != nil {
		payload["params"] = params
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("%v", envelope.Error)
	}
	return envelope.Result, nil
}

func safeMCPError(err error) string {
	const limit = 240
	message := strings.TrimSpace(err.Error())
	if len(message) > limit {
		message = message[:limit]
	}
	return message
}

func Serve(address string, service *market.Service, analyzer *analysis.VolumeAnalyzer, assets Assets) error {
	if assets.Symbol == "" {
		assets.Symbol = "RELIANCE"
	}
	symbol := strings.ToUpper(assets.Symbol)
	predictions := newPredictionStore(strings.ToLower(symbol) + "-target-predictions.json")
	dailyTargets := newDailyTargetStore(strings.ToLower(symbol) + "-daily-targets.json")
	go monitorPredictions(service, predictions)
	go monitorDailyTargets(service, dailyTargets)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/nse-mcp/tools", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleNSEMCPTools(w, r)
	})
	mux.HandleFunc("/api/nse-mcp/call", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleNSEMCPCall(w, r)
	})
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		requireMethod(w, r, http.MethodGet)
		writeJSON(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		requireMethod(w, r, http.MethodGet)
		writeJSON(w, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, map[string]string{"symbol": symbol, "theme": "light"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		html, err := os.ReadFile(assets.HTMLPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "dashboard asset unavailable")
			return
		}
		html = []byte(strings.ReplaceAll(string(html), "RELIANCE", symbol))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Write(html)
	})
	assetsDir := filepath.Dir(assets.HTMLPath) + string(os.PathSeparator)
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsDir+"assets"))))
	mux.HandleFunc("/api/quote", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		exchange := market.Exchange(strings.ToUpper(r.URL.Query().Get("exchange")))
		if exchange == "" {
			exchange = market.NSE
		}
		requestedSymbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
		if requestedSymbol == "" {
			requestedSymbol = symbol
		}
		quote, err := service.Quote(exchange, requestedSymbol)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, quote)
	})
	mux.HandleFunc("/api/analysis", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		exchange := market.Exchange(strings.ToUpper(r.URL.Query().Get("exchange")))
		if exchange == "" {
			exchange = market.NSE
		}
		points, err := service.Intraday(exchange, symbol)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		result, err := analyzer.Analyze(exchange, points)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "analysis unavailable for the current market data")
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("/api/candles", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		exchange := market.Exchange(strings.ToUpper(r.URL.Query().Get("exchange")))
		if exchange == "" {
			exchange = market.NSE
		}
		intervals := map[string]int64{"1m": 60, "5m": 300, "15m": 900, "1h": 3600}
		intervalSeconds, ok := intervals[strings.ToLower(r.URL.Query().Get("timeframe"))]
		if !ok {
			writeError(w, http.StatusBadRequest, "timeframe must be 1m, 5m, 15m, or 1h")
			return
		}
		points, err := service.Intraday(exchange, symbol)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, analysis.AggregateCandles(points, intervalSeconds))
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		streamQuotes(w, r, service, symbol)
	})
	mux.HandleFunc("/api/prediction", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			createPrediction(w, r, service, analyzer, predictions, symbol, dailyTargets)
		case http.MethodGet:
			evaluatePredictions(w, service, predictions)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	mux.HandleFunc("/api/daily-targets", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		resolveDailyTargets(service, dailyTargets)
		writeJSON(w, dailyTargets.snapshot())
	})
	mux.HandleFunc("/api/performance", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, analysis.BuildPerformanceReport(len(predictions.snapshot()), nil))
	})
	mux.HandleFunc("/api/options-backtest", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		report, err := analysis.RunOptionsBacktest(nil, analysis.BacktestResult{})
		if err != nil {
			writeJSON(w, report)
			return
		}
		writeJSON(w, report)
	})
	mux.HandleFunc("/api/volume-targets", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		points, err := service.Intraday(market.NSE, symbol)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, analysis.AnalyzeVolumeTargets(points, strings.ToUpper(symbol)))
	})
	mux.HandleFunc("/api/options", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		if !strings.EqualFold(symbol, "NIFTY") && !strings.EqualFold(symbol, "NIFTY50") {
			writeError(w, http.StatusNotFound, "option chain is available only for NIFTY")
			return
		}
		chain, err := service.OptionChain(market.NSE, symbol)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, chain)
	})

	fmt.Printf("Live market dashboard: http://%s\n", address)
	server := &http.Server{
		Addr:              address,
		Handler:           withProductionMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	return server.ListenAndServe()
}

func createPrediction(w http.ResponseWriter, r *http.Request, service *market.Service, analyzer *analysis.VolumeAnalyzer, store *predictionStore, symbol string, dailyTargets ...*dailyTargetStore) {
	exchange := market.Exchange(strings.ToUpper(r.URL.Query().Get("exchange")))
	if exchange == "" {
		exchange = market.NSE
	}
	points, err := service.Intraday(exchange, symbol)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	result, err := analyzer.Analyze(exchange, points)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "prediction unavailable for the current market data")
		return
	}
	now := time.Now()
	direction := 0.0
	optionConfidence := 0.0
	switch result.Signal {
	case "BUY":
		direction = 1
	case "SELL":
		direction = -1
	}
	if direction == 0 && (strings.EqualFold(symbol, "NIFTY") || strings.EqualFold(symbol, "NIFTY50")) {
		bullish, bearish := niftyDirectionVotes(result)
		switch {
		case bullish > bearish:
			direction = 1
		case bearish > bullish:
			direction = -1
		}
	}
	calculatedDistance := 1.5 * maxFloat(result.RangeProxy, math.Abs(result.Target10m-result.Last)*1.5)
	if strings.EqualFold(symbol, "NIFTY") || strings.EqualFold(symbol, "NIFTY50") {
		if chain, chainErr := service.OptionChain(market.NSE, symbol); chainErr == nil && chain.Confidence >= 60 {
			optionConfidence = chain.Confidence
			switch chain.Bias {
			case "BUY":
				direction = 1
			case "SELL":
				direction = -1
			}
		}
		calculatedDistance = maxFloat(niftyTargetDistance(result), 50)
	}
	distance := calculatedDistance
	target := result.Last
	directionName := "HOLD"
	if direction > 0 {
		directionName = "BUY"
	} else if direction < 0 {
		directionName = "SELL"
	}
	if direction != 0 {
		target += direction * distance
	}
	stopLossDistance := 25.0
	if strings.EqualFold(symbol, "NIFTY") || strings.EqualFold(symbol, "NIFTY50") {
		stopLossDistance = niftyStopLossDistance(result)
	}
	if direction == 0 {
		stopLossDistance = 0
	}
	stopLoss := result.Last
	if direction > 0 {
		stopLoss -= stopLossDistance
	} else if direction < 0 {
		stopLoss += stopLossDistance
	}
	prediction := Prediction{
		ID: now.Format("20060102T150405.000000"), Exchange: exchange, Symbol: symbol,
		CreatedAt: now, Entry: result.Last,
		Target: target, LowBand: target - result.RangeProxy/2, HighBand: target + result.RangeProxy/2,
		Signal: result.Signal, Confidence: maxFloat(result.Confidence, optionConfidence), Direction: directionName,
		Method:   "ATR percentile-median with VWAP/EMA/MACD/RSI/ROC and NIFTY option-chain Greek consensus",
		StopLoss: stopLoss, Status: "pending", Outcome: "pending",
	}
	if r.URL.Query().Get("force") == "1" {
		if err := store.addNow(prediction); err != nil {
			writeError(w, http.StatusInternalServerError, "prediction could not be persisted")
			return
		}
	} else if err := store.add(prediction); err != nil {
		writeError(w, http.StatusInternalServerError, "prediction could not be persisted")
		return
	}
	if len(dailyTargets) > 0 {
		recordDailyTargets(result, symbol, exchange, dailyTargets[0], now)
	}
	writeJSON(w, prediction)
}

func niftyDirectionVotes(result analysis.Result) (bullish, bearish int) {
	if result.Last > result.VWAP {
		bullish++
	} else if result.Last < result.VWAP {
		bearish++
	}
	if result.Target10m > result.Last {
		bullish++
	} else if result.Target10m < result.Last {
		bearish++
	}
	if result.Indicators.EMA20 > 0 && result.Last > result.Indicators.EMA20 {
		bullish++
	} else if result.Indicators.EMA20 > 0 && result.Last < result.Indicators.EMA20 {
		bearish++
	}
	if result.Indicators.MACD > result.Indicators.SignalLine {
		bullish++
	} else if result.Indicators.MACD < result.Indicators.SignalLine {
		bearish++
	}
	if result.Indicators.RSI14 >= 55 {
		bullish++
	} else if result.Indicators.RSI14 <= 45 && result.Indicators.RSI14 > 0 {
		bearish++
	}
	if result.Indicators.ROC10 > 0 {
		bullish++
	} else if result.Indicators.ROC10 < 0 {
		bearish++
	}
	return bullish, bearish
}

func niftyTargetDistance(result analysis.Result) float64 {
	atr := math.Max(result.Indicators.ATR14, result.RangeProxy)
	candidates := []float64{
		0.8 * atr,
		1.0 * atr,
		1.25 * atr,
		10 * result.RangeProxy,
		1.5 * math.Abs(result.Target10m-result.Last),
	}
	sort.Float64s(candidates)
	median := candidates[len(candidates)/2]
	volatilityBuffer := 0.5 * math.Abs(result.Last-result.VWAP)
	return maxFloat(50, median+volatilityBuffer)
}

func niftyStopLossDistance(result analysis.Result) float64 {
	atr := math.Max(result.Indicators.ATR14, result.RangeProxy)
	return maxFloat(25, math.Min(60, atr*0.75))
}

func evaluatePredictions(w http.ResponseWriter, service *market.Service, store *predictionStore) {
	monitorPredictionStore(service, store)
	writeJSON(w, store.snapshot())
}

func monitorPredictions(service *market.Service, store *predictionStore) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		monitorPredictionStore(service, store)
	}
}

func monitorPredictionStore(service *market.Service, store *predictionStore) {
	now := time.Now()
	for _, prediction := range store.snapshot() {
		if prediction.Status != "pending" {
			continue
		}
		direction := effectiveDirection(prediction)
		points, err := service.Intraday(prediction.Exchange, prediction.Symbol)
		if err == nil {
			for _, point := range points {
				if point.Time < prediction.CreatedAt.Unix() || point.Time > now.Unix() {
					continue
				}
				result := prediction
				result.ResultAt = &now
				if direction == "BUY" && point.High >= prediction.Target {
					result.ResultPrice = point.High
					result.TargetHit = true
					result.Outcome = "target"
					result.Status = "completed"
					_ = store.update(result)
					break
				}
				if direction == "SELL" && point.Low <= prediction.Target {
					result.ResultPrice = point.Low
					result.TargetHit = true
					result.Outcome = "target"
					result.Status = "completed"
					_ = store.update(result)
					break
				}
				if prediction.StopLoss > 0 && direction == "BUY" && point.Low <= prediction.StopLoss {
					result.ResultPrice = point.Low
					result.Outcome = "stop-loss"
					result.Status = "completed"
					_ = store.update(result)
					break
				}
				if prediction.StopLoss > 0 && direction == "SELL" && point.High >= prediction.StopLoss {
					result.ResultPrice = point.High
					result.Outcome = "stop-loss"
					result.Status = "completed"
					_ = store.update(result)
					break
				}
			}
		}
		if prediction.Status != "pending" {
			continue
		}
		latest, err := storeQuote(service, prediction)
		if err != nil {
			continue
		}
		result := prediction
		result.ResultAt = &now
		result.ResultPrice = latest
		result.TargetHit = targetReached(result, latest)
		result.DirectionHit = directionReached(result, latest)
		result.Status = "completed"
		if result.TargetHit {
			result.Outcome = "target"
		} else if stoppedOut(result, latest) {
			result.Outcome = "stop-loss"
		} else {
			continue
		}
		_ = store.update(result)
	}
}

func storeQuote(service *market.Service, prediction Prediction) (float64, error) {
	quote, err := service.Quote(prediction.Exchange, prediction.Symbol)
	if err != nil {
		return 0, err
	}
	return quote.Last, nil
}

func effectiveDirection(prediction Prediction) string {
	if prediction.Direction == "BUY" || prediction.Direction == "SELL" {
		return prediction.Direction
	}
	if prediction.Signal == "BUY" {
		return "BUY"
	}
	if prediction.Signal == "SELL" {
		return "SELL"
	}
	if prediction.Target > prediction.Entry {
		return "BUY"
	}
	if prediction.Target < prediction.Entry {
		return "SELL"
	}
	return "HOLD"
}

func targetReached(prediction Prediction, price float64) bool {
	if effectiveDirection(prediction) == "BUY" {
		return price >= prediction.Target
	}
	return effectiveDirection(prediction) == "SELL" && price <= prediction.Target
}

func directionReached(prediction Prediction, price float64) bool {
	if effectiveDirection(prediction) == "BUY" {
		return price >= prediction.Entry
	}
	return effectiveDirection(prediction) == "SELL" && price <= prediction.Entry
}

func stoppedOut(prediction Prediction, price float64) bool {
	if prediction.StopLoss <= 0 {
		return false
	}
	if effectiveDirection(prediction) == "BUY" {
		return price <= prediction.StopLoss
	}
	return effectiveDirection(prediction) == "SELL" && price >= prediction.StopLoss
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func recordDailyTargets(result analysis.Result, symbol string, exchange market.Exchange, store *dailyTargetStore, now time.Time) {
	for _, timeframe := range result.Timeframes {
		if timeframe.Signal != "BUY" && timeframe.Signal != "SELL" {
			continue
		}
		duration := 15 * time.Minute
		switch timeframe.Timeframe {
		case "5m":
			duration = 5 * time.Minute
		case "15m":
			duration = 15 * time.Minute
		case "1h":
			duration = 60 * time.Minute
		}
		target := DailyTarget{
			ID:         now.Format("20060102T150405.000000") + "-" + timeframe.Timeframe,
			TradingDay: now.Format("2006-01-02"),
			CreatedAt:  now,
			DueAt:      now.Add(duration),
			Exchange:   exchange,
			Symbol:     symbol,
			Timeframe:  timeframe.Timeframe,
			Signal:     timeframe.Signal,
			Confidence: timeframe.Confidence,
			Entry:      timeframe.Entry,
			StopLoss:   timeframe.StopLoss,
			Target:     timeframe.Target,
			Status:     "pending",
			Outcome:    "pending",
			Reason:     timeframe.Reason,
		}
		_ = store.add(target)
	}
}

func monitorDailyTargets(service *market.Service, store *dailyTargetStore) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		resolveDailyTargets(service, store)
	}
}

func resolveDailyTargets(service *market.Service, store *dailyTargetStore) {
	now := time.Now()
	for _, target := range store.snapshot() {
		if target.Status != "pending" {
			continue
		}
		points, err := service.Intraday(target.Exchange, target.Symbol)
		if err == nil {
			for _, point := range points {
				if point.Time < target.CreatedAt.Unix() || point.Time > now.Unix() {
					continue
				}
				updated := target
				updated.ResultAt = &now
				updated.ResultPrice = point.Close
				if target.Signal == "BUY" && point.High >= target.Target {
					updated.Status, updated.Outcome = "completed", "target-hit"
					updated.ResultPrice, updated.ResultAt = point.High, &now
					_ = store.update(updated)
					break
				}
				if target.Signal == "SELL" && point.Low <= target.Target {
					updated.Status, updated.Outcome = "completed", "target-hit"
					updated.ResultPrice, updated.ResultAt = point.Low, &now
					_ = store.update(updated)
					break
				}
				if target.StopLoss > 0 && target.Signal == "BUY" && point.Low <= target.StopLoss {
					updated.Status, updated.Outcome = "completed", "stop-loss-hit"
					updated.ResultPrice = point.Low
					_ = store.update(updated)
					break
				}
				if target.StopLoss > 0 && target.Signal == "SELL" && point.High >= target.StopLoss {
					updated.Status, updated.Outcome = "completed", "stop-loss-hit"
					updated.ResultPrice = point.High
					_ = store.update(updated)
					break
				}
			}
		}
		if target.Status == "completed" {
			continue
		}
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": message},
	})
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func writeServiceError(w http.ResponseWriter, err error) {
	if strings.Contains(strings.ToLower(err.Error()), "exchange") {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, "market data provider is temporarily unavailable")
}

func withProductionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func streamQuotes(w http.ResponseWriter, r *http.Request, service *market.Service, symbol string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	heartbeat := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-ticker.C:
			events := make([]map[string]any, 2)
			results := make(chan struct {
				index   int
				payload map[string]any
			}, 2)
			exchanges := []market.Exchange{market.NSE, market.BSE}
			if strings.EqualFold(symbol, "NIFTY") || strings.EqualFold(symbol, "NIFTY50") {
				exchanges = []market.Exchange{market.NSE}
			}
			for index, exchange := range exchanges {
				go func(index int, exchange market.Exchange) {
					payload := map[string]any{"exchange": exchange, "at": time.Now()}
					quote, err := service.Quote(exchange, symbol)
					if err != nil {
						payload["error"] = err.Error()
					} else {
						payload["quote"] = quote
					}
					results <- struct {
						index   int
						payload map[string]any
					}{index: index, payload: payload}
				}(index, exchange)
			}

			for range exchanges {
				result := <-results
				events[result.index] = result.payload
			}
			for _, payload := range events {
				if payload == nil {
					continue
				}
				data, _ := json.Marshal(payload)
				fmt.Fprintf(w, "event: quote\ndata: %s\n\n", data)
			}
			flusher.Flush()
		}
	}
}

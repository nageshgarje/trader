package market

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type YahooProvider struct {
	client   *http.Client
	logger   *log.Logger
	mu       sync.Mutex
	cache    map[string]cachedChart
	inflight map[string]*chartCall
}

type cachedChart struct {
	payload map[string]any
	expires time.Time
}

type chartCall struct {
	done    chan struct{}
	payload map[string]any
	err     error
}

func NewYahooProvider(client *http.Client, logger *log.Logger) *YahooProvider {
	if client.Transport == nil {
		client.Transport = &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			MaxConnsPerHost:     20,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
	}
	return &YahooProvider{client: client, logger: logger, cache: make(map[string]cachedChart), inflight: make(map[string]*chartCall)}
}

func (p *YahooProvider) Quote(exchange Exchange, symbol string) (Quote, error) {
	ticker := yahooTicker(exchange, symbol)
	if ticker == "" {
		ticker = strings.ToUpper(symbol)
	}
	if symbol == "500325" {
		ticker = "RELIANCE"
	}
	if ticker == strings.ToUpper(symbol) {
		ticker += map[Exchange]string{NSE: ".NS", BSE: ".BO"}[exchange]
	}
	payload, err := p.chart(ticker, url.Values{"interval": {"1m"}, "range": {"1d"}})
	if err != nil {
		return Quote{}, err
	}
	result, err := chartResult(payload)
	if err != nil {
		return Quote{}, err
	}
	meta, ok := result["meta"].(map[string]any)
	if !ok {
		return Quote{}, errors.New("quote metadata unavailable")
	}
	return Quote{
		Exchange: string(exchange), Symbol: symbol, Last: number(meta["regularMarketPrice"]),
		Change: number(meta["regularMarketChangePercent"]), DayHigh: number(meta["regularMarketDayHigh"]),
		DayLow: number(meta["regularMarketDayLow"]), Volume: number(meta["regularMarketVolume"]),
		Updated: time.Now(),
	}, nil
}

func (p *YahooProvider) Status(exchange Exchange) (any, error) {
	return map[string]any{"exchange": exchange, "status": "live prices available through Yahoo fallback"}, nil
}

func (p *YahooProvider) Search(exchange Exchange, query string) (any, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("search query is required")
	}
	if len(query) > 100 {
		return nil, errors.New("search query is too long")
	}
	endpoint := "https://query1.finance.yahoo.com/v1/finance/search?"
	request, err := http.NewRequest(http.MethodGet, endpoint+url.Values{"q": {query}, "quotesCount": {"10"}, "newsCount": {"0"}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	response, err := p.do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var value any
	return value, json.NewDecoder(response.Body).Decode(&value)
}

func (p *YahooProvider) Intraday(exchange Exchange, symbol string) ([]MinutePoint, error) {
	ticker := yahooTicker(exchange, symbol)
	if ticker == "" {
		ticker = strings.ToUpper(symbol)
	}
	if symbol == "500325" {
		ticker = "RELIANCE"
	}
	if ticker == strings.ToUpper(symbol) {
		ticker += map[Exchange]string{NSE: ".NS", BSE: ".BO"}[exchange]
	}
	payload, err := p.chart(ticker, url.Values{"interval": {"1m"}, "range": {"1d"}})
	if err != nil {
		return nil, err
	}

	result, err := chartResult(payload)
	if err != nil {
		return nil, err
	}
	return minutePoints(result), nil
}

func yahooTicker(exchange Exchange, symbol string) string {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "NIFTY" || normalized == "NIFTY50" || normalized == "^NSEI" {
		if exchange == NSE {
			return "^NSEI"
		}
	}
	return ""
}

func (p *YahooProvider) chart(ticker string, query url.Values) (map[string]any, error) {
	key := ticker + "|" + query.Encode()
	p.mu.Lock()
	if cached, ok := p.cache[key]; ok && time.Now().Before(cached.expires) {
		p.mu.Unlock()
		return cached.payload, nil
	}
	if call, ok := p.inflight[key]; ok {
		p.mu.Unlock()
		<-call.done
		return call.payload, call.err
	}
	call := &chartCall{done: make(chan struct{})}
	p.inflight[key] = call
	p.mu.Unlock()

	payload, err := p.fetchChart(ticker, query)
	p.mu.Lock()
	call.payload, call.err = payload, err
	if err == nil {
		p.cache[key] = cachedChart{payload: payload, expires: time.Now().Add(3 * time.Second)}
	}
	delete(p.inflight, key)
	p.mu.Unlock()
	close(call.done)
	return payload, err
}

func (p *YahooProvider) fetchChart(ticker string, query url.Values) (map[string]any, error) {
	endpoint := "https://query1.finance.yahoo.com/v8/finance/chart/" + url.PathEscape(ticker) + "?" + query.Encode()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := p.do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (p *YahooProvider) do(request *http.Request) (*http.Response, error) {
	request.Header.Set("User-Agent", "Mozilla/5.0")
	request.Header.Set("Accept", "application/json,text/plain,*/*")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 512))
		return nil, fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	return response, nil
}

func chartResult(payload map[string]any) (map[string]any, error) {
	chart, ok := payload["chart"].(map[string]any)
	if !ok {
		return nil, errors.New("invalid chart response")
	}
	results, ok := chart["result"].([]any)
	if !ok || len(results) == 0 {
		return nil, errors.New("market data unavailable")
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		return nil, errors.New("invalid chart result")
	}
	return result, nil
}

func minutePoints(result map[string]any) []MinutePoint {
	timestamps, _ := result["timestamp"].([]any)
	indicators, _ := result["indicators"].(map[string]any)
	quotes, _ := indicators["quote"].([]any)
	if len(quotes) == 0 {
		return nil
	}
	quote, _ := quotes[0].(map[string]any)
	points := make([]MinutePoint, 0, len(timestamps))
	for i, timestamp := range timestamps {
		point := MinutePoint{
			Time: int64(number(timestamp)), Open: listNumber(quote["open"], i),
			High: listNumber(quote["high"], i), Low: listNumber(quote["low"], i),
			Close: listNumber(quote["close"], i), Volume: listNumber(quote["volume"], i),
		}
		if point.Close > 0 {
			points = append(points, point)
		}
	}
	return points
}

func listNumber(value any, index int) float64 {
	values, ok := value.([]any)
	if !ok || index >= len(values) {
		return 0
	}
	return number(values[index])
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case json.Number:
		result, _ := typed.Float64()
		return result
	default:
		return 0
	}
}

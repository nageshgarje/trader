package market

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

const nseOptionChainURL = "https://www.nseindia.com/api/option-chain-indices"

func (p *YahooProvider) OptionChain(exchange Exchange, symbol string) (OptionChain, error) {
	if exchange != NSE || !strings.EqualFold(symbol, "NIFTY") {
		return OptionChain{}, errors.New("option chain is available only for NIFTY on NSE")
	}
	client, err := p.optionHTTPClient()
	if err != nil {
		return OptionChain{}, err
	}
	request, err := http.NewRequest(http.MethodGet, nseOptionChainURL+"?symbol=NIFTY", nil)
	if err != nil {
		return OptionChain{}, err
	}
	setNSEHeaders(request)
	response, err := client.Do(request)
	if err != nil {
		return OptionChain{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 512))
		return OptionChain{}, fmt.Errorf("NSE option chain returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Records struct {
			UnderlyingValue float64     `json:"underlyingValue"`
			ExpiryDates     []string    `json:"expiryDates"`
			Data            []nseStrike `json:"data"`
		} `json:"records"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return OptionChain{}, err
	}
	if payload.Records.UnderlyingValue <= 0 || len(payload.Records.Data) == 0 {
		return OptionChain{}, errors.New("NSE option chain data unavailable")
	}
	return calculateOptionChain(payload.Records.UnderlyingValue, payload.Records.ExpiryDates[0], payload.Records.Data), nil
}

func (p *YahooProvider) optionHTTPClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: p.client.Timeout, Jar: jar, Transport: p.client.Transport}
	baseURL, _ := url.Parse("https://www.nseindia.com/")
	request, err := http.NewRequest(http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, err
	}
	setNSEHeaders(request)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	response.Body.Close()
	return client, nil
}

func setNSEHeaders(request *http.Request) {
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	request.Header.Set("Accept", "application/json,text/plain,*/*")
	request.Header.Set("Accept-Language", "en-US,en;q=9")
	request.Header.Set("Referer", "https://www.nseindia.com/option-chain")
}

type nseStrike struct {
	ExpiryDate string    `json:"expiryDate"`
	Strike     float64   `json:"strikePrice"`
	CALL       nseOption `json:"CE"`
	PUT        nseOption `json:"PE"`
}

type nseOption struct {
	OpenInterest      float64 `json:"openInterest"`
	ChangeOI          float64 `json:"changeinOpenInterest"`
	TotalTraded       float64 `json:"totalTradedVolume"`
	LastPrice         float64 `json:"lastPrice"`
	ImpliedVolatility float64 `json:"impliedVolatility"`
}

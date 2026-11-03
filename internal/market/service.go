package market

import (
	"errors"
	"time"
)

const RequestTimeout = 20 * time.Second

type Exchange string

const (
	NSE Exchange = "NSE"
	BSE Exchange = "BSE"
)

type Quote struct {
	Exchange string    `json:"exchange"`
	Symbol   string    `json:"symbol"`
	Last     float64   `json:"last"`
	Change   float64   `json:"change"`
	DayHigh  float64   `json:"dayHigh"`
	DayLow   float64   `json:"dayLow"`
	Volume   float64   `json:"volume"`
	Updated  time.Time `json:"updated"`
}

type MinutePoint struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type Provider interface {
	Quote(exchange Exchange, symbol string) (Quote, error)
	Status(exchange Exchange) (any, error)
	Search(exchange Exchange, query string) (any, error)
	Intraday(exchange Exchange, symbol string) ([]MinutePoint, error)
	OptionChain(exchange Exchange, symbol string) (OptionChain, error)
}

type Service struct {
	provider Provider
}

func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

func (s *Service) Quote(exchange Exchange, symbol string) (Quote, error) {
	if !validExchange(exchange) {
		return Quote{}, errors.New("exchange must be NSE or BSE")
	}
	return s.provider.Quote(exchange, symbol)
}

func (s *Service) Status(exchange Exchange) (any, error) {
	if !validExchange(exchange) {
		return nil, errors.New("exchange must be NSE or BSE")
	}
	return s.provider.Status(exchange)
}

func (s *Service) Search(exchange Exchange, query string) (any, error) {
	if !validExchange(exchange) {
		return nil, errors.New("exchange must be NSE or BSE")
	}
	return s.provider.Search(exchange, query)
}

func (s *Service) Intraday(exchange Exchange, symbol string) ([]MinutePoint, error) {
	if !validExchange(exchange) {
		return nil, errors.New("exchange must be NSE or BSE")
	}
	return s.provider.Intraday(exchange, symbol)
}

func (s *Service) OptionChain(exchange Exchange, symbol string) (OptionChain, error) {
	if !validExchange(exchange) {
		return OptionChain{}, errors.New("exchange must be NSE or BSE")
	}
	return s.provider.OptionChain(exchange, symbol)
}

func validExchange(exchange Exchange) bool {
	return exchange == NSE || exchange == BSE
}

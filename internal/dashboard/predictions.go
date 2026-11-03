package dashboard

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nageshgarje/trader/internal/market"
)

type Prediction struct {
	ID           string          `json:"id"`
	Exchange     market.Exchange `json:"exchange"`
	Symbol       string          `json:"symbol"`
	CreatedAt    time.Time       `json:"createdAt"`
	DueAt        time.Time       `json:"dueAt"`
	Entry        float64         `json:"entry"`
	Target       float64         `json:"target"`
	LowBand      float64         `json:"lowBand"`
	HighBand     float64         `json:"highBand"`
	Signal       string          `json:"signal"`
	Confidence   float64         `json:"confidence"`
	Direction    string          `json:"direction"`
	Method       string          `json:"method"`
	StopLoss     float64         `json:"stopLoss"`
	Status       string          `json:"status"`
	ResultAt     *time.Time      `json:"resultAt,omitempty"`
	ResultPrice  float64         `json:"resultPrice,omitempty"`
	TargetHit    bool            `json:"targetHit"`
	DirectionHit bool            `json:"directionHit"`
	Error        string          `json:"error,omitempty"`
	Outcome      string          `json:"outcome"`
}

type predictionStore struct {
	path        string
	mu          sync.Mutex
	predictions []Prediction
}

type DailyTarget struct {
	ID          string          `json:"id"`
	TradingDay  string          `json:"tradingDay"`
	CreatedAt   time.Time       `json:"createdAt"`
	DueAt       time.Time       `json:"dueAt"`
	Exchange    market.Exchange `json:"exchange"`
	Symbol      string          `json:"symbol"`
	Timeframe   string          `json:"timeframe"`
	Signal      string          `json:"signal"`
	Confidence  float64         `json:"confidence"`
	Entry       float64         `json:"entry"`
	StopLoss    float64         `json:"stopLoss"`
	Target      float64         `json:"target"`
	Status      string          `json:"status"`
	ResultAt    *time.Time      `json:"resultAt,omitempty"`
	ResultPrice float64         `json:"resultPrice,omitempty"`
	Outcome     string          `json:"outcome"`
	Reason      string          `json:"reason"`
}

func newPredictionStore(path string) *predictionStore {
	store := &predictionStore{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return store
	}
	_ = json.Unmarshal(data, &store.predictions)
	return store
}

func (s *predictionStore) add(prediction Prediction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.predictions {
		if existing.Exchange == prediction.Exchange &&
			existing.Symbol == prediction.Symbol &&
			existing.CreatedAt.Add(14*time.Minute).After(prediction.CreatedAt) &&
			existing.Status == "pending" {
			return nil
		}
	}
	s.predictions = append(s.predictions, prediction)
	return s.saveLocked()
}

func (s *predictionStore) addNow(prediction Prediction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.predictions = append(s.predictions, prediction)
	return s.saveLocked()
}

func (s *predictionStore) snapshot() []Prediction {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := append([]Prediction(nil), s.predictions...)
	return result
}

func (s *predictionStore) update(prediction Prediction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.predictions {
		if s.predictions[i].ID == prediction.ID {
			s.predictions[i] = prediction
			break
		}
	}
	return s.saveLocked()
}

func (s *predictionStore) saveLocked() error {
	data, err := json.MarshalIndent(s.predictions, "", "  ")
	if err != nil {
		return err
	}
	temp := s.path + ".tmp"
	if err := os.MkdirAll(parentDirectory(s.path), 0750); err != nil {
		return err
	}
	if err := os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(temp, s.path); err != nil {
		return errors.New("save predictions: " + err.Error())
	}
	return nil
}

func parentDirectory(path string) string {
	if index := strings.LastIndex(path, string(os.PathSeparator)); index >= 0 {
		return path[:index]
	}
	return "."
}

type dailyTargetStore struct {
	path    string
	mu      sync.Mutex
	targets []DailyTarget
}

func newDailyTargetStore(path string) *dailyTargetStore {
	store := &dailyTargetStore{path: path, targets: make([]DailyTarget, 0)}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &store.targets)
	}
	return store
}

func (s *dailyTargetStore) add(target DailyTarget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	day := target.CreatedAt.Format("2006-01-02")
	for _, existing := range s.targets {
		if existing.TradingDay == day && existing.Exchange == target.Exchange &&
			existing.Symbol == target.Symbol && existing.Timeframe == target.Timeframe &&
			existing.Status == "pending" {
			return nil
		}
	}
	s.targets = append(s.targets, target)
	return s.saveLocked()
}

func (s *dailyTargetStore) snapshot() []DailyTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]DailyTarget, len(s.targets))
	copy(result, s.targets)
	return result
}

func (s *dailyTargetStore) update(target DailyTarget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.targets {
		if s.targets[i].ID == target.ID {
			s.targets[i] = target
			break
		}
	}
	return s.saveLocked()
}

func (s *dailyTargetStore) saveLocked() error {
	data, err := json.MarshalIndent(s.targets, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(parentDirectory(s.path), 0750); err != nil {
		return err
	}
	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(temp, s.path); err != nil {
		return errors.New("save daily targets: " + err.Error())
	}
	return nil
}

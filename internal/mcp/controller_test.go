package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nageshgarje/trader/internal/analysis"
	"github.com/nageshgarje/trader/internal/market"
)

func TestInitializeAndToolsList(t *testing.T) {
	controller := NewController(&fakeMarketService{}, analysis.NewVolumeAnalyzer())
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\",\"params\":{}}\n")
	var output strings.Builder

	if err := Serve(input, &output, controller, nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d responses, want 2: %s", len(lines), output.String())
	}
	var response Response
	if err := json.Unmarshal([]byte(lines[0]), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatalf("initialize returned error: %+v", response.Error)
	}
}

type fakeMarketService struct{}

func (*fakeMarketService) Quote(market.Exchange, string) (market.Quote, error) {
	return market.Quote{}, nil
}

func (*fakeMarketService) Status(market.Exchange) (any, error) { return nil, nil }

func (*fakeMarketService) Search(market.Exchange, string) (any, error) { return nil, nil }

func (*fakeMarketService) Intraday(market.Exchange, string) ([]market.MinutePoint, error) {
	return nil, nil
}

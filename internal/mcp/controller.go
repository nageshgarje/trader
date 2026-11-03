package mcp

import (
	"encoding/json"
	"errors"

	"github.com/nageshgarje/trader/internal/analysis"
	"github.com/nageshgarje/trader/internal/market"
)

type MarketService interface {
	Quote(exchange market.Exchange, symbol string) (market.Quote, error)
	Status(exchange market.Exchange) (any, error)
	Search(exchange market.Exchange, query string) (any, error)
	Intraday(exchange market.Exchange, symbol string) ([]market.MinutePoint, error)
}

type AnalysisService interface {
	Analyze(exchange market.Exchange, points []market.MinutePoint) (analysis.Result, error)
}

type Controller interface {
	Handle(request Request) (Response, error)
}

type controller struct {
	marketService   MarketService
	analysisService AnalysisService
}

func NewController(marketService MarketService, analysisService AnalysisService) Controller {
	return &controller{marketService: marketService, analysisService: analysisService}
}

func (c *controller) Handle(request Request) (Response, error) {
	switch request.Method {
	case MethodInitialize:
		return Response{JSONRPC: JSONRPCVersion, ID: request.ID, Result: map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": ServerName, "version": ServerVersion},
		}}, nil
	case MethodInitialized, MethodCancelled:
		return Response{}, nil
	case MethodPing:
		return Response{JSONRPC: JSONRPCVersion, ID: request.ID, Result: map[string]any{}}, nil
	case MethodToolsList:
		return Response{JSONRPC: JSONRPCVersion, ID: request.ID, Result: map[string]any{"tools": ToolDefinitions()}}, nil
	case MethodToolsCall:
		return c.callTool(request)
	default:
		return Response{}, errors.New("method not found")
	}
}

func (c *controller) callTool(request Request) (Response, error) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return Response{}, errors.New("invalid tools/call parameters")
	}

	exchange := market.Exchange(Upper(argument(params.Arguments, "exchange")))
	symbol := argument(params.Arguments, "symbol")
	if symbol == "" {
		symbol = DefaultSymbol
	}

	var result any
	var err error
	switch params.Name {
	case ToolMarketQuote:
		result, err = c.marketService.Quote(exchange, symbol)
	case ToolMarketStatus:
		result, err = c.marketService.Status(exchange)
	case ToolMarketSearch:
		result, err = c.marketService.Search(exchange, argument(params.Arguments, "query"))
	case ToolMarketVolumeAnalysis:
		var points []market.MinutePoint
		points, err = c.marketService.Intraday(exchange, symbol)
		if err == nil {
			result, err = c.analysisService.Analyze(exchange, points)
		}
	case ToolGetStockData:
		name := argument(params.Arguments, "name")
		if name == "" {
			name = argument(params.Arguments, "symbol")
		}
		result, err = c.marketService.Search(market.NSE, name)
	case ToolSearchIndustry:
		result, err = c.marketService.Search(market.NSE, argument(params.Arguments, "query")+" industry")
	case ToolGetTrendingStocks, ToolGetNSEMostActive, ToolGetBSEMostActive:
		result = map[string]any{
			"status": "unavailable",
			"reason": "The configured market provider does not expose this aggregate endpoint yet.",
			"tool":   params.Name,
		}
	default:
		err = errors.New("unknown tool: " + params.Name)
	}
	if err != nil {
		return Response{}, err
	}
	return Response{JSONRPC: JSONRPCVersion, ID: request.ID, Result: map[string]any{
		"content": []map[string]any{{"type": "text", "text": Compact(result)}},
	}}, nil
}

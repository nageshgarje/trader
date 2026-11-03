package mcp

import (
	"encoding/json"
	"strings"
)

func ToolDefinitions() []map[string]any {
	return []map[string]any{
		{"name": ToolMarketQuote, "description": "Get a live NSE or BSE quote.", "inputSchema": objectSchema([]string{"exchange", "symbol"})},
		{"name": ToolMarketStatus, "description": "Get NSE or BSE market status.", "inputSchema": objectSchema([]string{"exchange"})},
		{"name": ToolMarketSearch, "description": "Search NSE or BSE symbols.", "inputSchema": objectSchema([]string{"exchange", "query"})},
		{"name": ToolMarketVolumeAnalysis, "description": "Analyze minute volume and return a buy/sell signal.", "inputSchema": objectSchema([]string{"exchange", "symbol"})},
		{"name": ToolGetTrendingStocks, "description": "Get NSE and BSE trending-market data from the configured provider.", "inputSchema": objectSchema([]string{})},
		{"name": ToolGetNSEMostActive, "description": "Get NSE most-active data from the configured provider.", "inputSchema": objectSchema([]string{})},
		{"name": ToolGetBSEMostActive, "description": "Get BSE most-active data from the configured provider.", "inputSchema": objectSchema([]string{})},
		{"name": ToolGetStockData, "description": "Search for detailed NSE or BSE company market data.", "inputSchema": objectSchema([]string{"name"})},
		{"name": ToolSearchIndustry, "description": "Search companies by industry.", "inputSchema": objectSchema([]string{"query"})},
	}
}

func objectSchema(required []string) map[string]any {
	properties := map[string]any{
		"exchange": map[string]any{"type": "string", "enum": []string{"NSE", "BSE"}},
		"symbol":   map[string]any{"type": "string"},
		"query":    map[string]any{"type": "string"},
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func argument(arguments map[string]any, name string) string {
	value, _ := arguments[name].(string)
	return strings.TrimSpace(value)
}

func Compact(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func Upper(value string) string {
	return strings.ToUpper(value)
}

package mcp

const (
	ProtocolVersion = "2024-11-05"
	ServerName      = "india-market-mcp"
	ServerVersion   = "0.2.0"

	MethodInitialize  = "initialize"
	MethodInitialized = "notifications/initialized"
	MethodCancelled   = "notifications/cancelled"
	MethodPing        = "ping"
	MethodToolsList   = "tools/list"
	MethodToolsCall   = "tools/call"

	ToolMarketQuote          = "market_quote"
	ToolMarketStatus         = "market_status"
	ToolMarketSearch         = "market_search"
	ToolMarketVolumeAnalysis = "market_volume_analysis"
	ToolGetTrendingStocks    = "get_trending_stocks"
	ToolGetNSEMostActive     = "get_nse_most_active"
	ToolGetBSEMostActive     = "get_bse_most_active"
	ToolGetStockData         = "get_stock_data"
	ToolSearchIndustry       = "search_industry"

	DefaultSymbol = "RELIANCE"
)

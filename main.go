package main

import (
	"log"
	"net/http"
	"os"

	"github.com/nageshgarje/trader/internal/analysis"
	"github.com/nageshgarje/trader/internal/dashboard"
	"github.com/nageshgarje/trader/internal/market"
	mcpserver "github.com/nageshgarje/trader/internal/mcp"
)

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	provider := market.NewYahooProvider(&http.Client{Timeout: market.RequestTimeout}, logger)
	marketService := market.NewService(provider)
	analyzer := analysis.NewVolumeAnalyzer()

	if len(os.Args) > 1 && os.Args[1] == "--mcp-http" {
		controller := mcpserver.NewController(marketService, analyzer)
		address := "127.0.0.1:8000"
		if configured := os.Getenv("ISE_MCP_HTTP_ADDR"); configured != "" {
			address = configured
		}
		if err := mcpserver.ServeHTTP(address, controller, logger); err != nil {
			logger.Fatal(err)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "--dashboard" {
		if err := dashboard.Serve(dashboardAddress(), marketService, analyzer, dashboard.Assets{
			HTMLPath: dashboard.DefaultHTMLPath,
			Symbol:   dashboard.DefaultRelianceSymbol,
		}); err != nil {
			logger.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--dashboard-nifty" {
		if err := dashboard.Serve(niftyDashboardAddress(), marketService, analyzer, dashboard.Assets{
			HTMLPath: dashboard.DefaultHTMLPath,
			Symbol:   dashboard.DefaultNiftySymbol,
		}); err != nil {
			logger.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--dashboard-nse" {
		if err := dashboard.Serve(nseDashboardAddress(), marketService, analyzer, dashboard.Assets{
			HTMLPath: dashboard.DefaultNSEDashboardHTMLPath,
			Symbol:   dashboard.DefaultNiftySymbol,
		}); err != nil {
			logger.Fatal(err)
		}
		return
	}

	controller := mcpserver.NewController(marketService, analyzer)
	if err := mcpserver.Serve(os.Stdin, os.Stdout, controller, logger); err != nil {
		logger.Fatal(err)
	}
}

func dashboardAddress() string {
	if address := os.Getenv(dashboard.AddressEnvironmentVariable); address != "" {
		return address
	}
	return dashboard.DefaultAddress
}

func niftyDashboardAddress() string {
	if address := os.Getenv(dashboard.NiftyAddressEnvironmentVariable); address != "" {
		return address
	}
	return dashboard.DefaultNiftyAddress
}

func nseDashboardAddress() string {
	if address := os.Getenv(dashboard.NSEDashboardAddressEnvironmentVariable); address != "" {
		return address
	}
	return dashboard.DefaultNSEDashboardAddress
}

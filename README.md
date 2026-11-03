# India Market MCP Server

A Go-based Model Context Protocol (MCP) server exposing NSE and BSE market data.

## Tools

- `market_quote` - live quote
- `market_historical` - daily OHLCV history
- `market_status` - exchange market status
- `market_search` - symbol or scrip search

The server communicates over MCP stdio.

## Run

```sh
go run .
```

## Live browser dashboard

```sh
go run . --dashboard
```

Open `http://127.0.0.1:8080`. The dashboard streams NSE and BSE Reliance quotes every
five seconds using Server-Sent Events and plots both prices on a live canvas chart.

## VS Code MCP configuration

Add the server to your MCP configuration:

```json
{
  "servers": {
    "india-markets": {
      "type": "stdio",
      "command": "go",
      "args": ["run", "/absolute/path/to/trader"]
    }
  }
}
```

For NSE, pass an uppercase symbol, such as `RELIANCE`. For BSE, pass the numeric scrip code, such as `500325`.

Public exchange endpoints can be rate-limited or change without notice. NSE pages commonly require a session warm-up cookie; this server sends normal browser headers and a cacheable HTTP client, but availability depends on the exchange.

## Production endpoints

- `GET /health/live` - process liveness
- `GET /health/ready` - dashboard readiness
- `GET /api/quote`
- `GET /api/analysis`
- `GET|POST /api/prediction`
- `GET /events` - Server-Sent Events quote stream

The JSON error shape is:

```json
{"error":{"message":"human-readable safe message"}}
```

## Container

```sh
docker build -t india-market-terminal .
docker run --rm -p 8080:8080 india-market-terminal
```

When mounting prediction files, use a dedicated volume and run one replica until persistence is moved to a shared database.

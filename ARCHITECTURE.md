# Architecture Assessment and Production Plan

## Current architecture

The application is a small Go modular monolith that serves two interfaces:

- A JSON-lines MCP server over standard input/output.
- An HTTP dashboard with JSON, Server-Sent Events, and a self-contained HTML UI.

Market data is retrieved through an adapter for Yahoo Finance. The analysis and prediction modules are separate Go packages. Predictions are persisted as local JSON files and are intended for a single dashboard instance.

## Technical debt addressed

- HTTP handlers now enforce methods, use structured JSON errors, apply security headers, and have bounded server timeouts.
- Provider failures no longer expose upstream response bodies.
- Search input is bounded and validated.
- Prediction writes use restrictive file permissions and create their parent directory safely.
- The dashboard has responsive layout, semantic regions, escaped dynamic content, and improved loading/error states.

## Target boundaries

This project should remain a modular monolith until independent deployment is required. The existing market provider, analysis, and dashboard packages are already suitable module boundaries. A future database-backed repository should be introduced only when JSON persistence becomes a concurrency or availability bottleneck.

Recommended evolution:

1. Move provider calls behind interfaces for multiple market-data sources.
2. Add context-aware provider methods and cancellation.
3. Move prediction persistence behind a repository interface.
4. Add a database and migrations if multi-instance dashboard deployment is required.
5. Add Redis only when shared quote caching or rate limiting is required across replicas.
6. Add asynchronous processing only for scheduled prediction workflows that cannot run safely in-process.

## Production operations

- Bind the dashboard to loopback or a trusted network by default.
- Set `MARKET_DASHBOARD_ADDR` explicitly in container deployments.
- Run one dashboard replica while predictions are stored on local JSON files.
- Mount prediction JSON files or migrate persistence before running multiple replicas.
- External market endpoints can be unavailable or rate limited; monitor provider errors and latency.
- The application does not currently implement user authentication. Do not publish it to an untrusted network without an authentication proxy.

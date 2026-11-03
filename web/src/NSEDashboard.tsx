import { useEffect, useMemo, useState } from 'react'

type Quote = { symbol?: string; last: number; change: number; changePercent: number; open: number; dayHigh: number; dayLow: number; previousClose: number; volume: number }
type Analysis = { last: number; vwap: number; signal: string; confidence: number; rangeProxy: number; indicators: Record<string, number>; reasons?: string[] }
type Candle = { time: number | string; open: number; high: number; low: number; close: number; volume: number }
type OptionStrike = { strike:number; callOI:number; putOI:number; callVolume:number; putVolume:number; callIV:number; putIV:number; callDelta:number; putDelta:number }
type OptionChain = { underlying:number; expiry:string; pcr:number; pcrVolume:number; support:number; resistance:number; maxPain:number; bias:string; confidence:number; reason:string; strikes:OptionStrike[] }
type MCPTool = { name:string; description?:string; inputSchema?:{properties?:Record<string,{type?:string; description?:string}>; required?:string[]} }

const indian = new Intl.NumberFormat('en-IN', { maximumFractionDigits: 2 })
const money = (value?: number) => `₹${indian.format(Number(value || 0))}`
const signed = (value?: number) => `${Number(value || 0) >= 0 ? '+' : ''}${Number(value || 0).toFixed(2)}`

export default function NSEDashboard() {
  const [quote, setQuote] = useState<Quote>()
  const [analysis, setAnalysis] = useState<Analysis>()
  const [chain, setChain] = useState<OptionChain>()
  const [candles, setCandles] = useState<Candle[]>([])
  const [frame, setFrame] = useState('15m')
  const [status, setStatus] = useState('Connecting')
  const [error, setError] = useState('')
  const [updatedAt, setUpdatedAt] = useState<Date>()
  const [mcpTools, setMcpTools] = useState<MCPTool[]>([])
  const [selectedTool, setSelectedTool] = useState('')
  const [toolArguments, setToolArguments] = useState('{}')
  const [toolResult, setToolResult] = useState('')
  const [mcpError, setMcpError] = useState('')

  async function load() {
    try {
      const [quoteResponse, analysisResponse, chainResponse] = await Promise.all([
        fetch('/api/quote?exchange=NSE&symbol=NIFTY', { cache: 'no-store' }),
        fetch('/api/analysis?exchange=NSE&symbol=NIFTY', { cache: 'no-store' }),
        fetch('/api/options?exchange=NSE&symbol=NIFTY', { cache: 'no-store' }),
      ])
      if (!quoteResponse.ok || !analysisResponse.ok) throw new Error('Core NSE MCP market services are unavailable')
      setQuote(await quoteResponse.json())
      setAnalysis(await analysisResponse.json())
      if (chainResponse.ok) {
        setChain(await chainResponse.json())
      } else {
        setChain(undefined)
      }
      setUpdatedAt(new Date())
      setStatus('Live')
      setError(chainResponse.ok ? '' : 'NSE option-chain data is temporarily blocked upstream; core market data remains live.')
    } catch (reason) {
      setStatus('Offline')
      setError(reason instanceof Error ? reason.message : 'Unable to load NSE data')
    }
  }

  async function loadCandles(selected = frame) {
    const response = await fetch(`/api/candles?exchange=NSE&symbol=NIFTY&timeframe=${selected}`, { cache: 'no-store' })
    if (!response.ok) throw new Error('NSE candle data unavailable')
    setCandles(await response.json())
  }

  useEffect(() => { void load(); const timer = window.setInterval(() => void load(), 10000); return () => window.clearInterval(timer) }, [])
  useEffect(() => { void loadCandles().catch(reason => setError(reason instanceof Error ? reason.message : 'Chart unavailable')) }, [frame])
  useEffect(() => { void loadMCPTools() }, [])

  async function loadMCPTools() {
    try {
      const response = await fetch('/api/nse-mcp/tools', { cache: 'no-store' })
      const payload = await response.json()
      if (!response.ok) throw new Error(payload?.error?.message || 'NSE MCP tools unavailable')
      const tools = Array.isArray(payload.tools) ? payload.tools : []
      setMcpTools(tools)
      setSelectedTool(too => too || tools[0]?.name || '')
      setMcpError('')
    } catch (reason) {
      setMcpError(reason instanceof Error ? reason.message : 'NSE MCP unavailable')
    }
  }

  async function callMCPTool() {
    let argumentsValue: Record<string, unknown>
    try {
      argumentsValue = JSON.parse(toolArguments)
    } catch {
      setToolResult('Arguments must be valid JSON.')
      return
    }
    try {
      const response = await fetch('/api/nse-mcp/call', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: selectedTool, arguments: argumentsValue }),
      })
      const payload = await response.json()
      if (!response.ok) throw new Error(payload?.error?.message || 'MCP tool call failed')
      setToolResult(JSON.stringify(payload, null, 2))
    } catch (reason) {
      setToolResult(reason instanceof Error ? reason.message : 'MCP tool call failed')
    }
  }

  const nearbyStrikes = useMemo(() => {
    const spot = quote?.last || chain?.underlying || 0
    return (chain?.strikes || []).filter(item => Math.abs(item.strike - spot) <= spot * 0.012).slice(0, 12)
  }, [chain, quote])

  const signal = (analysis?.signal || 'HOLD').toUpperCase()
  const positive = (quote?.changePercent || 0) >= 0
  const closeValues = candles.map(item => item.close)
  const chart = toChart(closeValues)

  return (
    <div className="nse-app">
      <aside>
        <div className="nse-brand"><span>N</span><div><strong>NSE</strong><small>Market Intelligence</small></div></div>
        <nav><button className="active">Overview</button><button>Option Chain</button><button>Technicals</button><button>Market Breadth</button></nav>
        <div className="source-card"><span>MCP SOURCE</span><strong>India Markets</strong><small>NSE · Live data</small></div>
      </aside>
      <main>
        <header><div><span className="market-label">NIFTY 50 · NSE</span><h1>Market dashboard</h1></div><div className="header-status"><span className={status === 'Live' ? 'live-dot' : 'offline-dot'} />{status}<small>{updatedAt ? updatedAt.toLocaleTimeString('en-IN') : 'Waiting for data'}</small></div></header>
        {error && <div className="nse-error">{error}</div>}
        <section className="hero-grid">
          <div className="price-panel">
            <div className="instrument"><div><span>NIFTY</span><strong>Index</strong></div><span className={`session ${positive ? 'up' : 'down'}`}>{positive ? 'BULLISH' : 'BEARISH'}</span></div>
            <div className="price-row"><h2>{money(quote?.last)}</h2><span className={positive ? 'up-text' : 'down-text'}>{signed(quote?.changePercent)}%</span></div>
            <div className="range-row"><span>O {money(quote?.open)}</span><span>H {money(quote?.dayHigh)}</span><span>L {money(quote?.dayLow)}</span><span>Prev {money(quote?.previousClose)}</span></div>
          </div>
          <div className="signal-panel">
            <span>MODEL SIGNAL</span><strong className={`signal-${signal.toLowerCase()}`}>{signal}</strong>
            <div className="confidence-track"><i style={{ width: `${Math.max(0, Math.min(100, analysis?.confidence || 0))}%` }} /></div>
            <small>{Number(analysis?.confidence || 0).toFixed(1)}% signal confidence</small>
          </div>
        </section>
        <section className="metric-grid">
          <Metric label="VWAP" value={money(analysis?.vwap)} />
          <Metric label="ATR range" value={Number(analysis?.rangeProxy || 0).toFixed(2)} />
          <Metric label="Market volume" value={compact(quote?.volume)} />
          <Metric label="PCR" value={Number(chain?.pcr || 0).toFixed(2)} />
          <Metric label="Max pain" value={money(chain?.maxPain)} />
        </section>
        <section className="content-grid">
          <article className="panel chart-panel">
            <div className="panel-head"><div><span>PRICE ACTION</span><h3>NIFTY intraday chart</h3></div><select value={frame} onChange={event => setFrame(event.target.value)}><option value="5m">5 minutes</option><option value="15m">15 minutes</option><option value="1h">1 hour</option></select></div>
            {candles.length ? <svg viewBox="0 0 900 280" preserveAspectRatio="none"><defs><linearGradient id="area" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="#2f80ed" stopOpacity=".28"/><stop offset="1" stopColor="#2f80ed" stopOpacity="0"/></linearGradient></defs><polygon points={`0,280 ${chart.area} 900,280`} fill="url(#area)" /><polyline points={chart.line} fill="none" stroke="#2f80ed" strokeWidth="3" /></svg> : <div className="panel-empty">Loading validated NSE candles…</div>}
          </article>
          <article className="panel levels-panel">
            <div className="panel-head"><div><span>DERIVATIVES</span><h3>Key levels</h3></div></div>
            <Level label="Support" value={chain?.support} tone="support" />
            <Level label="Resistance" value={chain?.resistance} tone="resistance" />
            <Level label="Max pain" value={chain?.maxPain} />
            <div className="bias-box"><span>Chain bias</span><strong>{chain?.bias || 'Awaiting data'}</strong><small>{chain?.reason || 'Option chain data is loading.'}</small></div>
          </article>
        </section>
        <section className="panel chain-panel">
          <div className="panel-head"><div><span>OPTION CHAIN · NSE MCP</span><h3>Near-the-money contracts</h3></div><div className="expiry">Expiry <strong>{chain?.expiry || '—'}</strong></div></div>
          {nearbyStrikes.length ? <div className="table-scroll"><table><thead><tr><th>Call OI</th><th>Call vol</th><th>Call Δ</th><th className="strike">Strike</th><th>Put Δ</th><th>Put vol</th><th>Put OI</th></tr></thead><tbody>{nearbyStrikes.map(item => <tr key={item.strike} className={Math.abs(item.strike - (quote?.last || 0)) < 50 ? 'atm' : ''}><td>{compact(item.callOI)}</td><td>{compact(item.callVolume)}</td><td>{item.callDelta.toFixed(3)}</td><td className="strike">{indian.format(item.strike)}</td><td>{item.putDelta.toFixed(3)}</td><td>{compact(item.putVolume)}</td><td>{compact(item.putOI)}</td></tr>)}</tbody></table></div> : <div className="panel-empty">Option-chain data is unavailable from the current NSE provider.</div>}
        </section>
        <section className="panel mcp-panel">
          <div className="panel-head"><div><span>EXTERNAL NSE MCP</span><h3>Tool explorer</h3></div><button onClick={() => void loadMCPTools()}>Refresh tools</button></div>
          {mcpError && <div className="mcp-message error">{mcpError}</div>}
          {mcpTools.length ? <>
            <div className="mcp-controls">
              <select value={selectedTool} onChange={event => setSelectedTool(event.target.value)}>{mcpTools.map(tool => <option key={tool.name} value={tool.name}>{tool.name}</option>)}</select>
              <textarea aria-label="MCP tool arguments" value={toolArguments} onChange={event => setToolArguments(event.target.value)} spellCheck={false} />
              <button className="run-tool" onClick={() => void callMCPTool()} disabled={!selectedTool}>Run tool</button>
            </div>
            <p className="mcp-description">{mcpTools.find(tool => tool.name === selectedTool)?.description || 'No tool description supplied by the MCP server.'}</p>
            {toolResult && <pre>{toolResult}</pre>}
          </> : !mcpError && <div className="panel-empty">Discovering NSE MCP tools…</div>}
        </section>
      </main>
    </div>
  )
}

function Metric({ label, value }: { label:string; value:string }) { return <div className="metric"><span>{label}</span><strong>{value}</strong></div> }
function Level({ label, value, tone = '' }: { label:string; value?:number; tone?:string }) { return <div className={`level ${tone}`}><span>{label}</span><strong>{money(value)}</strong></div> }
function compact(value?: number) { if (!value) return '—'; return new Intl.NumberFormat('en-IN', { notation:'compact', maximumFractionDigits:1 }).format(value) }
function toChart(values:number[]) {
  if (!values.length) return { line:'', area:'' }
  const min = Math.min(...values), max = Math.max(...values), range = Math.max(max-min, 0.01)
  const coords = values.map((value,index) => {
    const x = values.length === 1 ? 450 : index * 900 / (values.length - 1)
    const y = 245 - ((value-min)/range) * 210
    return [x, y] as const
  })
  return { line: coords.map(([x,y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' '), area: coords.map(([x,y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ') }
}

import { useEffect, useState } from 'react'
import './App.css'

type Quote = { last: number; change: number; dayHigh: number; dayLow: number; volume: number }
type Signal = { timeframe: string; signal: string; confidence: number; entry: number; stopLoss: number; target: number; reason: string }
type Analysis = {
  last: number; vwap: number; target10m: number; rangeProxy: number; confidence: number; accuracy: number
  targetHitRate: number; validationSamples: number; volumeAcceleration: number; buyVolume: number; sellVolume: number
  signal: string; reasons: string[]; indicatorAccuracy?: Record<string, number>
  indicators: Record<string, number>; indicatorSignals: Array<{name:string; value:number; signal:string; strength:number}>
  timeframes: Signal[]
}
type Candle = { time: number; close: number }
type Target = { timeframe:string; signal:string; entry:number; stopLoss:number; target:number; confidence:number; outcome:string; status:string; createdAt:string }

const money = (value: number) => Number(value || 0).toLocaleString('en-IN', { style: 'currency', currency: 'INR', maximumFractionDigits: 2 })
const pct = (value: number) => `${Number(value || 0).toFixed(1)}%`

export default function App() {
  const [symbol, setSymbol] = useState('NIFTY')
  const [quote, setQuote] = useState<Quote>()
  const [analysis, setAnalysis] = useState<Analysis>()
  const [signals, setSignals] = useState<Signal[]>([])
  const [targets, setTargets] = useState<Target[]>([])
  const [candles, setCandles] = useState<Candle[]>([])
  const [timeframe, setTimeframe] = useState('live')
  const [status, setStatus] = useState('Connecting')
  const [error, setError] = useState('')

  async function load() {
    try {
      const [configRes, quoteRes, analysisRes, targetsRes] = await Promise.all([
        fetch('/api/config', { cache: 'no-store' }), fetch('/api/quote?exchange=NSE', { cache: 'no-store' }),
        fetch('/api/analysis?exchange=NSE', { cache: 'no-store' }), fetch('/api/daily-targets', { cache: 'no-store' }),
      ])
      if (!configRes.ok || !quoteRes.ok || !analysisRes.ok || !targetsRes.ok) throw new Error('One or more market services are unavailable')
      const config = await configRes.json()
      const quoteData = await quoteRes.json()
      const analysisData = await analysisRes.json()
      const targetData = await targetsRes.json()
      setSymbol(config.symbol || 'NIFTY')
      setQuote(quoteData)
      setAnalysis(analysisData)
      setSignals(analysisData.timeframes || [])
      setTargets(Array.isArray(targetData) ? targetData : [])
      setStatus('Live')
      setError('')
    } catch (reason) {
      setStatus('Disconnected')
      setError(reason instanceof Error ? reason.message : 'Market data unavailable')
    }
  }

  async function loadCandles(frame = timeframe) {
    if (frame === 'live') {
      setCandles([])
      return
    }
    const response = await fetch(`/api/candles?exchange=NSE&timeframe=${frame}`, { cache: 'no-store' })
    if (!response.ok) throw new Error('Candle history unavailable')
    setCandles(await response.json())
  }

  useEffect(() => {
    load()
    const timer = window.setInterval(load, 10000)
    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => {
    loadCandles().catch(reason => setError(reason instanceof Error ? reason.message : 'Chart unavailable'))
    const timer = window.setInterval(() => loadCandles(), 10000)
    return () => window.clearInterval(timer)
  }, [timeframe])

  const values = candles.map(item => item.close)
  const low = values.length ? Math.min(...values) : 0
  const high = values.length ? Math.max(...values) : 1
  const points = values.map((value, index) => {
    const x = values.length === 1 ? 50 : 12 + index * 976 / (values.length - 1)
    const y = 210 - (value - low) / Math.max(high - low, 0.0001) * 180
    return `${x},${y}`
  }).join(' ')

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand"><span className="brand-mark">M</span><span>Market Terminal</span></div>
        <nav><a className="active" href="#overview">Overview</a><a href="#signals">Signals</a><a href="#targets">Targets</a></nav>
      </aside>
      <main>
        <header className="topbar"><div><strong>{symbol} market overview</strong><span>India equities</span></div><span className={`status ${status === 'Live' ? 'live' : ''}`}>{status}</span></header>
        <div className="content" id="overview">
          {error && <div className="error-banner">{error}</div>}
          <section className="headline">
            <div><span className="eyebrow">NSE · {symbol}</span><h1>{quote ? money(quote.last) : 'Loading...'}</h1><p>{quote ? `${quote.change >= 0 ? '+' : ''}${Number(quote.change).toFixed(2)}% today` : 'Waiting for market data'}</p></div>
            <div className={`direction ${(analysis?.signal || 'hold').toLowerCase()}`}>{analysis?.signal || 'HOLD'}</div>
          </section>

          <section className="metrics-grid">
            <Metric label="VWAP" value={money(analysis?.vwap || 0)} />
            <Metric label="Model target" value={money(analysis?.target10m || 0)} />
            <Metric label="Confidence" value={pct(analysis?.confidence || 0)} />
            <Metric label="Volume acceleration" value={`${Number(analysis?.volumeAcceleration || 0).toFixed(2)}x`} />
            <Metric label="Target hit rate" value={pct(analysis?.targetHitRate || 0)} />
          </section>

          <section className="grid">
            <article className="card chart-card">
              <div className="card-head"><div><span className="eyebrow">Price movement</span><h2>Live exchange prices</h2></div><select value={timeframe} onChange={event => setTimeframe(event.target.value)}><option value="live">Live</option><option value="1m">1 minute</option><option value="5m">5 minutes</option><option value="15m">15 minutes</option><option value="1h">1 hour</option></select></div>
              {timeframe === 'live' ? <div className="chart-empty">Select a timeframe to display validated OHLC candles.</div> : candles.length ? (
                <svg viewBox="0 0 1000 230" role="img" aria-label={`${timeframe} price chart`}><polyline points={points} fill="none" stroke="#2563eb" strokeWidth="2.5" /></svg>
              ) : <div className="chart-empty">Loading candle history...</div>}
            </article>

            <article className="card" id="signals">
              <div className="card-head"><div><span className="eyebrow">Multi-timeframe</span><h2>BUY / SELL levels</h2></div></div>
              <div className="signals">{signals.map(item => <SignalCard key={item.timeframe} item={item} />)}</div>
            </article>

            <article className="card" id="targets">
              <div className="card-head"><div><span className="eyebrow">Performance ledger</span><h2>Daily target validation</h2></div></div>
              <div className="table-wrap"><table><thead><tr><th>Time</th><th>Frame</th><th>Signal</th><th>Entry</th><th>Stop</th><th>Target</th><th>Result</th></tr></thead><tbody>{targets.map((target, index) => <tr key={`${target.createdAt}-${index}`}><td>{new Date(target.createdAt).toLocaleTimeString()}</td><td>{target.timeframe}</td><td>{target.signal}</td><td>{money(target.entry)}</td><td>{money(target.stopLoss)}</td><td>{money(target.target)}</td><td>{target.status === 'pending' ? 'Pending' : target.outcome}</td></tr>)}</tbody></table></div>
            </article>
          </section>

        </div>
      </main>
    </div>
  )
}

function Metric({ label, value }: { label:string; value:string }) {
  return <div className="metric"><span>{label}</span><strong>{value}</strong></div>
}

function SignalCard({ item }: { item:Signal }) {
  return <div className={`signal-card ${item.signal.toLowerCase()}`}><div><strong>{item.timeframe}</strong><span>{item.signal}</span></div><small>{item.reason}</small><dl><div><dt>Entry</dt><dd>{money(item.entry)}</dd></div><div><dt>Stop</dt><dd>{money(item.stopLoss)}</dd></div><div><dt>Target</dt><dd>{money(item.target)}</dd></div></dl><div className="confidence">{pct(item.confidence)} confidence</div></div>
}

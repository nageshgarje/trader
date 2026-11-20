#!/usr/bin/env python3
"""Trader dashboard: one page that shows everything the skills wrote to trader/, refreshed live.
Standard library only. Read only: it never approves, places or changes an order, a rule or a file.

    python3 dashboard/dashboard.py serve                 # http://127.0.0.1:8765, refreshes every 15 s
    python3 dashboard/dashboard.py serve --port 9000 --folder path/to/project
    python3 dashboard/dashboard.py snapshot              # writes trader/dashboard.html (no server)
    python3 dashboard/dashboard.py state                 # the same data as JSON

What it shows, for the US (NYSE) and India (NSE) side by side:
  - the market clocks: open or closed, the session, the next open
  - the trade plan (trader/plan.json) with the latest price on a stop-entry-target ladder
  - the Time Machine's odds and verdict (trader/timemachine.json)
  - the proposed orders with trader-risk's verdict and trader-broker's status (READY, WAITING, DONE)
  - the price alerts and which have rung
  - cash and positions per market, valued at the latest price
  - today's decisions from trader-broker's log
  - the scanner's latest hits

Prices: the latest delayed Yahoo quote for the plan, the orders and the positions, at most once every
30 seconds (--no-quotes to use trader/prices.json only). The server listens on 127.0.0.1 only.
"""

import argparse
import datetime
import html
import importlib.util
import json
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
QUOTE_EVERY = 30          # seconds between quote refreshes
LOCK = threading.Lock()   # the pack's tools keep their market in module state: one reader at a time


def _load(folder, name):
    spec = importlib.util.spec_from_file_location("dash_" + name, os.path.join(ROOT, folder, name + ".py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


alerts = _load("trader-alerts", "alerts")
ticket = _load("trader-broker", "ticket")


def read(path):
    try:
        with open(path, encoding="utf-8") as fh:
            return json.load(fh)
    except (OSError, ValueError):
        return None


def mtime(path):
    try:
        return datetime.datetime.fromtimestamp(os.path.getmtime(path), datetime.timezone.utc).strftime(
            "%Y-%m-%dT%H:%M:%SZ")
    except OSError:
        return None


class Quotes(object):
    """The latest delayed quotes, fetched at most every QUOTE_EVERY seconds per symbol."""

    def __init__(self, enabled=True):
        self.enabled, self.cache = enabled, {}

    def get(self, symbols):
        out, want = {}, []
        now = time.time()
        for s in symbols:
            hit = self.cache.get(s)
            if hit and now - hit[0] < QUOTE_EVERY:
                out[s] = hit[1]
            else:
                want.append(s)
        if self.enabled and want:
            for code in ("us", "nse"):
                group = [s for s in want if alerts.market_for(s) == code]
                if not group:
                    continue
                alerts.use_market(code)
                try:
                    got, _ = alerts.get_quotes(group, "yahoo", None, pause=0.2)
                except Exception:          # a quote is a nice-to-have: never break the page
                    got = {}
                finally:
                    alerts.use_market("us")
                for s, q in got.items():
                    self.cache[s] = (now, q)
                    out[s] = q
        return out


def clocks():
    out = []
    for code in ("us", "nse"):
        alerts.use_market(code)
        try:
            st = alerts.market_status(alerts.parse_now(None))
        finally:
            alerts.use_market("us")
        out.append({"code": code, "exchange": "NYSE" if code == "us" else "NSE",
                    "place": "New York" if code == "us" else "India (IST)", "now": st["now_new_york"],
                    "date": st["date"], "open": st["open"], "reason": st["reason"], "opens": st.get("opens"),
                    "closes": st.get("closes"), "next_open": st.get("next_open"),
                    "early_close": st.get("early_close")})
    return out


def order_rows(folder, orders_doc, risk, cfg):
    """Each proposed order with trader-risk's verdict and trader-broker's status, in its market."""
    orders = (orders_doc or {}).get("orders") or []
    if not orders:
        return [], None
    first = orders[0] if isinstance(orders[0], dict) else {}
    code = alerts.market_for(first.get("symbol"), (orders_doc or {}).get("market") or first.get("market"))
    ticket.use_market(code)
    try:
        state_path = ticket.market_path(os.path.join("trader", "state", "broker.json"))
        state = read(os.path.join(folder, state_path)) or {"orders": []}
        today = ticket.to_eastern(ticket.parse_now(None)).date().isoformat()
        rows = []
        for i, o in enumerate(orders, 1):
            ready, label, rv = ticket.status_of(o, risk, state, today, orders)
            tool, args = ticket.tool_of(o)
            rows.append({"index": i, "plain": ticket.localize(ticket.plain(o)), "symbol": o.get("symbol"),
                         "side": o.get("side"), "ready": ready, "status": ticket.localize(label),
                         "verdict": (rv or {}).get("verdict"), "reasons": (rv or {}).get("reasons") or [],
                         "warnings": (rv or {}).get("warnings") or [], "tool": tool, "tool_input": args,
                         "why": ticket.localize(o.get("why") or "")})
    finally:
        ticket.use_market("us")
    return rows, code


def portfolio_rows(folder, quotes):
    out = []
    for code, name in (("us", "portfolio.json"), ("nse", "portfolio.nse.json")):
        pf = read(os.path.join(folder, "trader", name))
        if not isinstance(pf, dict):
            continue
        pos = []
        for p in pf.get("positions") or []:
            sym = str(p.get("ticker") or p.get("symbol") or "").upper()
            shares = float(p.get("shares") or 0)
            if not sym or abs(shares) < 1e-9:
                continue
            cost = sum(float(l.get("cost") or 0) for l in p.get("lots") or []) or None
            q = quotes.get(sym)
            last = q["price"] if q else None
            value = round(last * shares, 2) if last is not None else None
            pos.append({"symbol": sym, "shares": shares, "cost": cost, "last": last, "value": value,
                        "pnl": round(value - cost, 2) if value is not None and cost else None})
        out.append({"market": code, "file": "trader/" + name, "cash": pf.get("cash"), "positions": pos,
                    "currency": "INR" if code == "nse" else "USD"})
    return out


def broker_log(folder):
    out = []
    for code, name in (("us", "broker.json"), ("nse", "broker.nse.json")):
        st = read(os.path.join(folder, "trader", "state", name)) or {}
        recs = st.get("orders") or []
        if not recs:
            continue
        ticket.use_market(code)
        try:
            today = ticket.to_eastern(ticket.parse_now(None)).date().isoformat()
        finally:
            ticket.use_market("us")
        for r in recs:
            if ticket.rec_day(r) == today:
                plain = r.get("plain") or "%s %s %s" % (r.get("side"), r.get("qty"), r.get("symbol"))
                if code == "nse":                      # the log stores the words as written, in dollars
                    plain = plain.replace("$", "\u20b9")
                out.append({"market": code, "at": r.get("at"), "status": r.get("status"), "plain": plain,
                            "reply": r.get("reply"), "max_loss": r.get("max_loss"),
                            "broker_order_id": r.get("broker_order_id"), "note": r.get("note")})
    return out


def state(folder, quotes):
    """Everything the page shows, as one JSON object."""
    t = lambda *p: os.path.join(folder, "trader", *p)
    cfg = read(t("config.json")) or {}
    plan = read(t("plan.json"))
    entry = read(t("entry.json"))
    tm = read(t("timemachine.json"))
    orders_doc = read(t("orders.json"))
    risk = read(t("state", "risk.json"))
    rules_doc = read(t("alerts.json")) or {}
    rung = (read(t("state", "alerts.json")) or {}).get("rung") or {}
    watch = read(t("watchlist.json"))
    prices = read(t("prices.json")) or {}
    with LOCK:
        orders, orders_market = order_rows(folder, orders_doc, risk, cfg)
        wanted = []
        if isinstance(plan, dict) and plan.get("symbol"):
            wanted.append(str(plan["symbol"]).upper())
        wanted += [str(o["symbol"]).upper() for o in orders if o.get("symbol")]
        for code, name in (("us", "portfolio.json"), ("nse", "portfolio.nse.json")):
            pf = read(t(name)) or {}
            wanted += [str(p.get("ticker") or p.get("symbol") or "").upper() for p in pf.get("positions") or []]
        wanted = [s for i, s in enumerate(wanted) if s and s not in wanted[:i]]
        live = quotes.get(wanted)
        q = {}
        for s in wanted:                        # live quote first, else what trader-alerts last wrote
            if s in live:
                q[s] = {"price": live[s]["price"], "prev_close": live[s].get("prev_close"),
                        "time": live[s].get("time"), "source": "live"}
            elif s in (prices.get("prices") or {}):
                d = (prices.get("detail") or {}).get(s) or {}
                q[s] = {"price": prices["prices"][s], "prev_close": d.get("prev_close"),
                        "time": d.get("time") or prices.get("as_of"), "source": "trader/prices.json"}
        portfolios = portfolio_rows(folder, q)
        log = broker_log(folder)
        mk = clocks()
    plan_out = None
    if isinstance(plan, dict) and plan.get("symbol"):
        sym = str(plan["symbol"]).upper()
        plan_out = dict(plan, market=plan.get("market") or alerts.market_for(sym),
                        quote=q.get(sym), file_time=mtime(t("plan.json")))
    tm_out = None
    if isinstance(tm, dict) and tm.get("mc"):
        m, h = tm["mc"], tm.get("history") or {}
        tm_out = {"symbol": (tm.get("plan") or {}).get("symbol"), "date": (tm.get("plan") or {}).get("date"),
                  "market": tm.get("market") or "us", "verdict": tm.get("verdict"),
                  "notes": tm.get("verdict_notes") or [], "p_target": m.get("p_target"), "p_stop": m.get("p_stop"),
                  "p_neither": m.get("p_neither"), "p_green": m.get("p_green"), "median": m.get("median_close"),
                  "p5": m.get("p5_close"), "p95": m.get("p95_close"), "avg_R": m.get("avg_R"), "seed": m.get("seed"),
                  "replays": m.get("replays"), "until": m.get("until"), "sessions": m.get("pool_sessions"),
                  "hist": m.get("hist"), "history": {k: h.get(k) for k in ("sessions", "triggered", "target",
                                                                             "stop", "close", "avg_R")}}
    rules = []
    for r in rules_doc.get("rules") or []:
        rid = r.get("id") or "%s-%s" % (r.get("symbol", "market"), r.get("type"))
        rules.append({"id": rid, "symbol": r.get("symbol"), "type": r.get("type"), "price": r.get("price"),
                      "at": r.get("at"), "pct": r.get("pct"), "date": r.get("date"), "note": r.get("note"),
                      "market": alerts.market_for(r.get("symbol"), r.get("market")), "rung": rung.get(rid)})
    hits = []
    if isinstance(watch, dict):
        for h in (watch.get("hits") or [])[:30]:
            hits.append({k: h.get(k) for k in ("symbol", "setups", "price", "at", "or_high", "or_low", "pct_over",
                                               "close_date", "rsi", "volume") if h.get(k) is not None})
    return {
        "generated_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "folder": os.path.abspath(folder),
        "paper": bool(cfg.get("paper", True)),
        "markets": mk,
        "plan": plan_out,
        "entry": entry if isinstance(entry, dict) else None,
        "timemachine": tm_out,
        "orders": orders, "orders_market": orders_market,
        "risk": {"checked_at": (risk or {}).get("checked_at"), "pass": (risk or {}).get("pass"),
                 "block": (risk or {}).get("block"), "warnings": (risk or {}).get("warnings") or [],
                 "today": (risk or {}).get("today"), "market": (risk or {}).get("market")} if risk else None,
        "alerts": rules,
        "portfolios": portfolios,
        "log": log,
        "watchlist": {"mode": watch.get("mode"), "market": watch.get("market") or "us",
                      "created_at": watch.get("created_at"), "count": len(watch.get("hits") or []), "hits": hits}
        if isinstance(watch, dict) else None,
        "quotes": q,
    }


def page(snapshot=None):
    """The page. With snapshot (a state dict) the data is baked in and the page does not poll."""
    with open(os.path.join(HERE, "dashboard.html"), encoding="utf-8") as fh:
        text = fh.read()
    data = "null" if snapshot is None else json.dumps(snapshot).replace("</", "<\\/")
    return text.replace("/*__SNAPSHOT__*/null", data)


def make_handler(folder, quotes):
    class Handler(BaseHTTPRequestHandler):
        def _send(self, code, body, ctype):
            data = body.encode("utf-8")
            self.send_response(code)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(data)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):
            path = self.path.split("?")[0]
            if path in ("/", "/index.html"):
                self._send(200, page(), "text/html; charset=utf-8")
            elif path == "/api/state":
                try:
                    self._send(200, json.dumps(state(folder, quotes)), "application/json")
                except Exception as e:            # show the problem on the page, keep serving
                    self._send(500, json.dumps({"error": "%s: %s" % (type(e).__name__, e)}), "application/json")
            else:
                self._send(404, "not found", "text/plain")

        def log_message(self, fmt, *args):
            pass

    return Handler


def main(argv=None):
    ap = argparse.ArgumentParser(description="Trader dashboard: everything in trader/, live and read only.")
    sub = ap.add_subparsers(dest="cmd")
    for name, helptext in (("serve", "serve the live page on 127.0.0.1"), ("snapshot", "write one static page"),
                           ("state", "print the data as JSON")):
        p = sub.add_parser(name, help=helptext)
        p.add_argument("--folder", default=".", help="the project folder that holds trader/ (default .)")
        p.add_argument("--no-quotes", action="store_true", help="no live quotes: trader/prices.json only")
        if name == "serve":
            p.add_argument("--port", type=int, default=8765)
        if name == "snapshot":
            p.add_argument("--out", default="trader/dashboard.html")
    a = ap.parse_args(argv)
    if not a.cmd:
        ap.print_help()
        return 1
    if not os.path.isdir(os.path.join(a.folder, "trader")):
        print("dashboard.py: no trader/ folder in %s yet: run a skill first (or pass --folder)"
              % os.path.abspath(a.folder), file=sys.stderr)
        return 1
    quotes = Quotes(not a.no_quotes)
    if a.cmd == "state":
        print(json.dumps(state(a.folder, quotes), indent=2))
        return 0
    if a.cmd == "snapshot":
        out = a.out if os.path.isabs(a.out) else os.path.join(a.folder, a.out)
        with open(out, "w", encoding="utf-8") as fh:
            fh.write(page(state(a.folder, quotes)))
        print("Wrote %s. Open it in a browser; it shows the data as of now and does not refresh." % out)
        return 0
    server = ThreadingHTTPServer(("127.0.0.1", a.port), make_handler(a.folder, quotes))
    print("Trader dashboard on http://127.0.0.1:%d (read only, refreshes every 15 s). Ctrl+C stops it." % a.port)
    print("Reading %s" % html.escape(os.path.abspath(os.path.join(a.folder, "trader"))), flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    sys.exit(main())

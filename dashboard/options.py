#!/usr/bin/env python3
"""Option chain for NSE (NFO) and BSE (BFO) options, with rule-based entry and exit signals.
Standard library only. It reads prices and writes trader/options.json; it never places an order.

    python3 dashboard/options.py chain NIFTY                     # live chain from Kite Connect + signal
    python3 dashboard/options.py chain BANKNIFTY --expiry 2026-11-25 --strikes 8 --write
    python3 dashboard/options.py chain NIFTY --csv option-chain-ED-NIFTY-27-Nov-2026.csv --lot 75 --write
    python3 dashboard/options.py track                           # follow the current signal for exits
    python3 dashboard/options.py track --side CE --strike 25800 --premium 118.5 --lots 1
    python3 dashboard/options.py untrack
    python3 dashboard/options.py login                           # prints the Kite login link
    python3 dashboard/options.py login --request-token XXXX      # turns it into today's access token

Where the chain comes from:
  kite  Zerodha Kite Connect (api.kite.trade): live LTP, OI, volume and depth for every strike.
        Needs KITE_API_KEY and KITE_ACCESS_TOKEN in the environment, or trader/state/kite_session.json
        from `login` (a token lasts until about 6 a.m. the next day). Kite Connect's market data is part
        of its paid plan. Underlyings: NIFTY, BANKNIFTY, FINNIFTY, MIDCPNIFTY, SENSEX (BFO) and any F&O
        stock (RELIANCE, TCS, ...).
  csv   the CSV that nseindia.com's option chain page downloads. No key needed; the chain is as of
        the download, and the spot price comes from Yahoo. NSE refuses scripted requests, so this tool
        does not fetch NSE's site itself.

What it works out: spot, ATM strike, put-call ratio, max pain, the call OI wall above (resistance)
and the put OI wall below (support), OI added near the money, ATM implied volatility (Black-Scholes)
and the straddle's expected move. The signal (BUY CE, BUY PE, WAIT or NO TRADE) needs the 15-minute
opening range of the underlying to break, plus the chain agreeing, plus room to the next OI wall; it
names the ATM option, a premium stop and target (estimated with delta), and lots for your max loss.

Rule-based tooling, not a prediction and not financial advice. Option buyers lose the whole premium
often; an estimate made with delta is off when volatility moves.
"""

import argparse
import csv
import datetime
import gzip
import hashlib
import importlib.util
import io
import json
import math
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
IST = datetime.timezone(datetime.timedelta(hours=5, minutes=30))
KITE = "https://api.kite.trade"


def _load(folder, name):
    spec = importlib.util.spec_from_file_location("opt_" + name, os.path.join(ROOT, folder, name + ".py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


alerts = _load("trader-alerts", "alerts")

# underlying -> (options exchange, Kite spot instrument, Yahoo symbol for the opening range)
UNDERLYINGS = {
    "NIFTY": ("NFO", "NSE:NIFTY 50", "^NSEI"),
    "BANKNIFTY": ("NFO", "NSE:NIFTY BANK", "^NSEBANK"),
    "FINNIFTY": ("NFO", "NSE:NIFTY FIN SERVICE", "NIFTY_FIN_SERVICE.NS"),
    "MIDCPNIFTY": ("NFO", "NSE:NIFTY MID SELECT", "^NSEMDCP50"),
    "SENSEX": ("BFO", "BSE:SENSEX", "^BSESN"),
}
ALIASES = {"NIFTY50": "NIFTY", "NIFTY 50": "NIFTY", "^NSEI": "NIFTY", "BANK NIFTY": "BANKNIFTY",
           "NIFTYBANK": "BANKNIFTY", "^NSEBANK": "BANKNIFTY", "^BSESN": "SENSEX"}

DEFAULTS = {
    "strikes": 10,              # strikes shown each side of the ATM strike
    "window": 5,                # strikes each side counted as "near the money"
    "rate": 0.065,              # risk-free rate for implied volatility (about the 91-day T-bill)
    "or_minutes": 15,           # opening range: 09:15 to 09:30 IST
    "entries_from": "09:30",
    "entries_until": "14:30",   # options lose time value fast late in the day
    "flat_by": "15:15",
    "min_rr": 1.5,              # the next OI wall must be at least this many R away
    "pcr_bull": 1.2,            # put writers outnumber call writers: support under the price
    "pcr_bear": 0.8,
    "max_loss": None,           # rupees; default: the "nse" block's max_loss_per_trade_usd
}


class OptionsError(Exception):
    pass


# ---------------------------------------------------------------- small helpers

def underlying(sym):
    s = str(sym or "").upper().strip()
    s = ALIASES.get(s, s)
    for suf in (".NS", ".BO"):
        if s.endswith(suf):
            s = s[:-len(suf)]
    if not re.match(r"^[A-Z0-9][A-Z0-9&\-]{0,24}$", s):
        raise OptionsError("%r is not an NSE or BSE symbol" % sym)
    return s


def spec_for(sym):
    s = underlying(sym)
    return UNDERLYINGS.get(s) or ("NFO", "NSE:" + s, s + ".NS")


def is_index(sym):
    return underlying(sym) in UNDERLYINGS


def num(x):
    """'1,234.50' -> 1234.5; '-', '' and None -> None."""
    if x is None:
        return None
    if isinstance(x, (int, float)):
        return float(x)
    s = str(x).replace(",", "").strip()
    if s in ("", "-", "--"):
        return None
    try:
        return float(s)
    except ValueError:
        return None


def tick(x, step=0.05):
    return None if x is None else round(round(x / step) * step, 2)


def hm(text):
    h, m = str(text).split(":")
    return datetime.time(int(h), int(m))


def now_ist(now=None):
    if now is None:
        return datetime.datetime.now(IST)
    if isinstance(now, str):
        return datetime.datetime.strptime(now, "%Y-%m-%d %H:%M").replace(tzinfo=IST)
    return now.astimezone(IST)


def settings(cfg):
    out = dict(DEFAULTS)
    oc = cfg.get("options") if isinstance(cfg.get("options"), dict) else {}
    out.update({k: v for k, v in oc.items() if k in DEFAULTS})
    if out["max_loss"] is None:
        nse = cfg.get("nse") if isinstance(cfg.get("nse"), dict) else {}
        out["max_loss"] = nse.get("max_loss_per_trade_usd")
    return out


# ---------------------------------------------------------------- Black-Scholes

def _ncdf(x):
    return 0.5 * (1 + math.erf(x / math.sqrt(2)))


def bs_price(S, K, T, r, sigma, kind):
    if T <= 0 or sigma <= 0:
        return max(0.0, S - K) if kind == "CE" else max(0.0, K - S)
    d1 = (math.log(S / K) + (r + sigma * sigma / 2) * T) / (sigma * math.sqrt(T))
    d2 = d1 - sigma * math.sqrt(T)
    if kind == "CE":
        return S * _ncdf(d1) - K * math.exp(-r * T) * _ncdf(d2)
    return K * math.exp(-r * T) * _ncdf(-d2) - S * _ncdf(-d1)


def implied_vol(price, S, K, T, r, kind):
    """Annual volatility that prices the option at `price`, or None (below intrinsic, no time)."""
    if not price or price <= 0 or not S or T <= 0:
        return None
    if price < bs_price(S, K, T, r, 1e-6, kind) - 0.05:
        return None
    lo, hi = 0.005, 4.0
    for _ in range(80):
        mid = (lo + hi) / 2
        if bs_price(S, K, T, r, mid, kind) > price:
            hi = mid
        else:
            lo = mid
    v = (lo + hi) / 2
    return None if v >= 3.99 else v


def delta(S, K, T, r, sigma, kind):
    if not sigma or T <= 0:
        return (1.0 if S > K else 0.0) if kind == "CE" else (-1.0 if S < K else 0.0)
    d1 = (math.log(S / K) + (r + sigma * sigma / 2) * T) / (sigma * math.sqrt(T))
    return _ncdf(d1) if kind == "CE" else _ncdf(d1) - 1


def years_to_expiry(expiry, now):
    """Expiry at 15:30 IST; at least 15 minutes so a same-day option still has a price model."""
    if not expiry:
        return None
    end = datetime.datetime.combine(datetime.date.fromisoformat(expiry), datetime.time(15, 30), IST)
    return max((end - now).total_seconds(), 900) / (365 * 86400)


# ---------------------------------------------------------------- sources

class Kite(object):
    """Kite Connect REST, read only: instruments and quotes. `get` is swapped in tests."""

    def __init__(self, api_key, access_token, get=None):
        self.api_key, self.token = api_key, access_token
        self._get = get or self._http
        self.live = get is None
        self._instruments = {}

    @classmethod
    def from_env(cls, folder=".", today=None, get=None):
        key, token = os.environ.get("KITE_API_KEY"), os.environ.get("KITE_ACCESS_TOKEN")
        if not (key and token):
            s = _read(os.path.join(folder, "trader", "state", "kite_session.json")) or {}
            if s.get("date") == (today or now_ist().date().isoformat()):
                key, token = key or s.get("api_key"), s.get("access_token")
        return cls(key, token, get) if key and token else None

    def _http(self, path):
        req = urllib.request.Request(KITE + path, headers={
            "X-Kite-Version": "3", "Authorization": "token %s:%s" % (self.api_key, self.token)})
        try:
            with urllib.request.urlopen(req, timeout=30) as r:
                body = r.read()
                if r.headers.get("Content-Encoding") == "gzip":
                    body = gzip.decompress(body)
                return body.decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            try:
                msg = json.loads(e.read().decode("utf-8", "replace")).get("message")
            except Exception:
                msg = None
            if e.code == 403:
                raise OptionsError("Kite refused the token (%s): run `options.py login` again" % (msg or 403))
            raise OptionsError("Kite answered HTTP %d%s" % (e.code, ": " + msg if msg else ""))
        except OSError as e:
            raise OptionsError("cannot reach Kite: %s" % e)

    def instruments(self, exchange):
        if exchange not in self._instruments:
            rows = list(csv.DictReader(io.StringIO(self._get("/instruments/" + exchange))))
            self._instruments[exchange] = [r for r in rows if r.get("instrument_type") in ("CE", "PE")]
        return self._instruments[exchange]

    def quote(self, keys):
        out = {}
        for i in range(0, len(keys), 500):
            part = keys[i:i + 500]
            if i:
                time.sleep(1)
            doc = json.loads(self._get("/quote?" + urllib.parse.urlencode([("i", k) for k in part])))
            if doc.get("status") != "success":
                raise OptionsError("Kite quote failed: %s" % doc.get("message"))
            out.update(doc.get("data") or {})
        return out


def expiries(kite, sym):
    exch = spec_for(sym)[0]
    name = underlying(sym)
    return sorted({r["expiry"] for r in kite.instruments(exch) if r.get("name") == name and r.get("expiry")})


def chain_from_kite(kite, sym, expiry=None, now=None, around=40):
    """The chain for one expiry: every strike within `around` strikes of the spot."""
    now = now_ist(now)
    exch, spot_key, _ = spec_for(sym)
    name = underlying(sym)
    rows = [r for r in kite.instruments(exch) if r.get("name") == name]
    if not rows:
        raise OptionsError("Kite lists no %s options for %s: is it in F&O?" % (exch, name))
    exps = sorted({r["expiry"] for r in rows if r["expiry"] >= now.date().isoformat()})
    if not exps:
        raise OptionsError("no live %s expiry left" % name)
    if expiry and expiry not in exps:
        raise OptionsError("%s has no %s expiry; the next ones are %s" % (name, expiry, ", ".join(exps[:4])))
    expiry = expiry or exps[0]
    rows = [r for r in rows if r["expiry"] == expiry]
    sq = kite.quote([spot_key]).get(spot_key)
    if not sq:
        raise OptionsError("Kite has no quote for %s" % spot_key)
    spot = float(sq["last_price"])
    strikes = sorted({float(r["strike"]) for r in rows})
    atm_i = min(range(len(strikes)), key=lambda i: abs(strikes[i] - spot))
    keep = set(strikes[max(0, atm_i - around):atm_i + around + 1])
    rows = [r for r in rows if float(r["strike"]) in keep]
    if kite.live:
        time.sleep(1)                                       # Kite allows about one quote call a second
    q = kite.quote(["%s:%s" % (exch, r["tradingsymbol"]) for r in rows])
    by = {}
    for r in rows:
        d = q.get("%s:%s" % (exch, r["tradingsymbol"])) or {}
        depth = d.get("depth") or {}
        bid = ((depth.get("buy") or [{}])[0]).get("price") or None
        ask = ((depth.get("sell") or [{}])[0]).get("price") or None
        leg = {"tradingsymbol": r["tradingsymbol"], "ltp": num(d.get("last_price")), "oi": num(d.get("oi")),
               "chg_oi": None, "volume": num(d.get("volume")), "iv": None, "bid": bid, "ask": ask}
        by.setdefault(float(r["strike"]), {})[r["instrument_type"].lower()] = leg
    lot = int(float(rows[0].get("lot_size") or 0)) or None
    return {"symbol": name, "exchange": exch, "source": "kite", "expiry": expiry, "expiries": exps[:6],
            "spot": spot, "prev_close": num((sq.get("ohlc") or {}).get("close")), "lot": lot,
            "as_of": now.strftime("%Y-%m-%d %H:%M"),
            "rows": [dict(strike=k, ce=v.get("ce"), pe=v.get("pe")) for k, v in sorted(by.items())]}


CSV_FIELDS = {"OI": "oi", "CHNG IN OI": "chg_oi", "VOLUME": "volume", "IV": "iv", "LTP": "ltp", "BID": "bid",
              "ASK": "ask", "BID PRICE": "bid", "ASK PRICE": "ask"}


def chain_from_nse_csv(path, sym=None, expiry=None):
    """nseindia.com's option chain download: CALLS on the left of STRIKE, PUTS mirrored on the right."""
    with open(path, encoding="utf-8-sig") as fh:
        table = list(csv.reader(fh))
    head = next((i for i, r in enumerate(table) if "STRIKE" in [c.strip().upper() for c in r]), None)
    if head is None:
        raise OptionsError("%s has no STRIKE column: is it NSE's option chain CSV?" % path)
    cols = [c.strip().upper() for c in table[head]]
    k = cols.index("STRIKE")
    left = {CSV_FIELDS[c]: i for i, c in enumerate(cols[:k]) if c in CSV_FIELDS}
    right = {CSV_FIELDS[c]: k + 1 + i for i, c in enumerate(cols[k + 1:]) if c in CSV_FIELDS}
    rows = []
    for r in table[head + 1:]:
        if len(r) <= k or num(r[k]) is None:
            continue
        leg = lambda m: {f: num(r[i]) if i < len(r) else None for f, i in m.items()}
        ce, pe = leg(left), leg(right)
        for x in (ce, pe):
            if x.get("iv") is not None:
                x["iv"] /= 100.0
        rows.append({"strike": num(r[k]), "ce": ce, "pe": pe})
    if not rows:
        raise OptionsError("%s has no strikes" % path)
    base = os.path.basename(path)
    if not sym:
        m = re.search(r"option-chain-[A-Z]+-([A-Z0-9&]+)", base, re.I)
        sym = m.group(1) if m else None
    if not expiry:
        m = re.search(r"(\d{1,2})-([A-Za-z]{3})-(\d{4})", base)
        if m:
            expiry = datetime.datetime.strptime("-".join(m.groups()), "%d-%b-%Y").date().isoformat()
    if not sym:
        raise OptionsError("which underlying is %s? pass the symbol: chain NIFTY --csv ..." % base)
    stamp = datetime.datetime.fromtimestamp(os.path.getmtime(path), IST).strftime("%Y-%m-%d %H:%M")
    return {"symbol": underlying(sym), "exchange": spec_for(sym)[0], "source": "nse-csv", "expiry": expiry,
            "expiries": [expiry] if expiry else [], "spot": None, "prev_close": None, "lot": None,
            "as_of": stamp, "file": base, "rows": sorted(rows, key=lambda x: x["strike"])}


def yahoo_bars(ysym, get=None):
    """Today's 5-minute bars from Yahoo: [(IST datetime, open, high, low, close)] and the meta block."""
    url = "https://query2.finance.yahoo.com/v8/finance/chart/%s?range=1d&interval=5m" % urllib.parse.quote(ysym)
    if get:
        text = get(url)
    else:
        code, text = alerts._get(url)
        if code != 200:
            raise OptionsError("Yahoo answered HTTP %d for %s" % (code, ysym))
    try:
        res = json.loads(text)["chart"]["result"][0]
    except (ValueError, KeyError, TypeError, IndexError):
        raise OptionsError("Yahoo has no bars for %s" % ysym)
    q = (res.get("indicators", {}).get("quote") or [{}])[0]
    out = []
    for i, t in enumerate(res.get("timestamp") or []):
        o, h, l, c = (q.get(k, [None] * (i + 1))[i] for k in ("open", "high", "low", "close"))
        if None not in (o, h, l, c):
            out.append((datetime.datetime.fromtimestamp(t, IST), o, h, l, c))
    return out, res.get("meta") or {}


def opening_range(bars, day, minutes=15):
    start = datetime.datetime.combine(day, datetime.time(9, 15), IST)
    end = start + datetime.timedelta(minutes=minutes)
    inside = [b for b in bars if start <= b[0] < end]
    if not inside or bars[-1][0] < end - datetime.timedelta(minutes=5):
        return None
    hi, lo = max(b[2] for b in inside), min(b[3] for b in inside)
    return {"high": hi, "low": lo, "mid": round((hi + lo) / 2, 2), "until": end.strftime("%H:%M")}


# ---------------------------------------------------------------- analysis

def analyse(ch, now=None, opts=None, oi_base=None):
    """Spot-relative numbers for a chain. oi_base {tradingsymbol: oi} gives OI change when the source has none."""
    o = opts or DEFAULTS
    now = now_ist(now)
    rows, spot = ch["rows"], ch.get("spot")
    if not spot:
        raise OptionsError("no spot price for %s" % ch["symbol"])
    strikes = [r["strike"] for r in rows]
    step = sorted(b - a for a, b in zip(strikes, strikes[1:]))[len(strikes) // 2 - 1] if len(strikes) > 2 else None
    atm = min(strikes, key=lambda s: abs(s - spot))
    T = years_to_expiry(ch.get("expiry"), now)
    for r in rows:
        for kind in ("ce", "pe"):
            leg = r.get(kind)
            if not leg:
                continue
            if oi_base and leg.get("chg_oi") is None and leg.get("tradingsymbol") in oi_base and leg.get("oi") is not None:
                leg["chg_oi"] = leg["oi"] - oi_base[leg["tradingsymbol"]]
            if leg.get("iv") is None and T and leg.get("ltp"):
                leg["iv"] = implied_vol(leg["ltp"], spot, r["strike"], T, o["rate"], kind.upper())
    oi = lambda r, k: (r.get(k) or {}).get("oi") or 0
    ce_oi, pe_oi = sum(oi(r, "ce") for r in rows), sum(oi(r, "pe") for r in rows)
    pain = None
    if ce_oi or pe_oi:
        pain = min(strikes, key=lambda k: sum(oi(r, "ce") * max(0, k - r["strike"]) + oi(r, "pe") * max(0, r["strike"] - k)
                                             for r in rows))
    above = [r for r in rows if r["strike"] > spot and oi(r, "ce")]
    below = [r for r in rows if r["strike"] < spot and oi(r, "pe")]
    resistance = max(above, key=lambda r: oi(r, "ce"))["strike"] if above else None
    support = max(below, key=lambda r: oi(r, "pe"))["strike"] if below else None
    ai = strikes.index(atm)
    near = rows[max(0, ai - o["window"]):ai + o["window"] + 1]
    chg = lambda k: [(r.get(k) or {}).get("chg_oi") for r in near]
    has_chg = any(x is not None for x in chg("ce") + chg("pe"))
    near_ce = sum(x or 0 for x in chg("ce")) if has_chg else None
    near_pe = sum(x or 0 for x in chg("pe")) if has_chg else None
    a = next(r for r in rows if r["strike"] == atm)
    straddle = ((a.get("ce") or {}).get("ltp") or 0) + ((a.get("pe") or {}).get("ltp") or 0)
    ivs = [x for x in ((a.get("ce") or {}).get("iv"), (a.get("pe") or {}).get("iv")) if x]
    return {"spot": spot, "atm": atm, "step": step, "pcr": round(pe_oi / ce_oi, 2) if ce_oi else None,
            "call_oi": ce_oi, "put_oi": pe_oi, "max_pain": pain, "resistance": resistance, "support": support,
            "near_call_oi_change": near_ce, "near_put_oi_change": near_pe,
            "atm_iv": round(sum(ivs) / len(ivs), 4) if ivs else None,
            "straddle": round(straddle, 2) if straddle else None,
            "expected_move_pct": round(straddle / spot * 100, 2) if straddle else None,
            "days_to_expiry": round(T * 365, 2) if T else None,
            "expires_today": ch.get("expiry") == now.date().isoformat()}


def signal(ch, an, orng, now=None, opts=None, market_open=True):
    """BUY CE, BUY PE, WAIT or NO TRADE with the reasons, from the opening range and the chain."""
    o = opts or DEFAULTS
    now = now_ist(now)
    t = now.time()
    spot = an["spot"]
    sig = {"action": "NO TRADE", "votes": [], "reasons": [], "at": now.strftime("%Y-%m-%d %H:%M")}
    if not market_open:
        sig["reasons"].append("the market is closed")
        return sig
    if t >= hm(o["flat_by"]):
        sig.update(action="EXIT ALL", reasons=["after %s IST: be flat, no new trades" % o["flat_by"]])
        return sig
    if t < hm(o["entries_from"]) or not orng:
        sig.update(action="WAIT", reasons=["the opening range is set at %s IST" % o["entries_from"]])
        return sig
    score = 0
    if spot > orng["high"]:
        score += 1
        sig["votes"].append(["+1", "price %.2f is above the opening-range high %.2f" % (spot, orng["high"])])
        brk = 1
    elif spot < orng["low"]:
        score -= 1
        sig["votes"].append(["-1", "price %.2f is below the opening-range low %.2f" % (spot, orng["low"])])
        brk = -1
    else:
        sig["votes"].append(["0", "price is inside the opening range %.2f-%.2f" % (orng["low"], orng["high"])])
        brk = 0
    pcr = an.get("pcr")
    if pcr is not None:
        if pcr >= o["pcr_bull"]:
            score += 1
            sig["votes"].append(["+1", "put-call ratio %.2f: puts written under the price" % pcr])
        elif pcr <= o["pcr_bear"]:
            score -= 1
            sig["votes"].append(["-1", "put-call ratio %.2f: calls written over the price" % pcr])
        else:
            sig["votes"].append(["0", "put-call ratio %.2f is neutral" % pcr])
    c, p = an.get("near_call_oi_change"), an.get("near_put_oi_change")
    if c is not None and p is not None and (abs(c) + abs(p)) > 0:
        tilt = (p - c) / (abs(c) + abs(p))
        if tilt >= 0.2:
            score += 1
            sig["votes"].append(["+1", "near the money, put OI grew more than call OI (%+.0f vs %+.0f)" % (p, c)])
        elif tilt <= -0.2:
            score -= 1
            sig["votes"].append(["-1", "near the money, call OI grew more than put OI (%+.0f vs %+.0f)" % (c, p)])
        else:
            sig["votes"].append(["0", "OI added on both sides about equally"])
    sig["score"] = score
    if t > hm(o["entries_until"]):
        sig.update(action="NO TRADE", reasons=["after %s IST: too little day left for a new option buy"
                                               % o["entries_until"]])
        return sig
    if brk == 0:
        sig.update(action="WAIT", reasons=["wait for a close outside the opening range"])
        return sig
    if brk * score < 2:
        sig["reasons"].append("the breakout is %s but the chain does not agree (score %+d, needs %+d)"
                              % ("up" if brk > 0 else "down", score, 2 * brk))
        return sig
    kind = "CE" if brk > 0 else "PE"
    u_stop = orng["mid"]
    risk = abs(spot - u_stop)
    wall = an.get("resistance") if kind == "CE" else an.get("support")
    if wall is not None and abs(wall - spot) < o["min_rr"] * risk and (wall - spot) * brk > 0:
        sig.update(action="WAIT", reasons=["the %s OI wall at %s is only %.1fR away: too close for %.1fR"
                                           % ("call" if kind == "CE" else "put", _f(wall), abs(wall - spot) / risk,
                                              o["min_rr"])])
        return sig
    if wall is not None and (wall - spot) * brk > 0 and abs(wall - spot) <= 3 * risk:
        u_target, label = wall, "the %s OI wall" % ("call" if kind == "CE" else "put")
    else:
        u_target, label = spot + brk * 2 * risk, "2R"
    row = next((r for r in ch["rows"] if r["strike"] == an["atm"]), None)
    leg = (row or {}).get(kind.lower()) or {}
    prem = leg.get("ask") or leg.get("ltp")
    sig.update(action="BUY " + kind, reasons=["%s setup: breakout %s and the chain agrees (score %+d)"
                                              % ("BUY " + kind, "up" if brk > 0 else "down", score)])
    trade = {"side": kind, "strike": an["atm"], "expiry": ch.get("expiry"), "tradingsymbol": leg.get("tradingsymbol"),
             "under_entry": spot, "under_stop": u_stop, "under_target": round(u_target, 2), "target_label": label,
             "flat_by": o["flat_by"]}
    if prem:
        T = years_to_expiry(ch.get("expiry"), now)
        iv = leg.get("iv") or an.get("atm_iv")
        d = abs(delta(spot, an["atm"], T, o["rate"], iv, kind)) if iv else 0.5
        p_stop = tick(max(0.05, prem - d * risk))
        p_target = tick(prem + d * abs(u_target - spot))
        trade.update(premium=prem, premium_stop=p_stop, premium_target=p_target, delta=round(d, 2))
        lot = ch.get("lot")
        if lot and o.get("max_loss"):
            per_lot = (prem - p_stop) * lot
            trade["lots"] = int(o["max_loss"] // per_lot) if per_lot > 0 else 0
            trade["risk_per_lot"] = round(per_lot, 2)
            if trade["lots"] == 0:
                sig["reasons"].append("but one lot risks ₹%s at the stop, more than your max loss ₹%s: skip it"
                                      % (_f(per_lot), _f(o["max_loss"])))
                sig["action"] = "NO TRADE"
    if an.get("expires_today"):
        sig["reasons"].append("this option expires today: its premium can fall to zero within minutes")
    sig["trade"] = trade
    return sig


def exit_check(trade, spot, premium, now=None, opts=None):
    """HOLD or EXIT for a tracked option trade, with the reason."""
    o = opts or DEFAULTS
    now = now_ist(now)
    up = trade["side"] == "CE"
    why = []
    if now.time() >= hm(trade.get("flat_by") or o["flat_by"]):
        why.append("it is %s IST: flat by %s, win or lose" % (now.strftime("%H:%M"), trade.get("flat_by") or o["flat_by"]))
    if trade.get("expiry") and now.date().isoformat() > trade["expiry"]:
        why.append("the option has expired")
    if spot is not None:
        if (spot <= trade["under_stop"]) if up else (spot >= trade["under_stop"]):
            why.append("%s is back through the stop level %s" % (_f(spot), _f(trade["under_stop"])))
        if (spot >= trade["under_target"]) if up else (spot <= trade["under_target"]):
            why.append("%s reached the target level %s (%s)" % (_f(spot), _f(trade["under_target"]),
                                                               trade.get("target_label", "")))
    if premium is not None:
        if trade.get("premium_stop") is not None and premium <= trade["premium_stop"]:
            why.append("premium %s is at or below the stop %s" % (_f(premium), _f(trade["premium_stop"])))
        if trade.get("premium_target") is not None and premium >= trade["premium_target"]:
            why.append("premium %s reached the target %s" % (_f(premium), _f(trade["premium_target"])))
    pnl = None
    if premium is not None and trade.get("premium"):
        pnl = round((premium - trade["premium"]) * (trade.get("lot") or 0) * (trade.get("lots") or 0), 2) or None
    return {"action": "EXIT" if why else "HOLD", "reasons": why or ["inside the plan: stop and target not hit"],
            "spot": spot, "premium": premium, "pnl": pnl}


def _f(x):
    return "-" if x is None else ("%.2f" % x).rstrip("0").rstrip(".") if abs(x - round(x)) > 1e-9 else "%d" % round(x)


# ---------------------------------------------------------------- one call for the CLI and the dashboard

def build(sym, folder=".", kite=None, csv_path=None, expiry=None, lot=None, now=None, cfg=None,
          yahoo_get=None, spot_override=None, oi_base=None, chain=None):
    """The chain, its numbers, the opening range and the signal, as one dict for trader/options.json."""
    cfg = cfg if cfg is not None else (_read(os.path.join(folder, "trader", "config.json")) or {})
    o = settings(cfg)
    now = now_ist(now)
    if chain:
        ch = json.loads(json.dumps(chain))     # a saved chain, re-read against a newer spot
    elif csv_path:
        ch = chain_from_nse_csv(csv_path, sym, expiry)
    elif kite:
        ch = chain_from_kite(kite, sym, expiry, now)
    else:
        raise OptionsError("no Kite token and no --csv: set KITE_API_KEY and KITE_ACCESS_TOKEN "
                           "(or run `options.py login`), or download the option chain CSV from nseindia.com")
    if lot:
        ch["lot"] = int(lot)
    notes = []
    orng, bars_spot = None, None
    try:
        bars, meta = yahoo_bars(spec_for(ch["symbol"])[2], yahoo_get)
        orng = opening_range(bars, now.date(), o["or_minutes"])
        if bars:
            bars_spot = round(bars[-1][4], 2)
        if ch.get("prev_close") is None:
            ch["prev_close"] = meta.get("chartPreviousClose") or meta.get("previousClose")
    except (OptionsError, alerts.AlertError) as e:
        notes.append("no opening range: %s" % e)
    if spot_override:
        ch["spot"] = spot_override
    elif ch.get("spot") is None:
        ch["spot"] = bars_spot
        notes.append("spot from Yahoo's 5-minute bars (delayed); the chain is from %s, as of %s"
                     % (ch.get("file", "the CSV"), ch["as_of"]))
    an = analyse(ch, now, o, oi_base)
    alerts.use_market("nse")
    try:
        st = alerts.market_status(now.astimezone(datetime.timezone.utc).replace(tzinfo=None))
    finally:
        alerts.use_market("us")
    sig = signal(ch, an, orng, now, o, st["open"])
    if sig.get("trade"):
        sig["trade"]["lot"] = ch.get("lot")
    return {"symbol": ch["symbol"], "exchange": ch["exchange"], "source": ch["source"], "expiry": ch.get("expiry"),
            "expiries": ch.get("expiries"), "lot": ch.get("lot"), "as_of": ch["as_of"], "file": ch.get("file"),
            "computed_at": now.strftime("%Y-%m-%d %H:%M"), "spot": ch["spot"], "prev_close": ch.get("prev_close"),
            "opening_range": orng, "analysis": an, "signal": sig, "rows": ch["rows"], "notes": notes,
            "market": {"open": st["open"], "reason": st["reason"]}}


def window(doc, n):
    """The rows within n strikes of the ATM strike."""
    rows = doc["rows"]
    atm = doc["analysis"]["atm"]
    i = next(k for k, r in enumerate(rows) if r["strike"] == atm)
    return rows[max(0, i - n):i + n + 1]


def _read(path):
    try:
        with open(path, encoding="utf-8") as fh:
            return json.load(fh)
    except (OSError, ValueError):
        return None


def _write(path, doc, private=False):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as fh:
        json.dump(doc, fh, indent=2)
    if private:
        os.chmod(tmp, 0o600)
    os.replace(tmp, path)


def oi_base_for(old, doc_symbol, expiry, today):
    """The first OI seen today for this chain, so later reads show the OI added since."""
    if isinstance(old, dict) and old.get("symbol") == doc_symbol and old.get("expiry") == expiry \
            and (old.get("oi_base_at") or "")[:10] == today:
        return old.get("oi_base"), old.get("oi_base_at")
    return None, None


# ---------------------------------------------------------------- printing

def render(doc, n):
    an, sig = doc["analysis"], doc["signal"]
    out = ["%s %s options, expiry %s, %s (%s)" % (doc["symbol"], doc["exchange"], doc.get("expiry") or "?",
                                                   doc["as_of"], doc["source"]),
           "Spot %s   ATM %s   PCR %s   max pain %s   support %s   resistance %s" % (
               _f(doc["spot"]), _f(an["atm"]), an["pcr"], _f(an["max_pain"]), _f(an["support"]), _f(an["resistance"])),
           "ATM IV %s   straddle %s (expected move about %s%%)   %s days to expiry" % (
               "-" if an["atm_iv"] is None else "%.1f%%" % (an["atm_iv"] * 100), _f(an["straddle"]),
               an["expected_move_pct"], an["days_to_expiry"]), ""]
    head = "%10s %9s %8s %8s | %8s | %8s %8s %9s %10s" % ("CE OI", "chg OI", "IV", "LTP", "STRIKE", "LTP", "IV",
                                                          "chg OI", "PE OI")
    out += [head, "-" * len(head)]
    iv = lambda x: "-" if x is None else "%.1f" % (x * 100)
    for r in window(doc, n):
        c, p = r.get("ce") or {}, r.get("pe") or {}
        mark = "*" if r["strike"] == an["atm"] else " "
        out.append("%10s %9s %8s %8s |%s%8s | %8s %8s %9s %10s" % (
            _f(c.get("oi")), _f(c.get("chg_oi")), iv(c.get("iv")), _f(c.get("ltp")), mark, _f(r["strike"]),
            _f(p.get("ltp")), iv(p.get("iv")), _f(p.get("chg_oi")), _f(p.get("oi"))))
    out += ["", "Signal: %s" % sig["action"]]
    for v in sig.get("votes") or []:
        out.append("  %3s  %s" % tuple(v))
    for r in sig.get("reasons") or []:
        out.append("  - %s" % r)
    t = sig.get("trade")
    if t and sig["action"].startswith("BUY"):
        out.append("  %s %s %s: premium about %s, stop %s, target %s (delta %s); exit if %s %s %s or at %s IST"
                   % (doc["symbol"], _f(t["strike"]), t["side"], _f(t.get("premium")), _f(t.get("premium_stop")),
                      _f(t.get("premium_target")), t.get("delta"), doc["symbol"],
                      "falls to" if t["side"] == "CE" else "rises to", _f(t["under_stop"]), t["flat_by"]))
        if t.get("lots") is not None:
            out.append("  %d lot(s) of %s for your max loss (about ₹%s a lot at the stop)"
                       % (t["lots"], doc.get("lot"), _f(t.get("risk_per_lot"))))
    for x in doc.get("notes") or []:
        out.append("note: %s" % x)
    out.append("Rule-based signal, not advice. Your broker's price is the one that fills.")
    return "\n".join(out)


# ---------------------------------------------------------------- commands

def cmd_login(a):
    key = os.environ.get("KITE_API_KEY")
    if not key:
        print("Set KITE_API_KEY (and KITE_API_SECRET) from your Kite Connect app at developers.kite.trade first.",
              file=sys.stderr)
        return 1
    if not a.request_token:
        print("1. Open https://kite.zerodha.com/connect/login?v=3&api_key=%s and log in to Zerodha." % key)
        print("2. You land on your app's redirect URL; copy request_token=... from the address bar.")
        print("3. Run: python3 dashboard/options.py login --request-token THAT_VALUE")
        return 0
    secret = os.environ.get("KITE_API_SECRET")
    if not secret:
        print("Set KITE_API_SECRET in the environment (never in a file you commit).", file=sys.stderr)
        return 1
    body = urllib.parse.urlencode({"api_key": key, "request_token": a.request_token,
                                   "checksum": hashlib.sha256((key + a.request_token + secret).encode()).hexdigest()})
    req = urllib.request.Request(KITE + "/session/token", data=body.encode(), headers={"X-Kite-Version": "3"})
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            doc = json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        print("Kite refused the login (HTTP %d): request tokens work once and for a few minutes." % e.code,
              file=sys.stderr)
        return 1
    path = os.path.join(a.folder, "trader", "state", "kite_session.json")
    _write(path, {"api_key": key, "access_token": doc["data"]["access_token"],
                  "date": now_ist().date().isoformat()}, private=True)
    print("Saved today's Kite token to %s (readable by you only; keep trader/state out of git)." % path)
    return 0


def cmd_chain(a):
    cfg = _read(os.path.join(a.folder, "trader", "config.json")) or {}
    kite = None if a.csv else Kite.from_env(a.folder)
    path = os.path.join(a.folder, "trader", "options.json")
    old = _read(path)
    now = now_ist(a.now)
    base, base_at = (None, None)
    if old and not a.csv:
        base, base_at = oi_base_for(old, underlying(a.symbol), a.expiry or old.get("expiry"), now.date().isoformat())
    doc = build(a.symbol, a.folder, kite, a.csv, a.expiry, a.lot, now, cfg, spot_override=a.spot, oi_base=base)
    n = a.strikes or settings(cfg)["strikes"]
    print(render(doc, n))
    if a.write:
        if doc["source"] == "kite":
            if base is None:
                base = {leg["tradingsymbol"]: leg["oi"] for r in doc["rows"] for leg in (r.get("ce"), r.get("pe"))
                        if leg and leg.get("tradingsymbol") and leg.get("oi") is not None}
                base_at = doc["computed_at"]
            doc["oi_base"], doc["oi_base_at"] = base, base_at
        if isinstance(old, dict) and old.get("trade") and old["trade"].get("symbol") == doc["symbol"]:
            doc["trade"] = old["trade"]
        _write(path, doc)
        print("Wrote %s." % path)
    return 0


def cmd_track(a):
    path = os.path.join(a.folder, "trader", "options.json")
    doc = _read(path)
    if not doc:
        print("No trader/options.json yet: run `options.py chain SYMBOL --write` first.", file=sys.stderr)
        return 1
    if a.side:
        if a.strike is None or a.premium is None:
            print("--side needs --strike and --premium", file=sys.stderr)
            return 1
        sig_t = (doc.get("signal") or {}).get("trade") or {}
        orng = doc.get("opening_range") or {}
        up = a.side.upper() == "CE"
        t = {"side": a.side.upper(), "strike": a.strike, "expiry": doc.get("expiry"), "premium": a.premium,
             "under_entry": doc["spot"],
             "under_stop": a.under_stop or sig_t.get("under_stop") or orng.get("mid"),
             "under_target": a.under_target or sig_t.get("under_target"),
             "premium_stop": a.premium_stop, "premium_target": a.premium_target, "flat_by": settings({})["flat_by"]}
        if t["under_stop"] is None or t["under_target"] is None:
            print("give --under-stop and --under-target (no signal levels to borrow)", file=sys.stderr)
            return 1
        row = next((r for r in doc["rows"] if r["strike"] == a.strike), {})
        t["tradingsymbol"] = ((row.get("ce" if up else "pe")) or {}).get("tradingsymbol")
        t["target_label"] = "your target"
    else:
        sig = doc.get("signal") or {}
        if not sig.get("action", "").startswith("BUY") or not sig.get("trade"):
            print("The last signal is %s, not a BUY: pass --side, --strike and --premium to track your own trade."
                  % sig.get("action"), file=sys.stderr)
            return 1
        t = dict(sig["trade"])
    t.update(symbol=doc["symbol"], lot=doc.get("lot"), lots=a.lots or t.get("lots") or 1,
             tracked_at=now_ist(a.now).strftime("%Y-%m-%d %H:%M"))
    doc["trade"] = t
    _write(path, doc)
    print("Tracking %s %s %s from premium %s: the dashboard shows HOLD or EXIT. This records nothing at the broker."
          % (doc["symbol"], _f(t["strike"]), t["side"], _f(t.get("premium"))))
    return 0


def cmd_untrack(a):
    path = os.path.join(a.folder, "trader", "options.json")
    doc = _read(path)
    if doc and doc.pop("trade", None):
        _write(path, doc)
        print("Stopped tracking the option trade.")
    else:
        print("Nothing tracked.")
    return 0


def cmd_expiries(a):
    kite = Kite.from_env(a.folder)
    if not kite:
        print("needs a Kite token: see `options.py login`", file=sys.stderr)
        return 1
    print("\n".join(expiries(kite, a.symbol)[:12]))
    return 0


def main(argv=None):
    ap = argparse.ArgumentParser(description="NSE / BSE option chain with rule-based entry and exit signals.")
    ap.add_argument("--folder", default=".", help="the project folder that holds trader/")
    sub = ap.add_subparsers(dest="cmd")
    c = sub.add_parser("chain", help="the chain, its numbers and the signal")
    c.add_argument("symbol")
    c.add_argument("--expiry", help="YYYY-MM-DD (default: the nearest)")
    c.add_argument("--strikes", type=int, help="strikes each side of ATM to print (default 10)")
    c.add_argument("--csv", help="nseindia.com option chain CSV instead of Kite")
    c.add_argument("--lot", type=int, help="lot size (Kite knows it; a CSV does not)")
    c.add_argument("--spot", type=float, help="use this spot price")
    c.add_argument("--now", help="YYYY-MM-DD HH:MM IST, for testing")
    c.add_argument("--write", action="store_true", help="write trader/options.json for the dashboard")
    t = sub.add_parser("track", help="follow a trade for exit signals (the current BUY signal by default)")
    t.add_argument("--side", choices=["CE", "PE", "ce", "pe"])
    t.add_argument("--strike", type=float)
    t.add_argument("--premium", type=float, help="what you paid")
    t.add_argument("--lots", type=int)
    t.add_argument("--premium-stop", type=float)
    t.add_argument("--premium-target", type=float)
    t.add_argument("--under-stop", type=float)
    t.add_argument("--under-target", type=float)
    t.add_argument("--now")
    sub.add_parser("untrack", help="stop following the trade")
    e = sub.add_parser("expiries", help="the next expiries (Kite)")
    e.add_argument("symbol")
    lg = sub.add_parser("login", help="get today's Kite access token")
    lg.add_argument("--request-token")
    a = ap.parse_args(argv)
    if not a.cmd:
        ap.print_help()
        return 1
    try:
        return {"chain": cmd_chain, "track": cmd_track, "untrack": cmd_untrack, "expiries": cmd_expiries,
                "login": cmd_login}[a.cmd](a)
    except (OptionsError, alerts.AlertError) as e:
        print("options.py: %s" % e, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

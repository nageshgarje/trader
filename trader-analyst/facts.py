#!/usr/bin/env python3
"""Earnings reader for trader-analyst. Standard library only.

Pulls a company's reported numbers from SEC's XBRL "company facts" API and
lays out what changed, quarter by quarter: revenue, gross, operating and net
margins, net income, diluted EPS, and growth against the same quarter a year
earlier. It also finds the latest earnings press release (the EX-99 exhibit
of the newest 8-K with item 2.02) and the latest 10-Q or 10-K.

    python3 facts.py AAPL
    python3 facts.py AAPL KO --quarters 8
    python3 facts.py AAPL --json
    python3 facts.py AAPL AXP GOOGL KO BAC --metrics trader/metrics.json

Sources (SEC; a User-Agent with your name and email is required, from
--user-agent, SEC_USER_AGENT or sec_user_agent in trader/config.json):
  https://www.sec.gov/files/company_tickers.json            ticker -> CIK
  https://data.sec.gov/api/xbrl/companyfacts/CIK##########.json
  https://data.sec.gov/submissions/CIK##########.json        filings list
  https://www.sec.gov/Archives/edgar/data/<cik>/<accession>/index.json

How the quarters are built:
  - A quarter is a reported value covering about three months (80 to 100
    days). When a period was reported more than once (a later filing repeats
    it as a comparison), the latest filing wins, so restatements are used.
  - Companies do not file a fourth-quarter 10-Q. The fourth quarter is the
    full year (10-K) minus the three quarters inside it, and it is marked
    "derived". Derived EPS is approximate: share counts change during a year.
  - Revenue is read from the first concept a company uses of:
    RevenueFromContractWithCustomerExcludingAssessedTax, Revenues,
    RevenuesNetOfInterestExpense, SalesRevenueNet and a few more. Banks and
    insurers report revenue differently; margins for them can be blank.

--metrics FILE merges one row per ticker into a JSON file (for notes or a sheet)
(TTM revenue and EPS, margins, growth). Offline: --offline DIR reads
company_tickers.json, companyfacts-CIK##########.json,
submissions-CIK##########.json and <accession>/index.json from DIR.

Numbers only: this tool never says whether a stock is a buy.
"""

import argparse
import datetime
import datetime as _dt
import gzip
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

TICKERS_URL = "https://www.sec.gov/files/company_tickers.json"
FACTS_URL = "https://data.sec.gov/api/xbrl/companyfacts/CIK%s.json"
SUBMISSIONS_URL = "https://data.sec.gov/submissions/CIK%s.json"
ARCHIVE_URL = "https://www.sec.gov/Archives/edgar/data/%d/%s/"
PLACEHOLDER = "you@example.com"
UA_HELP = ("SEC requires a User-Agent with your name and email. Set \"sec_user_agent\": "
           "\"Your Name you@example.com\" in trader/config.json (your real name and email), "
           "or export SEC_USER_AGENT, or pass --user-agent.")

CONCEPTS = {
    "revenue": ["RevenueFromContractWithCustomerExcludingAssessedTax", "Revenues",
                "RevenuesNetOfInterestExpense", "RevenueFromContractWithCustomerIncludingAssessedTax",
                "SalesRevenueNet", "SalesRevenueGoodsNet", "SalesRevenueServicesNet"],
    "cost_of_revenue": ["CostOfGoodsAndServicesSold", "CostOfRevenue", "CostOfGoodsSold"],
    "gross_profit": ["GrossProfit"],
    "operating_income": ["OperatingIncomeLoss"],
    "net_income": ["NetIncomeLoss", "ProfitLoss", "NetIncomeLossAvailableToCommonStockholdersBasic"],
    "eps_diluted": ["EarningsPerShareDiluted", "EarningsPerShareBasicAndDiluted"],
}
FLOWS = ("revenue", "cost_of_revenue", "gross_profit", "operating_income", "net_income")


class FactsError(Exception):
    pass


def user_agent(cli=None, config="trader/config.json"):
    ua = (cli or os.environ.get("SEC_USER_AGENT") or "").strip()
    if not ua and config and os.path.isfile(config):
        try:
            with open(config, encoding="utf-8") as fh:
                ua = str(json.load(fh).get("sec_user_agent") or "").strip()
        except (OSError, ValueError):
            ua = ""
    if not ua or "@" not in ua or PLACEHOLDER in ua:
        return None
    return ua


class Fetcher(object):
    def __init__(self, ua=None, offline=None, pause=0.15):
        self.ua, self.offline, self.pause, self._last = ua, offline, pause, 0.0

    def json(self, url, name):
        if self.offline:
            path = os.path.join(self.offline, name)
            if not os.path.isfile(path):
                raise FactsError("offline: no file %s" % path)
            with open(path, encoding="utf-8") as fh:
                return json.load(fh)
        if not self.ua:
            raise FactsError(UA_HELP)
        gap = time.time() - self._last
        if gap < self.pause:
            time.sleep(self.pause - gap)
        self._last = time.time()
        req = urllib.request.Request(url, headers={"User-Agent": self.ua,
                                                   "Accept-Encoding": "gzip"})
        try:
            with urllib.request.urlopen(req, timeout=60) as resp:
                data = resp.read()
                if resp.headers.get("Content-Encoding") == "gzip":
                    data = gzip.decompress(data)
        except urllib.error.HTTPError as exc:
            if exc.code == 404:
                raise FactsError("SEC has no data at %s (no XBRL filings for this CIK?)" % url)
            raise FactsError("HTTP %s for %s" % (exc.code, url))
        except urllib.error.URLError as exc:
            raise FactsError("could not reach %s: %s" % (url, exc.reason))
        return json.loads(data.decode("utf-8"))


# ---------------------------------------------------------------- facts

def _date(s):
    return _dt.date.fromisoformat(s)


def facts_for(companyfacts, concept, unit_hint=None):
    """Raw facts for one us-gaap concept (USD, or USD/shares for EPS)."""
    node = companyfacts.get("facts", {}).get("us-gaap", {}).get(concept)
    if not node:
        return []
    units = node.get("units", {})
    unit = unit_hint if unit_hint in units else next(iter(units), None)
    return units.get(unit, []) if unit else []


def periods(facts):
    """{(start, end): fact} for duration facts from 10-Q/10-K.

    The value comes from the latest filing (restatements win). The fiscal
    label (fy, fp) comes from the first filing that reported the period: a
    later 10-Q that repeats it as a comparison carries its own fy and fp."""
    latest, first = {}, {}
    for f in facts:
        if "start" not in f or f.get("form", "").split("/")[0] not in ("10-Q", "10-K", "20-F", "40-F"):
            continue
        key = (f["start"], f["end"])
        stamp = (f.get("filed", ""), f.get("accn", ""))
        if key not in latest or stamp > (latest[key].get("filed", ""), latest[key].get("accn", "")):
            latest[key] = f
        if key not in first or stamp < (first[key].get("filed", ""), first[key].get("accn", "")):
            first[key] = f
    out = {}
    for key, f in latest.items():
        g = dict(f)
        g["fy"], g["fp"] = first[key].get("fy"), first[key].get("fp")
        g["first_filed"] = first[key].get("filed")
        out[key] = g
    return out


def quarterly(facts, derive_q4=True):
    """{end: {"value", "start", "end", "derived", "fy", "fp", "form"}} three-month values."""
    per = periods(facts)
    q = {}
    years = []
    for (start, end), f in per.items():
        days = (_date(end) - _date(start)).days
        if 80 <= days <= 100:
            q[end] = {"value": f["val"], "start": start, "end": end, "derived": False,
                      "fy": f.get("fy"), "fp": f.get("fp"), "form": f.get("form"),
                      "filed": f.get("filed")}
        elif 350 <= days <= 380:
            years.append((start, end, f))
    if derive_q4:
        for start, end, f in years:
            if end in q:
                continue
            inside = [v for v in q.values() if v["start"] >= start and v["end"] < end]
            inside.sort(key=lambda v: v["end"])
            if len(inside) != 3:
                continue
            last_q_start = _date(inside[-1]["end"]) + _dt.timedelta(days=1)
            q[end] = {"value": f["val"] - sum(v["value"] for v in inside),
                      "start": last_q_start.isoformat(), "end": end, "derived": True,
                      "fy": f.get("fy"), "fp": "Q4", "form": f.get("form"),
                      "filed": f.get("filed")}
    return q


def series(companyfacts, key, derive_q4=True):
    """Quarterly series for a metric, trying its concepts in order and filling gaps."""
    unit = "USD/shares" if key == "eps_diluted" else "USD"
    merged, used = {}, []
    for concept in CONCEPTS[key]:
        q = quarterly(facts_for(companyfacts, concept, unit), derive_q4)
        if not q:
            continue
        added = False
        for end, v in q.items():
            if end not in merged:
                merged[end] = dict(v, concept=concept)
                added = True
        if added:
            used.append(concept)
    return merged, used


def _yoy_match(series_, end, days=365, slack=10):
    target = _date(end) - _dt.timedelta(days=days)
    best = None
    for e in series_:
        gap = abs((_date(e) - target).days)
        if gap <= slack and (best is None or gap < best[0]):
            best = (gap, e)
    return series_[best[1]] if best else None


def pct(a, b):
    if a is None or b in (None, 0):
        return None
    return round(100.0 * (a - b) / abs(b), 1)


def margin(part, whole):
    if part is None or not whole:
        return None
    return round(100.0 * part / whole, 1)


def analyse(companyfacts, quarters=6, name=None):
    """Every number in the report, as a dict."""
    s = {}
    concepts = {}
    for key in CONCEPTS:
        s[key], concepts[key] = series(companyfacts, key, derive_q4=True)
    return analyse_series(s, quarters, name or companyfacts.get("entityName"),
                          str(companyfacts.get("cik", "")).zfill(10), concepts)


def analyse_series(s, quarters=6, name=None, cik=None, concepts=None):
    """The report from quarterly series {key: {end: {"value", ...}}} (SEC facts or Yahoo's figures)."""
    rev = s["revenue"]
    if not rev and not s["net_income"]:
        raise FactsError("no quarterly revenue or net income in the XBRL facts")
    ends = sorted(set(rev) | set(s["net_income"]), reverse=True)[:quarters]
    rows = []
    for end in ends:
        r = rev.get(end, {}).get("value")
        gp = s["gross_profit"].get(end, {}).get("value")
        if gp is None and r is not None and end in s["cost_of_revenue"]:
            gp = r - s["cost_of_revenue"][end]["value"]
        oi = s["operating_income"].get(end, {}).get("value")
        ni = s["net_income"].get(end, {}).get("value")
        eps = s["eps_diluted"].get(end, {}).get("value")
        prior = {k: _yoy_match(s[k], end) for k in ("revenue", "net_income", "eps_diluted")}
        base = rev.get(end) or s["net_income"].get(end) or {}
        rows.append({
            "end": end, "start": base.get("start"),
            "fiscal": "%s %s" % (base.get("fp") or "", base.get("fy") or ""),
            "derived": any(s[k].get(end, {}).get("derived") for k in ("revenue", "net_income")),
            "revenue": r, "gross_profit": gp, "operating_income": oi, "net_income": ni,
            "eps_diluted": round(eps, 2) if eps is not None else None,
            "gross_margin": margin(gp, r), "operating_margin": margin(oi, r),
            "net_margin": margin(ni, r),
            "revenue_yoy": pct(r, (prior["revenue"] or {}).get("value")),
            "net_income_yoy": pct(ni, (prior["net_income"] or {}).get("value")),
            "eps_yoy": pct(eps, (prior["eps_diluted"] or {}).get("value")),
            "filed": base.get("filed"), "form": base.get("form"),
        })
    ttm = {}
    for key in ("revenue", "net_income", "eps_diluted"):
        last4 = sorted(s[key])[-4:]
        if len(last4) == 4 and (_date(last4[-1]) - _date(last4[0])).days < 300:
            ttm[key] = sum(s[key][e]["value"] for e in last4)
    if "eps_diluted" in ttm:
        ttm["eps_diluted"] = round(ttm["eps_diluted"], 2)
    return {"entity": name, "cik": cik, "rows": rows, "ttm": ttm, "concepts": concepts or {}}


def stale_note(result, filings):
    """A note when SEC's facts lag a report that is already filed."""
    rep = (filings or {}).get("report")
    if not rep or not result["rows"] or not rep.get("period"):
        return None
    latest = result["rows"][0]["end"]
    if rep["period"] > latest and (_date(rep["period"]) - _date(latest)).days > 20:
        return ("SEC's XBRL facts stop at %s, but a %s for the period ending %s was filed %s. "
                "Read it directly: %s" % (latest, rep["form"], rep["period"], rep["filed"],
                                         rep["url"]))
    return None


def changes(result):
    """Plain sentences: what changed in the latest quarter."""
    rows = result["rows"]
    if not rows:
        return []
    cur = rows[0]
    prev = rows[1] if len(rows) > 1 else None
    out = []

    def money(v):
        if v is None:
            return "n/a"
        if is_nse():                      # Indian convention: crore (10 million)
            return "$%s crore" % "{:,.0f}".format(v / 1e7)
        return "$%.2fB" % (v / 1e9) if abs(v) >= 1e9 else "$%.1fM" % (v / 1e6)

    label = "Quarter ending %s%s" % (cur["end"], " (derived from the 10-K)" if cur["derived"] else "")
    line = "%s: revenue %s" % (label, money(cur["revenue"]))
    if cur["revenue_yoy"] is not None:
        line += " (%+.1f%% vs a year earlier)" % cur["revenue_yoy"]
    line += ", net income %s" % money(cur["net_income"])
    if cur["net_income_yoy"] is not None:
        line += " (%+.1f%%)" % cur["net_income_yoy"]
    if cur["eps_diluted"] is not None:
        line += ", diluted EPS $%.2f" % cur["eps_diluted"]
        if cur["eps_yoy"] is not None:
            line += " (%+.1f%%)" % cur["eps_yoy"]
    out.append(line + ".")
    for key, name in (("gross_margin", "Gross margin"), ("operating_margin", "Operating margin"),
                      ("net_margin", "Net margin")):
        if cur[key] is None:
            continue
        s = "%s %.1f%%" % (name, cur[key])
        if prev and prev[key] is not None:
            s += " (%+.1f points vs the quarter before)" % (cur[key] - prev[key])
        out.append(s + ".")
    if cur["net_income"] is not None and cur["operating_income"] is not None \
            and cur["net_income"] > cur["operating_income"]:
        out.append("Net income is above operating income: it includes gains outside the "
                   "business (investments, tax items). Check the %s before reading it as growth."
                   % ("company's results filing on NSE" if is_nse() else "10-Q"))
    if prev and cur["revenue"] is not None and prev["revenue"]:
        out.append("Revenue %+.1f%% vs the quarter before (many businesses are seasonal: "
                   "compare with a year earlier first)." % pct(cur["revenue"], prev["revenue"]))
    return out


# ---------------------------------------------------------------- filings

EXHIBIT_RANK = [r"ex(?:hibit)?[-_]?99[-_.]?0?1(?!\d)", r"ex(?:hibit)?[-_]?99",
                r"earnings|release|press", r"ex(?:hibit)?[-_]?\d"]


def exhibit_name(names, primary=None):
    """The press release among a filing's files (EX-99.1 by its usual names)."""
    docs = [n for n in names if n.lower().endswith((".htm", ".html", ".txt", ".pdf"))
            and n != primary and not re.match(r"^(R\d+\.htm|\d{10}-\d{2}-\d{6})", n)]
    for rx in EXHIBIT_RANK:
        hits = sorted(n for n in docs if re.search(rx, n, re.I))
        if hits:
            return hits[0]
    return None


def latest_filings(submissions, cik, fetcher=None):
    """Newest 10-Q/10-K and newest earnings 8-K (item 2.02) with its EX-99 exhibit."""
    rec = submissions.get("filings", {}).get("recent", {})
    out = {"report": None, "earnings_8k": None}
    base_cik = int(cik)
    for i, form in enumerate(rec.get("form", [])):
        acc = rec["accessionNumber"][i]
        folder = ARCHIVE_URL % (base_cik, acc.replace("-", ""))
        if out["report"] is None and form in ("10-Q", "10-K", "20-F", "40-F"):
            out["report"] = {"form": form, "filed": rec["filingDate"][i],
                             "period": rec["reportDate"][i], "accession": acc,
                             "url": folder + rec["primaryDocument"][i]}
        if out["earnings_8k"] is None and form == "8-K" and "2.02" in (rec.get("items", [""] * (i + 1))[i] or ""):
            e = {"form": form, "filed": rec["filingDate"][i], "accession": acc,
                 "url": folder + rec["primaryDocument"][i], "exhibit": None,
                 "index": folder + "%s-index.htm" % acc}
            if fetcher is not None:
                try:
                    idx = fetcher.json(folder + "index.json", os.path.join(acc.replace("-", ""), "index.json"))
                    names = [it["name"] for it in idx.get("directory", {}).get("item", [])]
                    pick = exhibit_name(names, rec["primaryDocument"][i])
                    if pick:
                        e["exhibit"] = folder + pick
                except FactsError:
                    pass
            out["earnings_8k"] = e
        if out["report"] and out["earnings_8k"]:
            break
    return out


# ---------------------------------------------------------------- output

def _m(v):
    if v is None:
        return "-"
    return "{:,.0f}".format(v / (1e7 if is_nse() else 1e6))


def _p(v):
    return "-" if v is None else "%+.1f%%" % v if v < 0 or v > 0 else "0.0%"


def _pm(v):
    return "-" if v is None else "%.1f%%" % v


def render(ticker, result, filings):
    if is_nse():
        out = ["%s (%s). Rs crore except EPS. Source: Yahoo Finance quarterly figures (unofficial; "
               "check the company's results on nseindia.com)." % (result["entity"] or ticker, ticker), ""]
    else:
        out = ["%s (%s), CIK %s. US$ millions except EPS. Source: SEC XBRL company facts." % (
            result["entity"], ticker, result["cik"]), ""]
    out.append("%-11s %-9s %10s %8s %7s %7s %7s %10s %8s %7s" % (
        "quarter end", "fiscal", "revenue", "rev yoy", "gross", "oper", "net",
        "net inc", "EPS dil", "EPS yoy"))
    for r in result["rows"]:
        out.append("%-11s %-9s %10s %8s %7s %7s %7s %10s %8s %7s%s" % (
            r["end"], r["fiscal"].strip()[:9], _m(r["revenue"]), _p(r["revenue_yoy"]),
            _pm(r["gross_margin"]), _pm(r["operating_margin"]), _pm(r["net_margin"]),
            _m(r["net_income"]), "-" if r["eps_diluted"] is None else "%.2f" % r["eps_diluted"],
            _p(r["eps_yoy"]), "  derived" if r["derived"] else ""))
    t = result["ttm"]
    if t:
        out.append("")
        unit = " crore" if is_nse() else "M"
        out.append("Last 4 quarters: revenue %s, net income %s, diluted EPS %s." % (
            "$" + _m(t.get("revenue")) + unit if t.get("revenue") is not None else "-",
            "$" + _m(t.get("net_income")) + unit if t.get("net_income") is not None else "-",
            "$%.2f" % t["eps_diluted"] if t.get("eps_diluted") is not None else "-"))
    out.append("")
    out.append("What changed:")
    for line in changes(result):
        out.append("  - " + line)
    if filings:
        out.append("")
        rep, e8 = filings.get("report"), filings.get("earnings_8k")
        if rep:
            out.append("Latest %s: period %s, filed %s  %s" % (rep["form"], rep["period"],
                                                              rep["filed"], rep["url"]))
        if e8:
            out.append("Latest earnings 8-K (item 2.02): filed %s  %s" % (
                e8["filed"], e8["exhibit"] or e8["index"]))
    note = stale_note(result, filings)
    if note:
        out.append("")
        out.append("NOTE: " + note)
    if any(r["derived"] for r in result["rows"]):
        out.append("")
        out.append("Derived quarters are the 10-K year minus the three 10-Qs; derived EPS is "
                   "approximate.")
    return "\n".join(out) + "\n"


def metrics_row(ticker, result):
    rows = result["rows"]
    cur = rows[0] if rows else {}
    t = result["ttm"]
    return {"ticker": ticker, "name": result["entity"], "cik": result["cik"],
            "period_end": cur.get("end"), "revenue_ttm": t.get("revenue"),
            "net_income_ttm": t.get("net_income"), "eps_ttm": t.get("eps_diluted"),
            "revenue_growth_yoy": cur.get("revenue_yoy"), "eps_growth_yoy": cur.get("eps_yoy"),
            "net_income_growth_yoy": cur.get("net_income_yoy"),
            "gross_margin": cur.get("gross_margin"), "operating_margin": cur.get("operating_margin"),
            "net_margin": cur.get("net_margin"),
            "source": "Yahoo Finance (unofficial)" if is_nse() else "SEC XBRL company facts",
            "currency": "INR" if is_nse() else "USD"}


# ---------------------------------------------------------------- NSE: Yahoo's quarterly figures

YAHOO_FUND = ("https://query2.finance.yahoo.com/ws/fundamentals-timeseries/v1/finance/timeseries/%s"
              "?type=%s&period1=1483228800&period2=%d")
YAHOO_TYPES = {"revenue": "quarterlyTotalRevenue", "gross_profit": "quarterlyGrossProfit",
               "cost_of_revenue": "quarterlyCostOfRevenue", "operating_income": "quarterlyOperatingIncome",
               "net_income": "quarterlyNetIncome", "eps_diluted": "quarterlyDilutedEPS"}
YAHOO_UA = "Mozilla/5.0 (compatible; day-trader-skill/2.0; +https://github.com/Jakeschincariol/day-trader-skill)"


def yahoo_series(doc):
    """Yahoo's fundamentals-timeseries JSON -> {key: {end: {"value", "end", "fp", "fy"}}}. Yahoo keeps about
    five quarters and can skip one, so a quarter has a year-earlier one to compare with only sometimes."""
    try:
        results = doc["timeseries"]["result"]
    except (KeyError, TypeError):
        raise FactsError("Yahoo's figures are not the shape this tool knows")
    by_type = {}
    for r in results or []:
        t = ((r.get("meta") or {}).get("type") or [None])[0]
        if t:
            by_type[t] = [x for x in (r.get(t) or []) if x and x.get("reportedValue")]
    s = {}
    for key, t in YAHOO_TYPES.items():
        s[key] = {}
        for x in by_type.get(t, []):
            end = x.get("asOfDate")
            v = (x.get("reportedValue") or {}).get("raw")
            if end and isinstance(v, (int, float)):
                s[key][end] = {"value": float(v), "end": end, "fp": "", "fy": end[:4], "form": "Yahoo"}
    if not s["revenue"] and not s["net_income"]:
        raise FactsError("Yahoo has no quarterly revenue or net income for this ticker")
    return s


def yahoo_name(sym):
    """The company name from trader-scanner's NSE universe, if it is there."""
    try:
        with open("trader/universe.nse.txt", encoding="utf-8") as fh:
            for ln in fh:
                if ln.upper().startswith(sym + "|"):
                    return ln.split("|", 1)[1].strip()
    except OSError:
        pass
    return None


def fetch_yahoo_figures(sym, offline=None):
    name = "yahoo-fund-%s.json" % sym
    if offline:
        path = os.path.join(offline, name)
        if not os.path.exists(path):
            raise FactsError("offline: no file %s" % path)
        with open(path, encoding="utf-8") as fh:
            return json.load(fh)
    url = YAHOO_FUND % (urllib.parse.quote(sym), ",".join(YAHOO_TYPES.values()), int(time.time()))
    req = urllib.request.Request(url, headers={"User-Agent": YAHOO_UA, "Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        raise FactsError("Yahoo answered HTTP %d for %s" % (exc.code, sym))
    except (urllib.error.URLError, OSError, ValueError) as exc:
        raise FactsError("could not read Yahoo's figures for %s: %s" % (sym, getattr(exc, "reason", exc)))


def main_nse(args):
    """trader-analyst for NSE companies: Yahoo's quarterly figures, the same report."""
    results, rows, failed = [], [], 0
    for t in args.tickers:
        sym = ysym(t)
        if is_index(sym):
            print("facts: %s is an index, not a company: it has no earnings" % sym, file=sys.stderr)
            failed += 1
            continue
        try:
            result = analyse_series(yahoo_series(fetch_yahoo_figures(sym, args.offline)), args.quarters,
                                    yahoo_name(sym) or sym)
        except FactsError as exc:
            print("facts: %s: %s" % (sym, exc), file=sys.stderr)
            failed += 1
            continue
        results.append({"ticker": sym, "result": result, "filings": None, "changes": changes(result),
                        "note": None, "source": "yahoo"})
        rows.append(metrics_row(sym, result))
        if not args.json:
            sys.stdout.write(render(sym, result, None))
            sys.stdout.write("Yahoo keeps about five quarters and can skip one, so year-on-year growth shows "
                             "only where the quarter a year earlier is there.\n\n")
    if args.json:
        print(json.dumps(results, indent=2))
    if args.metrics and rows:
        write_metrics(args.metrics, rows, args.json)
    return 0 if results and not failed else (1 if results else 2)


def write_metrics(path, rows, quiet=False):
    existing = {}
    if os.path.isfile(path):
        with open(path, encoding="utf-8") as fh:
            existing = {r["ticker"]: r for r in json.load(fh).get("companies", [])}
    for r in rows:
        existing[r["ticker"]] = r
    folder = os.path.dirname(path)
    if folder:
        os.makedirs(folder, exist_ok=True)
    with open(path, "w", encoding="utf-8") as fh:
        json.dump({"as_of": _dt.date.today().isoformat(),
                   "companies": sorted(existing.values(), key=lambda r: r["ticker"])}, fh, indent=2)
        fh.write("\n")
    if not quiet:
        print("wrote %s (%d companies)" % (path, len(existing)))


def clean_title(title):
    return re.sub(r"\s*/[A-Z]{2,5}/?\s*$", "", title or "").strip() or None


def ticker_ciks(fetcher):
    """ticker -> (10-digit CIK, company name) from company_tickers.json."""
    data = fetcher.json(TICKERS_URL, "company_tickers.json")
    out = {}
    for v in data.values():
        out.setdefault(v["ticker"].upper().replace("-", "."),
                       (str(v["cik_str"]).zfill(10), clean_title(v.get("title"))))
    return out


# ---------------------------------------------------------------- markets: US (NYSE) or India (NSE)
# The same block lives in every tool that knows a market; tests/test_nse.py keeps the copies in step.

NSE_HOLIDAYS = {  # NSE circular NSE/CMTR/71775 (2025-12-12): weekday trading holidays for 2026
    "2026-01-26": "Republic Day",
    "2026-03-03": "Holi",
    "2026-03-26": "Shri Ram Navami",
    "2026-03-31": "Shri Mahavir Jayanti",
    "2026-04-03": "Good Friday",
    "2026-04-14": "Dr. Baba Saheb Ambedkar Jayanti",
    "2026-05-01": "Maharashtra Day",
    "2026-05-28": "Bakri Id",
    "2026-06-26": "Muharram",
    "2026-09-14": "Ganesh Chaturthi",
    "2026-10-02": "Mahatma Gandhi Jayanti",
    "2026-10-20": "Dussehra",
    "2026-11-10": "Diwali-Balipratipada",
    "2026-11-24": "Prakash Gurpurb Sri Guru Nanak Dev",
    "2026-12-25": "Christmas",
}
NSE_CALENDAR_YEARS = (2026,)   # Indian holidays follow the lunar calendar: no rule can fill other years
NSE_INDEXES = {"NIFTY": "^NSEI", "NIFTY50": "^NSEI", "NIFTY_50": "^NSEI", "BANKNIFTY": "^NSEBANK",
               "NIFTYBANK": "^NSEBANK", "FINNIFTY": "NIFTY_FIN_SERVICE.NS", "NIFTYIT": "^CNXIT"}
MARKETS = {
    "us": {"code": "us", "exchange": "NYSE", "place": "New York", "tz": "ET", "cur": "$",
           "open": datetime.time(9, 30), "close": datetime.time(16, 0),
           "no_entries_before": "09:45", "no_entries_after": "15:30", "flat_by": "15:55"},
    # NSE: 9:15 to 15:30 IST, no daylight saving. Brokers square off intraday positions from about
    # 15:15 (Zerodha MIS 15:25), so the defaults stop new entries at 15:00 and get flat by 15:15.
    "nse": {"code": "nse", "exchange": "NSE", "place": "India", "tz": "IST", "cur": "₹",
            "open": datetime.time(9, 15), "close": datetime.time(15, 30),
            "no_entries_before": "09:30", "no_entries_after": "15:00", "flat_by": "15:15"},
}
TIME_KEYS = ("no_entries_before", "no_entries_after", "flat_by")
# Money limits are in the market's own currency. The "_usd" names are kept so one config serves every
# tool, but on NSE they are rupees and come only from the "nse" block: $5,000 is never read as Rs 5,000.
MONEY_KEYS = ("budget", "max_loss_per_trade_usd", "max_daily_loss_usd", "max_order_usd", "min_order_usd")
MARKET = MARKETS["us"]


def market_for(sym, override=None):
    """"nse" for an NSE ticker (RELIANCE.NS, ^NSEI, NIFTY, BANKNIFTY) or when asked, else "us"."""
    if override:
        code = str(override).strip().lower()
        code = {"india": "nse", "in": "nse", "nyse": "us", "usa": "us"}.get(code, code)
        if code not in MARKETS:
            raise ValueError("market is us or nse, not %r" % override)
        return code
    s = str(sym or "").upper().strip()
    if s.endswith(".NS") or s.startswith("^NSE") or s.startswith("^CNX") or s in NSE_INDEXES:
        return "nse"
    return "us"


def use_market(code, points=False):
    """Switch the clock, the session, the calendar and the defaults to one market. points=True for
    an index (NIFTY is quoted in points, not rupees): prices print with no currency sign."""
    global MARKET, OPEN, CLOSE
    MARKET = dict(MARKETS[code], cur="") if points else MARKETS[code]
    OPEN, CLOSE = MARKET["open"], MARKET["close"]
    defaults = globals().get("DEFAULTS")
    if isinstance(defaults, dict):
        for k in TIME_KEYS:
            if k in defaults:
                defaults[k] = MARKET[k]
    hook = globals().get("on_market")    # a tool with its own session constants rebuilds them here
    if hook:
        hook()
    return MARKET


def is_nse():
    return MARKET["code"] == "nse"


def ysym(sym):
    """The Yahoo ticker: BRK.B -> BRK-B in the US; RELIANCE -> RELIANCE.NS and NIFTY -> ^NSEI for NSE."""
    s = str(sym).upper().strip()
    if is_nse():
        return NSE_INDEXES.get(s) or (s if s.startswith("^") or "." in s else s + ".NS")
    return s.replace(".", "-")


NSE_TICKER = re.compile(r"^\^?[A-Z0-9][A-Z0-9&_\-]{0,24}(\.NS)?$")   # M&M, BAJAJ-AUTO, 360ONE.NS, ^NSEI


def nse_ticker_ok(sym):
    return bool(NSE_TICKER.match(str(sym or "").strip().upper()))


def is_index(sym):
    return ysym(sym).startswith("^")


def nse_closed(d):
    """None when NSE trades on day d, else the reason it is closed."""
    if d.weekday() >= 5:
        return "weekend"
    name = NSE_HOLIDAYS.get(d.isoformat())
    return ("NSE holiday: " + name) if name else None


def calendar_known(d):
    return d.year in (NSE_CALENDAR_YEARS if is_nse() else globals().get("CALENDAR_YEARS", ()))


def calendar_note():
    if is_nse():
        return ("NSE has published its holiday list for %s only, and Indian holidays follow the lunar "
                "calendar: check nseindia.com for this date." % " and ".join(map(str, NSE_CALENDAR_YEARS)))
    return ("NYSE published its holiday list for %s; this date uses its standing rules, which cannot "
            "know a special closure: check nyse.com." % " and ".join(map(str, globals().get("CALENDAR_YEARS", ()))))


def market_config(doc):
    """The config for this market. The top-level clock times and money limits are the US ones. NSE takes
    its own from an "nse" block ({"nse": {"budget": 100000, "max_loss_per_trade_usd": 500,
    "no_entries_after": "14:30"}}, rupees), its clock defaults otherwise, a daily loss of three times the
    max loss per trade, and no max or min order unless the block sets one."""
    doc = dict(doc or {})
    if is_nse():
        for k in TIME_KEYS + MONEY_KEYS:
            doc.pop(k, None)
        doc.update(doc.get("nse") or {})
        doc.setdefault("budget", None)
        doc.setdefault("max_loss_per_trade_usd", None)
        ml = doc.get("max_loss_per_trade_usd")
        doc.setdefault("max_daily_loss_usd", 3 * float(ml) if isinstance(ml, (int, float)) else None)
        doc.setdefault("max_order_usd", None)
        doc.setdefault("min_order_usd", 0)
    return doc


SPLIT_FILES = ("trader/portfolio.json", "trader/state/broker.json")


def market_path(path):
    """NSE keeps its own account and decision log (rupees, its own daily limits): the default
    trader/portfolio.json becomes trader/portfolio.nse.json, and the same for trader/state/broker.json."""
    if is_nse() and path in SPLIT_FILES:
        return path[:-len(".json")] + ".nse.json"
    return path


def localize(text):
    """US words to this market's: $ -> the rupee sign, ET -> IST, NYSE -> NSE, New York -> India."""
    if not is_nse() and MARKET["cur"] == "$":
        return text
    cur = MARKET["cur"]
    if not is_nse():
        return text.replace("$", cur)
    try:
        cur.encode(getattr(sys.stdout, "encoding", None) or "utf-8")
    except (UnicodeEncodeError, LookupError):
        cur = "Rs "
    text = text.replace("nyse.com", "nseindia.com").replace("NYSE", "NSE").replace("New York", "India")
    return re.sub(r"\bET\b", "IST", text.replace("$", cur))


class Localized(object):
    """A stream that localizes what is written to it (stdout and stderr while an NSE run prints)."""

    def __init__(self, stream):
        self.stream = stream

    def write(self, s):
        return self.stream.write(localize(s))

    def __getattr__(self, name):
        return getattr(self.stream, name)


def run_localized(fn, *args):
    """Run fn with stdout and stderr localized for this market, and restore them after."""
    if not is_nse() and MARKET["cur"] == "$":
        return fn(*args)
    out, err = sys.stdout, sys.stderr
    sys.stdout, sys.stderr = Localized(out), Localized(err)
    try:
        return fn(*args)
    finally:
        sys.stdout, sys.stderr = out, err


def peek_json(path):
    """A JSON object from path, or {} when it is missing or not JSON (for picking the market early)."""
    try:
        with open(path, encoding="utf-8") as fh:
            doc = json.load(fh)
        return doc if isinstance(doc, dict) else {}
    except (OSError, ValueError, TypeError):
        return {}


def run_in_market(code, sym, fn, *args):
    """Run fn in one market (sym is the ticker, so an index prints in points), then go back to the US."""
    use_market(code)                     # is_index needs the market: NIFTY is ^NSEI only on NSE
    use_market(code, points=bool(sym) and is_index(sym))
    try:
        return run_localized(fn, *args)
    finally:
        use_market("us")

# ---------------------------------------------------------------- end of markets


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("tickers", nargs="*", help="tickers, e.g. AAPL KO")
    ap.add_argument("--cik", help="use this CIK instead of a ticker lookup (one company)")
    ap.add_argument("--quarters", type=int, default=6)
    ap.add_argument("--no-filings", action="store_true", help="skip the 8-K and 10-Q links")
    ap.add_argument("--metrics", metavar="FILE", help="merge a row per ticker into this JSON")
    ap.add_argument("--user-agent")
    ap.add_argument("--config", default="trader/config.json")
    ap.add_argument("--offline", metavar="DIR")
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--market", choices=["us", "nse"],
                    help="nse: Indian companies from Yahoo's quarterly figures; .NS tickers pick it")
    args = ap.parse_args(argv)
    if not args.tickers and not args.cik:
        ap.print_help()
        return 2
    try:
        nse = [market_for(t, args.market) == "nse" for t in args.tickers]
    except ValueError as e:
        print("facts: %s" % e, file=sys.stderr)
        return 2
    if nse and all(nse):
        return run_in_market("nse", None, main_nse, args)
    if any(nse):
        print("facts: run NSE (.NS) and US tickers separately: they come from different sources",
              file=sys.stderr)
        return 2
    ua = user_agent(args.user_agent, args.config)
    if not ua and not args.offline:
        print("facts: " + UA_HELP, file=sys.stderr)
        return 2
    fetcher = Fetcher(ua, offline=args.offline)
    results, rows, failed = [], [], 0
    try:
        ciks = {} if args.cik else ticker_ciks(fetcher)
    except FactsError as exc:
        print("facts: %s" % exc, file=sys.stderr)
        return 2
    wanted = args.tickers or [args.cik]
    for t in wanted:
        tick = t.upper()
        if args.cik:
            cik, name = str(args.cik).zfill(10), None
        else:
            cik, name = ciks.get(tick.replace("-", "."), (None, None))
        if not cik:
            print("facts: no SEC CIK for %s (is it a US filer? ETFs and foreign filers "
                  "may not have XBRL facts)" % tick, file=sys.stderr)
            failed += 1
            continue
        try:
            cf = fetcher.json(FACTS_URL % cik, "companyfacts-CIK%s.json" % cik)
            result = analyse(cf, quarters=args.quarters, name=name)
            filings = None
            if not args.no_filings:
                sub = fetcher.json(SUBMISSIONS_URL % cik, "submissions-CIK%s.json" % cik)
                filings = latest_filings(sub, cik, fetcher)
        except FactsError as exc:
            print("facts: %s: %s" % (tick, exc), file=sys.stderr)
            failed += 1
            continue
        results.append({"ticker": tick, "result": result, "filings": filings,
                        "changes": changes(result), "note": stale_note(result, filings)})
        rows.append(metrics_row(tick, result))
        if not args.json:
            sys.stdout.write(render(tick, result, filings))
            sys.stdout.write("\n")
    if args.json:
        print(json.dumps(results, indent=2))
    if args.metrics and rows:
        existing = {}
        if os.path.isfile(args.metrics):
            with open(args.metrics, encoding="utf-8") as fh:
                existing = {r["ticker"]: r for r in json.load(fh).get("companies", [])}
        for r in rows:
            existing[r["ticker"]] = r
        folder = os.path.dirname(args.metrics)
        if folder:
            os.makedirs(folder, exist_ok=True)
        with open(args.metrics, "w", encoding="utf-8") as fh:
            json.dump({"as_of": _dt.date.today().isoformat(),
                       "companies": sorted(existing.values(), key=lambda r: r["ticker"])},
                      fh, indent=2)
            fh.write("\n")
        if not args.json:
            print("wrote %s (%d companies)" % (args.metrics, len(existing)))
    return 0 if results and not failed else (1 if results else 2)


if __name__ == "__main__":
    sys.exit(main())

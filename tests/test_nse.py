"""The Indian market (NSE) in trader-entry and trader-exit, on real Yahoo bars saved on 2026-11-20:
RELIANCE.NS and NIFTY 50 (^NSEI), five sessions of 5-minute bars, 2026-11-16 to 2026-11-20."""

import contextlib
import io
import json
import os
import shutil
import tempfile
import unittest
from decimal import Decimal

from _load import load, FIX

entry = load("trader-entry", "entry")
exitp = load("trader-exit", "exit")
NSE = os.path.join(FIX, "nse")
REL = os.path.join(NSE, "RELIANCE.NS-5d5m-2026-11-20.json.gz")
NIFTY = os.path.join(NSE, "NSEI-5d5m-2026-11-20.json.gz")


def run(mod, argv):
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        code = mod.main(argv)
    return code, out.getvalue(), err.getvalue()


class Market(unittest.TestCase):
    def tearDown(self):
        entry.use_market("us")
        exitp.use_market("us")

    def test_the_ticker_picks_the_market(self):
        for s in ("RELIANCE.NS", "reliance.ns", "^NSEI", "NIFTY", "BANKNIFTY", "^NSEBANK", "^CNXIT"):
            self.assertEqual(entry.market_for(s), "nse", s)
        for s in ("TSLA", "BRK.B", "^GSPC", ""):
            self.assertEqual(entry.market_for(s), "us", s)
        self.assertEqual(entry.market_for("TSLA", "india"), "nse")
        self.assertRaises(ValueError, entry.market_for, "TSLA", "lse")

    def test_yahoo_tickers(self):
        entry.use_market("nse")
        self.assertEqual([entry.ysym(s) for s in ("reliance", "RELIANCE.NS", "NIFTY", "BANKNIFTY", "^NSEI")],
                         ["RELIANCE.NS", "RELIANCE.NS", "^NSEI", "^NSEBANK", "^NSEI"])
        entry.use_market("us")
        self.assertEqual(entry.ysym("brk.b"), "BRK-B")

    def test_ist_is_utc_plus_five_thirty_all_year(self):
        entry.use_market("nse")
        for day in ("2026-01-15", "2026-07-15"):
            self.assertEqual(entry.parse_now(day + " 09:15").strftime("%H:%M"), "03:45")

    def test_nse_calendar_from_the_2026_circular(self):
        entry.use_market("nse")
        self.assertEqual(entry.trading_day(entry.datetime.date(2026, 11, 10)),
                         (False, "NSE holiday: Diwali-Balipratipada"))
        self.assertEqual(entry.trading_day(entry.datetime.date(2026, 11, 26)), (True, ""))   # US Thanksgiving
        self.assertEqual(len(entry.NSE_HOLIDAYS), 15)
        self.assertTrue(all(entry.datetime.date.fromisoformat(d).weekday() < 5 for d in entry.NSE_HOLIDAYS))
        self.assertEqual(entry.session(entry.datetime.date(2026, 11, 27)), (entry.OPEN, entry.CLOSE))  # no early close
        self.assertFalse(entry.calendar_known(entry.datetime.date(2027, 3, 1)))

    def test_us_clock_times_in_the_config_do_not_leak_into_nse(self):
        doc = {"no_entries_after": "15:30", "flat_by": "15:55", "nse": {"no_entries_after": "14:30"}}
        entry.use_market("nse")
        cfg = entry.market_config(doc)
        self.assertEqual((cfg["no_entries_after"], cfg["budget"], cfg["max_loss_per_trade_usd"]), ("14:30", None, None))
        self.assertNotIn("flat_by", cfg)
        money = entry.market_config({"budget": 5000, "max_loss_per_trade_usd": 50,
                                     "nse": {"budget": 100000, "max_loss_per_trade_usd": 500}})
        self.assertEqual((money["budget"], money["max_loss_per_trade_usd"], money["max_daily_loss_usd"],
                          money["min_order_usd"]), (100000, 500, 1500.0, 0))
        self.assertEqual(entry.DEFAULTS["no_entries_after"], "15:00")
        entry.use_market("us")
        self.assertEqual(entry.DEFAULTS["no_entries_after"], "15:30")

    def test_the_block_is_the_same_in_every_tool(self):
        def block(path):
            with open(path, encoding="utf-8") as fh:
                text = fh.read()
            start = text.index("# ---------------------------------------------------------------- markets")
            return text[start:text.index("# ---------------------------------------------------------------- end of markets")]
        tools = ["trader-entry/entry.py", "trader-exit/exit.py", "trader-scanner/scan.py",
                 "trader-timemachine/timemachine.py", "trader-budget/size.py", "trader-risk/check.py",
                 "trader-alerts/alerts.py", "trader-broker/ticket.py", "trader-news/news.py",
                 "trader-analyst/facts.py"]
        blocks = {t: block(os.path.join(os.path.dirname(os.path.dirname(entry.__file__)), t)) for t in tools}
        self.assertEqual(len(set(blocks.values())), 1, [t for t in tools if blocks[t] != blocks[tools[0]]])


class EntryOnNse(unittest.TestCase):
    def test_reliance_opening_range_breakout(self):
        code, out, _ = run(entry, ["check", "RELIANCE.NS", "--offline", REL, "--now", "2026-11-19 15:10"])
        self.assertEqual(code, 0)
        self.assertIn("2026-11-19 15:10 IST", out)
        self.assertIn("Opening range 9:15-9:30: high ₹1366.70, low ₹1355.20 (3 of 3 bars).", out)
        self.assertIn("TRIGGERED 1:30 PM at ₹1367.10: the 13:25 bar (bar 50)", out)
        self.assertNotIn("$", out)

    def test_bare_symbol_with_market_flag_and_waiting(self):
        code, out, _ = run(entry, ["check", "RELIANCE", "--market", "nse", "--offline", REL,
                                   "--now", "2026-11-19 10:30"])
        self.assertEqual(code, 1)
        self.assertIn("WAITING  last close ₹1359.30 (10:25 bar), ₹7.40 under the range high", out)

    def test_cutoff_is_15_00_and_the_range_forms_until_9_30(self):
        _, out, _ = run(entry, ["check", "RELIANCE.NS", "--offline", REL, "--now", "2026-11-19 09:25"])
        self.assertIn("RANGE FORMING", out)
        entry.use_market("nse")
        try:
            chart = entry.get_chart("RELIANCE.NS", "5m", offline=REL)
            res = entry.evaluate(chart, entry.parse_now("2026-11-19 15:20"), level=1400.0, setup="level")
        finally:
            entry.use_market("us")
        self.assertEqual((res["status"], res["no_entries_after"]), ("expired", "15:00"))

    def test_nifty_is_in_points_and_has_no_volume(self):
        d = tempfile.mkdtemp()
        try:
            path = os.path.join(d, "entry.json")
            code, out, _ = run(entry, ["check", "NIFTY", "--offline", NIFTY, "--now", "2026-11-19 09:55",
                                       "--write", path])
            self.assertEqual(code, 0)
            self.assertIn("TRIGGERED 9:50 AM at 25257.10: the 9:45 bar (bar 6)", out)
            self.assertNotIn("₹", out)
            with open(path) as fh:
                doc = json.load(fh)
            self.assertEqual((doc["symbol"], doc["market"], doc["price"]), ("^NSEI", "nse", 25257.1))
        finally:
            shutil.rmtree(d)
        code, _, err = run(entry, ["check", "NIFTY", "--offline", NIFTY, "--now", "2026-11-19 11:00",
                                   "--min-volume-x", "1"])
        self.assertEqual(code, 1)
        self.assertIn("no volume", err)

    def test_holiday(self):
        _, out, _ = run(entry, ["check", "RELIANCE.NS", "--offline", REL, "--now", "2026-11-10 10:00"])
        self.assertIn("the NSE is closed today (NSE holiday: Diwali-Balipratipada)", out)
        self.assertIn("Next open: 2026-11-11 09:15", out)

    def test_us_runs_are_unchanged_after_an_nse_run(self):
        run(entry, ["check", "RELIANCE.NS", "--offline", REL, "--now", "2026-11-19 15:10"])
        self.assertEqual((entry.MARKET["code"], entry.OPEN, entry.DEFAULTS["no_entries_after"]),
                         ("us", entry.datetime.time(9, 30), "15:30"))


class ExitOnNse(unittest.TestCase):
    def tearDown(self):
        exitp.use_market("us")

    def test_tick_bands(self):
        exitp.use_market("nse")
        self.assertEqual([str(exitp.tick_for(p)) for p in (99, 250, 999.95, 1368.4, 5000, 12000, 25000)],
                         ["0.01", "0.05", "0.05", "0.10", "0.50", "1.00", "5.00"])
        exitp.use_market("nse", points=True)
        self.assertEqual(exitp.tick_for(25257.1), Decimal("0.01"))
        exitp.use_market("us")
        self.assertEqual(exitp.tick_for(1368.4), Decimal("0.01"))

    def test_reliance_plan_rounds_to_the_tick_and_is_flat_by_15_15(self):
        d = tempfile.mkdtemp()
        try:
            path = os.path.join(d, "plan.json")
            code, out, _ = run(exitp, ["plan", "--symbol", "RELIANCE.NS", "--entry", "1368.40", "--or-high", "1366.70",
                                       "--or-low", "1355.20", "--date", "2026-11-19", "--time", "10:40 AM",
                                       "--now", "2026-11-19 10:40", "--config", os.path.join(d, "none.json"),
                                       "--write", path])
            self.assertEqual(code, 0, out)
            self.assertIn("rounded down to the ₹0.10 tick", out)
            self.assertIn("Flat by       15:15 IST", out)
            with open(path) as fh:
                plan = json.load(fh)
        finally:
            shutil.rmtree(d)
        self.assertEqual((plan["market"], plan["stop"], plan["target"], plan["risk_per_share"], plan["flat_by"]),
                         ("nse", 1360.9, 1383.4, 7.5, "15:15"))

    def test_plan_from_a_nifty_entry_warns_it_is_an_index(self):
        d = tempfile.mkdtemp()
        try:
            ep = os.path.join(d, "entry.json")
            run(entry, ["check", "NIFTY", "--offline", NIFTY, "--now", "2026-11-19 09:55", "--write", ep])
            code, out, _ = run(exitp, ["plan", "--from", ep, "--target", "1R", "--now", "2026-11-19 09:55",
                                       "--config", os.path.join(d, "none.json")])
        finally:
            shutil.rmtree(d)
        self.assertEqual(code, 0, out)
        self.assertIn("Entry         25,257.10 at 9:50 AM (bar 6)", out)
        self.assertIn("Stop          25,185.62", out)
        self.assertIn("^NSEI is an index: you cannot buy it", out)

    def test_atr_stop_on_nse_bars(self):
        code, out, _ = run(exitp, ["plan", "--symbol", "RELIANCE.NS", "--entry", "1368.40", "--stop", "atr:1.5",
                                   "--setup", "level", "--date", "2026-11-19", "--time", "10:40 AM", "--entry-bar", "16",
                                   "--offline", REL, "--now", "2026-11-19 10:40", "--config", "/nonexistent.json"])
        self.assertEqual(code, 0, out)
        self.assertIn("Stop          ₹1,364.10  1.5 x ATR(14)", out)


if __name__ == "__main__":
    unittest.main()

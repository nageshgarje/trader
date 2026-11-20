"""The rest of the pack on India's NSE: scanner, time machine, budget, risk, broker (Zerodha), alerts,
news and analyst. Fixtures were saved from the real sources on 2026-11-19/20."""

import contextlib
import io
import json
import os
import shutil
import tempfile
import unittest

from _load import load, ROOT, FIX

NSE = os.path.join(FIX, "nse")
scan = load("trader-scanner", "scan")
tm = load("trader-timemachine", "timemachine")
size = load("trader-budget", "size")
risk = load("trader-risk", "check")
ticket = load("trader-broker", "ticket")
alerts = load("trader-alerts", "alerts")
news = load("trader-news", "news")
facts = load("trader-analyst", "facts")
exitp = load("trader-exit", "exit")

CONFIG = {"budget": 5000, "max_loss_per_trade_usd": 50, "paper": True,
          "nse": {"budget": 100000, "max_loss_per_trade_usd": 500}}
NOW = "2026-11-20 10:40"


def run(mod, argv):
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        code = mod.main(argv)
    return code, out.getvalue(), err.getvalue()


class InTemp(unittest.TestCase):
    """Each test runs in its own folder with a trader/ config and a RELIANCE plan from trader-exit."""

    def setUp(self):
        self.old = os.getcwd()
        self.dir = tempfile.mkdtemp()
        os.chdir(self.dir)
        os.makedirs("trader/state")
        self.write("trader/config.json", CONFIG)
        code, out, err = run(exitp, ["plan", "--symbol", "RELIANCE.NS", "--entry", "1368.40", "--or-high", "1366.70",
                                     "--or-low", "1355.20", "--date", "2026-11-20", "--time", "10:40 AM",
                                     "--now", NOW, "--write"])
        assert code == 0, out + err

    def tearDown(self):
        os.chdir(self.old)
        shutil.rmtree(self.dir)

    def write(self, path, doc):
        with open(path, "w") as fh:
            json.dump(doc, fh)

    def read(self, path):
        with open(path) as fh:
            return json.load(fh)


class Pipeline(InTemp):
    def test_budget_writes_the_buy_and_its_sl_m_stop_in_rupees(self):
        code, out, _ = run(size, ["--now", NOW, "--write"])
        self.assertEqual(code, 0, out)
        self.assertIn("max loss      ₹500 max loss / 7.50 risk a share", out)
        self.assertIn("Size: 54 shares of RELIANCE.NS = ₹73,893.60", out)
        doc = self.read("trader/orders.json")
        self.assertEqual((doc["market"], doc["broker"]), ("nse", "zerodha"))
        buy, stop = doc["orders"]
        self.assertEqual((buy["side"], buy["qty"], buy["type"], buy["product"]), ("buy", "54", "market", "MIS"))
        self.assertNotIn("order_class", buy)
        self.assertEqual((stop["side"], stop["type"], stop["stop_price"], stop["stop_for"]), ("sell", "stop", "1360.90", 1))
        self.assertNotIn("$", buy["why"])

    def test_budget_never_reads_the_dollar_budget_as_rupees(self):
        self.write("trader/config.json", {"budget": 5000, "max_loss_per_trade_usd": 50})
        code, _, err = run(size, ["--now", NOW])
        self.assertEqual(code, 1)
        self.assertIn("no rupee budget for NSE", err)

    def test_risk_passes_the_pair_and_blocks_after_15_00(self):
        run(size, ["--now", NOW, "--write"])
        code, out, _ = run(risk, ["--now", NOW])
        self.assertEqual(code, 0, out)
        self.assertIn("the stop at the broker is the SL-M sell 1360.90", out)
        self.assertIn("max loss ₹405 (limit ₹500)", out)
        self.assertIn("₹1,500 daily loss", out)                    # 3 x the max loss per trade
        self.assertIn("Zerodha has no paper account", out)
        code, out, _ = run(risk, ["--now", "2026-11-20 15:05", "--no-write"])
        self.assertEqual(code, 1)
        self.assertIn("no new entries after 15:00", out)
        code, out, _ = run(risk, ["--now", "2026-11-10 10:40", "--no-write"])
        self.assertIn("NSE holiday: Diwali-Balipratipada", out)

    def test_risk_blocks_what_zerodha_cannot_take(self):
        base = {"symbol": "RELIANCE.NS", "side": "buy", "qty": "5", "type": "market", "time_in_force": "day",
                "market": "nse", "product": "MIS", "plan": {"entry": 1368.4, "stop": 1360.9, "target": 1383.4}}
        bad = [dict(base, order_class="bracket", take_profit_limit_price="1383.40", stop_loss_stop_price="1360.90"),
               dict(base, product="CNC"), dict(base, symbol="^NSEI")]
        self.write("trader/orders.json", {"market": "nse", "orders": bad})
        code, out, _ = run(risk, ["--now", NOW, "--no-write"])
        self.assertEqual(code, 1)
        self.assertIn("Zerodha takes no bracket orders", out)
        self.assertIn("product MIS", out)
        self.assertIn("is an index and cannot be bought", out)

    def test_broker_kite_tickets_and_the_stop_waits_for_its_buy(self):
        run(size, ["--now", NOW, "--write"])
        run(risk, ["--now", NOW])
        code, out, _ = run(ticket, ["show", "--json", "--now", NOW])
        t1, t2 = json.loads(out)
        self.assertEqual((t1["tool"], t1["broker"], t1["ready"]), ("place_order", "zerodha", True))
        self.assertEqual(t1["tool_input"], {"variety": "regular", "exchange": "NSE", "tradingsymbol": "RELIANCE",
                                            "transaction_type": "BUY", "quantity": 54, "product": "MIS",
                                            "order_type": "MARKET", "validity": "DAY"})
        self.assertEqual((t2["tool_input"]["order_type"], t2["tool_input"]["trigger_price"], t2["ready"]),
                         ("SL-M", 1360.9, False))
        code, _, err = run(ticket, ["approve", "--index", "2", "--reply", "yes", "--now", NOW])
        self.assertIn("Place order 1 first", err)
        code, out, _ = run(ticket, ["approve", "--index", "1", "--reply", "yes", "--now", NOW])
        self.assertIn("Paper: call no broker tool", out)
        run(ticket, ["result", "--index", "1", "--status", "placed", "--note", "paper", "--filled-qty", "54",
                     "--filled-price", "1368.50", "--now", NOW])
        self.assertTrue(os.path.exists("trader/portfolio.nse.json"))
        self.assertTrue(os.path.exists("trader/state/broker.nse.json"))
        self.assertFalse(os.path.exists("trader/portfolio.json"))
        self.assertEqual(self.read("trader/portfolio.nse.json")["cash"], 100000 - 54 * 1368.5)
        code, out, _ = run(ticket, ["show", "--index", "2", "--json", "--now", NOW])
        self.assertTrue(json.loads(out)["ready"])

    def test_flatten_cancels_the_stop_first(self):
        code, out, _ = run(ticket, ["flatten", "--symbol", "RELIANCE.NS", "--qty", "54", "--now", "2026-11-20 15:14"])
        self.assertEqual(code, 0)
        self.assertIn("Cancel the SL-M stop first", out)
        self.assertIn("flat by 15:15", out)
        order = self.read("trader/orders.json")["orders"][0]
        self.assertEqual((order["side"], order["market"], order["product"]), ("sell", "nse", "MIS"))

    def test_alerts_from_the_plan_ring_on_ist(self):
        code, out, _ = run(alerts, ["add", "--from-plan"])
        self.assertEqual(code, 0)
        rules = self.read("trader/alerts.json")["rules"]
        self.assertEqual({r["market"] for r in rules}, {"nse"})
        self.assertEqual([r for r in rules if r["type"] == "time"][0]["at"], "15:15")
        code, out, _ = run(alerts, ["check", "--offline", NSE, "--now", "2026-11-20 10:00", "--no-write"])
        self.assertIn("NSE OPEN (2026-11-20 10:00 India", out)
        self.assertIn("ALERT RELIANCE.NS is ₹1359.60: at or below your stop ₹1360.90", out)
        self.assertIn("Your SL-M stop already sits at Zerodha", out)
        code, out, _ = run(alerts, ["clock", "--market", "nse", "--now", "2026-11-10 10:00"])
        self.assertIn("NSE holiday: Diwali-Balipratipada", out)
        self.assertIn("Next open: 2026-11-11 09:15", out)


class Tools(unittest.TestCase):
    def tearDown(self):
        for m in (scan, tm, alerts, news, facts):
            m.use_market("us")

    def test_scanner_universe_from_the_nifty_500(self):
        d = tempfile.mkdtemp()
        try:
            out_path = os.path.join(d, "u.txt")
            code, out, _ = run(scan, ["universe", "--market", "nse", "--offline", scan.NSE_SNAPSHOT, "--out", out_path])
            self.assertEqual(code, 0, out)
            self.assertIn("NIFTY 500, EQ series: 496 stocks", out)
            with open(out_path) as fh:
                lines = fh.read().splitlines()
        finally:
            shutil.rmtree(d)
        self.assertIn("RELIANCE.NS|Reliance Industries Ltd.", lines)
        self.assertTrue(all(ln.split("|")[0].endswith(".NS") for ln in lines))

    def test_scanner_marks_symbols_as_nse(self):
        scan.use_market("nse")
        self.assertEqual(scan.to_yahoo("RELIANCE"), "RELIANCE.NS")
        self.assertEqual(scan.session(scan.datetime.date(2026, 11, 20)), (scan.datetime.time(9, 15),
                                                                           scan.datetime.time(15, 30)))

    def test_time_machine_replays_an_nse_day_to_15_15(self):
        code, out, err = run(tm, ["run", "--symbol", "RELIANCE.NS", "--date", "2026-11-19", "--entry", "1367.10",
                                  "--stop", "1360.95", "--target", "1373.25", "--entry-time", "1:30 PM",
                                  "--offline", os.path.join(NSE, "RELIANCE.NS-60d5m-2026-11-19.json.gz"),
                                  "--now", "2026-11-19 15:40", "--json"])
        self.assertEqual(code, 0, err)
        doc = json.loads(out)
        self.assertEqual((doc["market"], doc["mc"]["until"], doc["mc"]["pool_sessions"]), ("nse", "15:15", 57))
        self.assertEqual((round(doc["mc"]["p_target"], 3), round(doc["mc"]["p_stop"], 3)), (0.397, 0.322))
        self.assertEqual(tm.SESSION_BARS, 78)          # back to the US after the run

    def test_news_from_google_news_india(self):
        d = tempfile.mkdtemp()
        try:
            code, out, err = run(news, ["--tickers", "RELIANCE.NS", "--offline", NSE, "--no-moves", "--all",
                                        "--hours", "100000", "--state", os.path.join(d, "s.json"), "--json"])
        finally:
            shutil.rmtree(d)
        doc = json.loads(out)
        self.assertTrue(doc["items"], err)
        self.assertTrue(all(i["source"].startswith("Google News") for i in doc["items"]))
        self.assertFalse(any("SEC" in n for n in doc["notes"]))
        self.assertIn("hl=en-IN", news.gnews_url("RELIANCE.NS", "Reliance Industries Ltd."))
        self.assertIn("%22Reliance%20Industries%22", news.gnews_url("RELIANCE.NS", "Reliance Industries Ltd."))

    def test_analyst_from_yahoo_figures_in_crore(self):
        code, out, err = run(facts, ["RELIANCE.NS", "--offline", NSE])
        self.assertEqual(code, 0, err)
        self.assertIn("Rs crore except EPS. Source: Yahoo Finance", out)
        self.assertIn("crore", out)
        self.assertNotIn("$", out)
        code, _, err = run(facts, ["RELIANCE.NS", "AAPL", "--offline", NSE])
        self.assertEqual(code, 2)
        self.assertIn("separately", err)


if __name__ == "__main__":
    unittest.main()

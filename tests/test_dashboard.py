"""The dashboard: it reads what the skills wrote, shows it, and changes nothing. Offline (no quotes)."""

import contextlib
import hashlib
import io
import json
import os
import shutil
import tempfile
import threading
import unittest
import urllib.request

from _load import load, ROOT

dash = load("dashboard", "dashboard")
exitp = load("trader-exit", "exit")
size = load("trader-budget", "size")
risk = load("trader-risk", "check")
ticket = load("trader-broker", "ticket")
alerts = load("trader-alerts", "alerts")

NOW = "2026-11-20 10:40"


def run(mod, argv):
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        code = mod.main(argv)
    return code, out.getvalue(), err.getvalue()


def tree_hash(folder):
    h = hashlib.sha256()
    for base, _, files in sorted(os.walk(folder)):
        for f in sorted(files):
            p = os.path.join(base, f)
            h.update(p.encode())
            with open(p, "rb") as fh:
                h.update(fh.read())
    return h.hexdigest()


class Dashboard(unittest.TestCase):
    def setUp(self):
        self.old = os.getcwd()
        self.dir = tempfile.mkdtemp()
        os.chdir(self.dir)
        os.makedirs("trader/state")
        self.quotes = dash.Quotes(enabled=False)

    def tearDown(self):
        os.chdir(self.old)
        shutil.rmtree(self.dir)

    def build_nse_trade(self):
        with open("trader/config.json", "w") as fh:
            json.dump({"paper": True, "nse": {"budget": 100000, "max_loss_per_trade_usd": 500}}, fh)
        run(exitp, ["plan", "--symbol", "RELIANCE.NS", "--entry", "1368.40", "--or-high", "1366.70", "--or-low",
                    "1355.20", "--date", "2026-11-20", "--time", "10:40 AM", "--now", NOW, "--write"])
        run(size, ["--now", NOW, "--write"])
        run(risk, ["--now", NOW])
        run(alerts, ["add", "--from-plan"])
        with open("trader/prices.json", "w") as fh:
            json.dump({"as_of": "2026-11-20T05:10:00Z", "prices": {"RELIANCE.NS": 1371.25}}, fh)

    def test_an_empty_folder_renders(self):
        s = dash.state(".", self.quotes)
        self.assertEqual([m["exchange"] for m in s["markets"]], ["NYSE", "NSE"])
        for k in ("plan", "timemachine", "watchlist", "risk"):
            self.assertIsNone(s[k], k)
        self.assertEqual((s["orders"], s["alerts"], s["portfolios"], s["log"]), ([], [], [], []))
        self.assertTrue(s["paper"])

    def test_it_shows_the_nse_trade_and_changes_nothing(self):
        self.build_nse_trade()
        before = tree_hash("trader")
        s = dash.state(".", self.quotes)
        self.assertEqual(tree_hash("trader"), before)             # read only
        self.assertEqual((s["plan"]["symbol"], s["plan"]["market"]), ("RELIANCE.NS", "nse"))
        self.assertEqual(s["plan"]["quote"], {"price": 1371.25, "prev_close": None, "time": "2026-11-20T05:10:00Z",
                                              "source": "trader/prices.json"})
        self.assertEqual(s["orders_market"], "nse")
        self.assertEqual([o["tool"] for o in s["orders"]], ["place_order", "place_order"])
        self.assertEqual([o["verdict"] for o in s["orders"]], ["PASS", "PASS"])
        self.assertIn("WAITING", s["orders"][1]["status"])        # the SL-M waits for its buy
        self.assertIn("₹", s["orders"][0]["plain"])
        self.assertEqual({a["market"] for a in s["alerts"]}, {"nse"})
        self.assertEqual(len(s["alerts"]), 3)

    def test_positions_and_todays_log_per_market(self):
        self.build_nse_trade()
        today = ticket.use_market("nse") and ticket.to_eastern(ticket.parse_now(None)).date().isoformat()
        ticket.use_market("us")
        with open("trader/portfolio.nse.json", "w") as fh:
            json.dump({"cash": 26101.0, "positions": [{"ticker": "RELIANCE.NS", "shares": 54,
                                                       "lots": [{"shares": 54, "cost": 73899.0}]}]}, fh)
        with open("trader/state/broker.nse.json", "w") as fh:
            json.dump({"orders": [{"at": today + "T10:41:00+05:30", "date": today, "status": "placed",
                                   "plain": "BUY 54 RELIANCE.NS / max loss about $405"}]}, fh)
        s = dash.state(".", self.quotes)
        pf = s["portfolios"][0]
        self.assertEqual((pf["market"], pf["cash"], pf["currency"]), ("nse", 26101.0, "INR"))
        pos = pf["positions"][0]
        self.assertEqual((pos["symbol"], pos["last"], pos["value"], pos["pnl"]),
                         ("RELIANCE.NS", 1371.25, 74047.5, 148.5))
        self.assertEqual(s["log"][0]["plain"], "BUY 54 RELIANCE.NS / max loss about ₹405")

    def test_snapshot_bakes_the_data_in(self):
        self.build_nse_trade()
        code, out, _ = run(dash, ["snapshot", "--no-quotes"])
        self.assertEqual(code, 0, out)
        with open("trader/dashboard.html", encoding="utf-8") as fh:
            text = fh.read()
        self.assertNotIn("/*__SNAPSHOT__*/null", text)
        self.assertIn('"RELIANCE.NS"', text)

    def test_the_server_answers_on_localhost(self):
        self.build_nse_trade()
        server = dash.ThreadingHTTPServer(("127.0.0.1", 0), dash.make_handler(".", self.quotes))
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            base = "http://127.0.0.1:%d" % server.server_address[1]
            with urllib.request.urlopen(base + "/") as r:
                self.assertIn("Trader Dashboard", r.read().decode())
            with urllib.request.urlopen(base + "/api/state") as r:
                self.assertEqual(json.loads(r.read().decode())["plan"]["symbol"], "RELIANCE.NS")
            req = urllib.request.Request(base + "/api/state", data=b"{}", method="POST")
            with self.assertRaises(urllib.error.HTTPError):      # nothing can be written through it
                urllib.request.urlopen(req)
        finally:
            server.shutdown()
            server.server_close()

    def test_no_trader_folder_is_a_plain_error(self):
        shutil.rmtree("trader")
        code, _, err = run(dash, ["state", "--no-quotes"])
        self.assertEqual(code, 1)
        self.assertIn("no trader/ folder", err)


if __name__ == "__main__":
    unittest.main()

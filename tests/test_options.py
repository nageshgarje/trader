"""Option chain and signals (dashboard/options.py). Offline: fixtures stand in for Kite, NSE and Yahoo."""

import contextlib
import io
import json
import os
import shutil
import tempfile
import unittest
import urllib.parse

from _load import load, FIX

opt = load("dashboard", "options")
dash = load("dashboard", "dashboard")

F = os.path.join(FIX, "options")
CSV = os.path.join(F, "option-chain-ED-NIFTY-24-Nov-2026.csv")
NOW = "2026-11-20 10:40"          # a Friday, after the 09:15-09:30 opening range


def yahoo(url):
    with open(os.path.join(F, "yahoo-NSEI-5m.json")) as fh:
        return fh.read()


def kite_get(path):
    if path.startswith("/instruments/"):
        with open(os.path.join(F, "kite-instruments-NFO.csv")) as fh:
            return fh.read()
    with open(os.path.join(F, "kite-quotes.json")) as fh:
        data = json.load(fh)["data"]
    keys = [v for _, v in urllib.parse.parse_qsl(path.split("?", 1)[1])]
    return json.dumps({"status": "success", "data": {k: data[k] for k in keys if k in data}})


CFG = {"nse": {"max_loss_per_trade_usd": 5000}}


class Maths(unittest.TestCase):
    def test_implied_vol_round_trips(self):
        T = 4 / 365
        for kind in ("CE", "PE"):
            p = opt.bs_price(25812, 25800, T, 0.065, 0.13, kind)
            self.assertAlmostEqual(opt.implied_vol(p, 25812, 25800, T, 0.065, kind), 0.13, places=4)
        self.assertIsNone(opt.implied_vol(1.0, 25812, 25000, T, 0.065, "CE"))   # below intrinsic

    def test_symbols(self):
        self.assertEqual(opt.underlying("nifty 50"), "NIFTY")
        self.assertEqual(opt.underlying("RELIANCE.NS"), "RELIANCE")
        self.assertEqual(opt.spec_for("SENSEX"), ("BFO", "BSE:SENSEX", "^BSESN"))
        self.assertEqual(opt.spec_for("TCS"), ("NFO", "NSE:TCS", "TCS.NS"))
        with self.assertRaises(opt.OptionsError):
            opt.underlying("bad name!")


class Chain(unittest.TestCase):
    def test_nse_csv_is_read_with_both_sides(self):
        ch = opt.chain_from_nse_csv(CSV)
        self.assertEqual((ch["symbol"], ch["expiry"], ch["source"]), ("NIFTY", "2026-11-24", "nse-csv"))
        row = next(r for r in ch["rows"] if r["strike"] == 26000)
        self.assertEqual((row["ce"]["oi"], row["pe"]["oi"]), (1820000, 162900))
        self.assertAlmostEqual(row["ce"]["iv"], 0.13)

    def test_analysis_finds_the_walls_and_max_pain(self):
        d = opt.build("NIFTY", csv_path=CSV, lot=75, now=NOW, cfg=CFG, yahoo_get=yahoo)
        a = d["analysis"]
        self.assertEqual(d["spot"], 25812)                      # from the last Yahoo bar
        self.assertEqual((a["atm"], a["step"], a["support"], a["resistance"]), (25800, 50, 25600, 26000))
        self.assertEqual((a["pcr"], a["max_pain"]), (1.23, 25800))
        self.assertGreater(a["near_put_oi_change"], a["near_call_oi_change"])
        self.assertEqual(d["opening_range"], {"high": 25760, "low": 25700, "mid": 25730.0, "until": "09:30"})

    def test_kite_chain_matches_and_picks_the_nearest_expiry(self):
        k = opt.Kite("key", "token", kite_get)
        d = opt.build("NIFTY", kite=k, now=NOW, cfg=CFG, yahoo_get=yahoo)
        self.assertEqual((d["source"], d["expiry"], d["lot"], d["spot"]), ("kite", "2026-11-24", 75, 25812.0))
        self.assertEqual(d["expiries"], ["2026-11-24", "2026-12-01"])
        self.assertEqual(d["analysis"]["resistance"], 26000)
        self.assertIsNone(d["analysis"]["near_call_oi_change"])           # Kite has no previous-day OI
        base = {leg["tradingsymbol"]: leg["oi"] - 1000 for r in d["rows"] for leg in (r["ce"], r["pe"])}
        d2 = opt.build("NIFTY", kite=k, now=NOW, cfg=CFG, yahoo_get=yahoo, oi_base=base)
        self.assertEqual(d2["rows"][0]["ce"]["chg_oi"], 1000)
        with self.assertRaises(opt.OptionsError):
            opt.build("NIFTY", kite=k, expiry="2026-11-26", now=NOW, cfg=CFG, yahoo_get=yahoo)


class Signals(unittest.TestCase):
    def doc(self, now=NOW, cfg=CFG, spot=None):
        return opt.build("NIFTY", csv_path=CSV, lot=75, now=now, cfg=cfg, yahoo_get=yahoo, spot_override=spot)

    def test_breakout_with_the_chain_is_buy_ce(self):
        s = self.doc()["signal"]
        self.assertEqual((s["action"], s["score"]), ("BUY CE", 3))
        t = s["trade"]
        self.assertEqual((t["side"], t["strike"], t["under_stop"], t["under_target"]), ("CE", 25800, 25730, 26000))
        self.assertEqual(t["target_label"], "the call OI wall")
        self.assertLess(t["premium_stop"], t["premium"])
        self.assertGreater(t["premium_target"], t["premium"])
        self.assertEqual(t["lots"], 1)

    def test_one_lot_over_the_max_loss_is_skipped(self):
        s = self.doc(cfg={"nse": {"max_loss_per_trade_usd": 1000}})["signal"]
        self.assertEqual(s["action"], "NO TRADE")
        self.assertIn("more than your max loss", s["reasons"][-1])

    def test_inside_the_range_waits_and_times_matter(self):
        self.assertEqual(self.doc(spot=25740)["signal"]["action"], "WAIT")
        self.assertEqual(self.doc(now="2026-11-20 09:25")["signal"]["action"], "WAIT")
        self.assertEqual(self.doc(now="2026-11-20 14:45")["signal"]["action"], "NO TRADE")
        self.assertEqual(self.doc(now="2026-11-20 15:20")["signal"]["action"], "EXIT ALL")
        s = self.doc(now="2026-11-21 10:40")["signal"]                       # Saturday
        self.assertEqual((s["action"], s["reasons"]), ("NO TRADE", ["the market is closed"]))

    def test_a_breakdown_against_a_put_heavy_chain_is_not_a_trade(self):
        s = self.doc(spot=25690)["signal"]
        self.assertEqual(s["action"], "NO TRADE")
        self.assertIn("does not agree", s["reasons"][0])

    def test_exit_check(self):
        t = self.doc()["signal"]["trade"]
        t.update(lot=75, lots=1)
        self.assertEqual(opt.exit_check(t, 25850, 180, "2026-11-20 11:00")["action"], "HOLD")
        stop = opt.exit_check(t, 25725, 110, "2026-11-20 11:00")
        self.assertEqual(stop["action"], "EXIT")
        self.assertEqual(len(stop["reasons"]), 2)                             # level and premium
        self.assertEqual(opt.exit_check(t, 26005, 270, "2026-11-20 11:00")["action"], "EXIT")
        late = opt.exit_check(t, 25850, 180, "2026-11-20 15:16")
        self.assertIn("flat by 15:15", late["reasons"][0])


class Cli(unittest.TestCase):
    def setUp(self):
        self.old = os.getcwd()
        self.dir = tempfile.mkdtemp()
        os.chdir(self.dir)
        os.makedirs("trader")
        self.env = {k: os.environ.pop(k) for k in ("KITE_API_KEY", "KITE_ACCESS_TOKEN") if k in os.environ}

    def tearDown(self):
        os.chdir(self.old)
        shutil.rmtree(self.dir)
        os.environ.update(self.env)

    def run_cli(self, argv):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = opt.main(argv)
        return code, out.getvalue(), err.getvalue()

    def test_no_source_is_a_plain_error(self):
        code, _, err = self.run_cli(["chain", "NIFTY"])
        self.assertEqual(code, 1)
        self.assertIn("no Kite token and no --csv", err)

    def test_write_track_and_the_dashboard_reads_it(self):
        doc = opt.build("NIFTY", csv_path=CSV, lot=75, now=NOW, cfg=CFG, yahoo_get=yahoo)
        with open("trader/options.json", "w") as fh:
            json.dump(doc, fh)
        code, out, _ = self.run_cli(["track", "--now", NOW])
        self.assertEqual(code, 0, out)
        with open("trader/options.json") as fh:
            saved = json.load(fh)
        self.assertEqual((saved["trade"]["side"], saved["trade"]["lots"]), ("CE", 1))
        before = os.path.getmtime("trader/options.json")
        o = dash.state(".", dash.Quotes(enabled=False))["options"]      # --no-quotes: the saved chain as is
        self.assertEqual(os.path.getmtime("trader/options.json"), before)  # read only
        self.assertEqual((o["symbol"], o["mode"], o["signal"]["action"]), ("NIFTY", "saved", "BUY CE"))
        self.assertEqual(len(o["rows"]), 21)                                # ATM +-10 strikes
        self.assertIn(o["trade"]["check"]["action"], ("HOLD", "EXIT"))
        self.assertNotIn("oi_base", o)
        code, out, _ = self.run_cli(["untrack"])
        self.assertIn("Stopped", out)

    def test_dashboard_without_a_chain(self):
        self.assertIsNone(dash.state(".", dash.Quotes(enabled=False))["options"])
        with open("trader/config.json", "w") as fh:
            json.dump({"options": {"symbol": "BANKNIFTY"}}, fh)
        o = dash.state(".", dash.Quotes(enabled=False))["options"]
        self.assertIn("no chain for BANKNIFTY", o["error"])


if __name__ == "__main__":
    unittest.main()

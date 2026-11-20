"""Code that each skill keeps its own copy of (so a folder works on its own) must stay in step:
the NYSE calendar in six tools, and the order fingerprint shared by trader-risk and trader-broker."""

import datetime
import inspect
import unittest

from _load import load

CALENDAR_TOOLS = [("trader-risk", "check"), ("trader-alerts", "alerts"), ("trader-entry", "entry"),
                  ("trader-exit", "exit"), ("trader-scanner", "scan"), ("trader-timemachine", "timemachine")]
MODS = {folder: load(folder, name) for folder, name in CALENDAR_TOOLS}
RISK, BROKER = MODS["trader-risk"], load("trader-broker", "ticket")

# Published NYSE calendars before the pack's own tables (special closures such as
# 2025-01-09, the national day of mourning, are left out: no rule can know them).
PUBLISHED = {
    2022: ("2022-01-17 2022-02-21 2022-04-15 2022-05-30 2022-06-20 2022-07-04 2022-09-05 2022-11-24 "
           "2022-12-26", "2022-11-25"),
    2023: ("2023-01-02 2023-01-16 2023-02-20 2023-04-07 2023-05-29 2023-06-19 2023-07-04 2023-09-04 "
           "2023-11-23 2023-12-25", "2023-07-03 2023-11-24"),
    2024: ("2024-01-01 2024-01-15 2024-02-19 2024-03-29 2024-05-27 2024-06-19 2024-07-04 2024-09-02 "
           "2024-11-28 2024-12-25", "2024-07-03 2024-11-29 2024-12-24"),
    2025: ("2025-01-01 2025-01-20 2025-02-17 2025-04-18 2025-05-26 2025-06-19 2025-07-04 2025-09-01 "
           "2025-11-27 2025-12-25", "2025-07-03 2025-11-28 2025-12-24"),
}


def year_of(table, year):
    return {k: v for k, v in table.items() if k.startswith(str(year))}


class NyseCalendar(unittest.TestCase):
    def test_every_copy_of_the_rules_is_the_same_code(self):
        srcs = {f: inspect.getsource(m.nyse_rules) for f, m in MODS.items()}
        self.assertEqual(len(set(srcs.values())), 1, sorted(srcs))

    def test_the_published_tables_agree_across_tools(self):
        for f, m in MODS.items():
            self.assertEqual(m.CALENDAR_YEARS, (2026, 2027), f)
            self.assertEqual(m.NYSE_EARLY_CLOSE, RISK.NYSE_EARLY_CLOSE, f)
            if hasattr(m, "NYSE_HOLIDAYS"):
                self.assertEqual(m.NYSE_HOLIDAYS, RISK.NYSE_HOLIDAYS, f)

    def test_rules_reproduce_the_published_2026_and_2027_lists_word_for_word(self):
        for y in (2026, 2027):
            hol, early = RISK.nyse_rules(y)
            self.assertEqual(hol, year_of(RISK.NYSE_HOLIDAYS, y), y)
            self.assertEqual(early, year_of(RISK.NYSE_EARLY_CLOSE, y), y)

    def test_rules_reproduce_earlier_published_years(self):
        for y, (hol, early) in PUBLISHED.items():
            h, e = RISK.nyse_rules(y)
            self.assertEqual(sorted(h), hol.split(), y)
            self.assertEqual(sorted(e), early.split(), y)

    def test_years_after_the_published_list_are_filled(self):
        hol = RISK.NYSE_HOLIDAYS
        self.assertEqual(hol["2028-01-17"], "Martin Luther King, Jr. Day")
        self.assertEqual(hol["2028-04-14"], "Good Friday")
        self.assertNotIn("2027-12-31", hol)          # a Saturday New Year's Day is not made up
        self.assertEqual(hol["2033-12-26"], "Christmas Day (observed)")
        self.assertIn("2100-11-25", hol)
        self.assertEqual(RISK.NYSE_EARLY_CLOSE["2028-07-03"], "day before Independence Day")
        self.assertEqual(MODS["trader-timemachine"].NYSE_EARLY_CLOSE["2030-12-24"], "Christmas Eve")

    def test_risk_closes_the_window_on_a_2028_holiday_and_says_where_it_came_from(self):
        cfg = {"no_entries_before": "09:45", "no_entries_after": "15:30"}
        reasons, warns = RISK.window_check(datetime.datetime(2028, 1, 17, 10, 30), cfg)
        self.assertIn("NYSE holiday: Martin Luther King, Jr. Day", " ".join(reasons))
        self.assertIn("standing rules", " ".join(warns))
        reasons, _ = RISK.window_check(datetime.datetime(2028, 1, 18, 10, 30), cfg)
        self.assertEqual(reasons, [])

    def test_exit_and_scanner_see_the_holiday_too(self):
        self.assertIsNone(MODS["trader-exit"].session(datetime.date(2028, 11, 23)))
        self.assertIsNone(MODS["trader-scanner"].session(datetime.date(2028, 11, 23)))


class OrderFingerprint(unittest.TestCase):
    def test_risk_and_broker_hash_the_same_fields_the_same_way(self):
        self.assertEqual(RISK.ALPACA_FIELDS, BROKER.ALPACA_FIELDS)
        def body(fn):                    # the code, without the docstring that names the other file
            return [ln for ln in inspect.getsource(fn).splitlines() if not ln.strip().startswith('"""')]
        self.assertEqual(body(RISK.fingerprint), body(BROKER.fingerprint))
        order = {"symbol": "TSLA", "side": "buy", "qty": "8", "type": "market", "time_in_force": "day",
                 "order_class": "bracket", "take_profit_limit_price": "370.04",
                 "stop_loss_stop_price": "358.32", "why": "notes are not hashed"}
        self.assertEqual(RISK.fingerprint(order), BROKER.fingerprint(order))


if __name__ == "__main__":
    unittest.main()

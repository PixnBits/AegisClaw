#!/usr/bin/env python3
"""Operator journal cross-check for one profile run."""

from __future__ import annotations

import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

import check_ollama_journal as journal
import run_one

# e2-shaped lines from the pilot check. The cache note must not be summed.
WINDOW_JOURNAL = """\
2026-10-04T23:02:10Z host ollama[1]: slot print_timing: prompt eval time = 812.00 ms / 671 tokens (1.21 ms per token)
2026-10-04T23:02:40Z host ollama[1]: slot print_timing: eval time = 9400.00 ms / 2581 tokens (3.64 ms per token)
2026-10-04T23:02:10Z host ollama[1]: slot update_slots: cached tokens = 254, n_tokens = 671, lcp 254/671
2026-10-04T22:00:00Z host ollama[1]: prompt eval time = 1 ms / 999 tokens
2026-10-04T23:02:40Z host ollama[1]: total time = 10000 ms / 3252 tokens
"""


def _result(**overrides):
    base = {
        "started_at": "2026-10-04T23:02:06Z",
        "wall_s": 120,
        "tokens_prompt": 671,
        "tokens_prompt_raw": 671,
        "tokens_prompt_cache_adjusted": 671,
        "tokens_completion": 2581,
        "llm_calls": 1,
        "tokens_unattributed": 0,
        "tokens_prompt_unattributed": 0,
        "tokens_completion_unattributed": 0,
    }
    base.update(overrides)
    return base


class OllamaJournalTests(unittest.TestCase):
    def test_window_prefers_total_s_over_conclusion_wall_s(self):
        start, end = journal.run_window(_result(wall_s=10, total_s=50))
        self.assertEqual(
            (end - start).total_seconds(),
            journal.LEAD_S + 50 + journal.SETTLE_S,
        )

    def test_window_command_stops_before_a_later_judge(self):
        argv = journal.journalctl_argv(_result())
        self.assertEqual(
            argv,
            [
                "journalctl",
                "-u",
                "ollama",
                "-o",
                "short-iso",
                "--since",
                "2026-10-04 23:02:01 UTC",
                "--until",
                "2026-10-04 23:04:21 UTC",
                "--no-pager",
            ],
        )

    def test_timing_lines_ignore_cache_similarity_and_lines_outside_the_window(self):
        start, end = journal.run_window(_result())
        rows = journal.extract_timing(WINDOW_JOURNAL, start, end)
        self.assertEqual(rows, [
            {"kind": "prompt", "tokens": 671},
            {"kind": "completion", "tokens": 2581},
        ])
        compared = journal.compare_totals(_result(), rows)
        self.assertTrue(compared["prompt_match"])
        self.assertTrue(compared["completion_match"])
        self.assertEqual(compared["journal_prompt_tokens"], 671)

    def test_untimestamped_excerpt_is_kept(self):
        rows = journal.extract_timing(
            "prompt eval time = 10 ms / 4 tokens\neval time = 3 ms / 9 tokens\n",
        )
        self.assertEqual(
            rows,
            [{"kind": "prompt", "tokens": 4}, {"kind": "completion", "tokens": 9}],
        )

    def test_prompt_eval_line_is_not_also_completion(self):
        rows = journal.extract_timing(
            "prompt eval time = 10 ms / 671 tokens\n",
        )
        self.assertEqual(rows, [{"kind": "prompt", "tokens": 671}])

    def test_method_string_matches_the_harness(self):
        self.assertEqual(journal.TOKEN_CACHE_METHOD, run_one.TOKEN_CACHE_METHOD)
        self.assertEqual(
            journal.TOKEN_CACHE_METHOD_UNAVAILABLE,
            run_one.TOKEN_CACHE_METHOD_UNAVAILABLE,
        )
        text = Path(journal.__file__).read_text(encoding="utf-8")
        self.assertIn("journalctl -u ollama", text)
        self.assertIn("prompt eval time", text)
        self.assertIn(run_one.TOKEN_CACHE_METHOD, text)
        self.assertIn(run_one.TOKEN_CACHE_METHOD_UNAVAILABLE, text)

    def test_cli_matches_a_saved_journal_and_does_not_fail_on_prompt_below_raw(self):
        with tempfile.TemporaryDirectory() as tmp:
            run = Path(tmp)
            (run / "result.json").write_text(json.dumps(_result()), encoding="utf-8")
            excerpt = run / "journal.txt"
            excerpt.write_text(WINDOW_JOURNAL, encoding="utf-8")
            stdout = StringIO()
            with redirect_stdout(stdout):
                code = journal.main([str(run), "--journal", str(excerpt)])
            self.assertEqual(code, 0)
            self.assertIn("completion: match", stdout.getvalue())
            self.assertIn("call count: match", stdout.getvalue())
            self.assertIn("journalctl -u ollama", stdout.getvalue())

            cache_hit = (
                "2026-10-04T23:02:10Z host ollama[1]: "
                "prompt eval time = 53.33 ms / 12 tokens (4.44 ms per token)\n"
                "2026-10-04T23:02:10Z host ollama[1]: cached n_tokens = 1457\n"
                "2026-10-04T23:02:40Z host ollama[1]: eval time = 9400.00 ms / 2581 tokens\n"
            )
            excerpt.write_text(cache_hit, encoding="utf-8")
            (run / "result.json").write_text(
                json.dumps(_result(tokens_prompt_raw=1469, tokens_prompt=1469)),
                encoding="utf-8",
            )
            stdout = StringIO()
            with redirect_stdout(stdout), redirect_stderr(StringIO()):
                code = journal.main([str(run), "--journal", str(excerpt)])
            self.assertEqual(code, 0)
            self.assertIn("cache-adjusted below raw", stdout.getvalue())

    def test_cli_fails_on_completion_or_call_count_mismatch(self):
        with tempfile.TemporaryDirectory() as tmp:
            run = Path(tmp)
            excerpt = run / "journal.txt"
            excerpt.write_text(WINDOW_JOURNAL, encoding="utf-8")
            (run / "result.json").write_text(
                json.dumps(_result(tokens_completion=1)),
                encoding="utf-8",
            )
            with redirect_stdout(StringIO()), redirect_stderr(StringIO()):
                self.assertEqual(journal.main([str(run), "--journal", str(excerpt)]), 1)
            (run / "result.json").write_text(
                json.dumps(_result(llm_calls=9)),
                encoding="utf-8",
            )
            with redirect_stdout(StringIO()), redirect_stderr(StringIO()):
                self.assertEqual(journal.main([str(run), "--journal", str(excerpt)]), 1)

    def test_cache_hit_prompt_sum_is_reported_not_failed(self):
        start, end = journal.run_window(_result(started_at="2026-10-04T23:02:06Z"))
        text = (
            "2026-10-04T23:02:10Z host ollama[1]: "
            "prompt eval time = 53.33 ms / 12 tokens (4.44 ms per token)\n"
            "2026-10-04T23:02:10Z host ollama[1]: cached n_tokens = 1457\n"
            "2026-10-04T23:02:40Z host ollama[1]: eval time = 100.00 ms / 100 tokens\n"
        )
        rows = journal.extract_timing(text, start, end)
        compared = journal.compare_totals(
            _result(
                tokens_prompt=1469,
                tokens_prompt_raw=1469,
                tokens_completion=100,
                llm_calls=1,
            ),
            rows,
        )
        self.assertEqual(compared["journal_prompt_tokens"], 12)
        self.assertTrue(compared["prompt_lt_raw"])
        self.assertFalse(compared["prompt_match"])
        self.assertTrue(compared["completion_match"])
        self.assertTrue(compared["calls_match"])

    def test_cli_without_excerpt_only_prints_the_command(self):
        with tempfile.TemporaryDirectory() as tmp:
            run = Path(tmp)
            (run / "result.json").write_text(json.dumps(_result()), encoding="utf-8")
            stdout = StringIO()
            with redirect_stdout(stdout):
                code = journal.main([str(run)])
            self.assertEqual(code, 0)
            self.assertIn("--since", stdout.getvalue())
            self.assertIn("no journal excerpt", stdout.getvalue())


    def test_prewarm_excluded_from_totals_and_gap(self):
        start, end = journal.run_window(_result(started_at="2026-10-05T12:25:23Z", wall_s=400, total_s=400))
        text = (
            "2026-10-05T12:25:23Z host ollama[1]: new prompt, task.n_tokens = 11\n"
            "2026-10-05T12:25:23Z host ollama[1]: prompt eval time = 10 ms / 11 tokens\n"
            "2026-10-05T12:25:23Z host ollama[1]: eval time = 100 ms / 367 tokens\n"
            "2026-10-05T12:25:54Z host ollama[1]: new prompt, task.n_tokens = 331\n"
            "2026-10-05T12:25:54Z host ollama[1]: prompt eval time = 10 ms / 331 tokens\n"
            "2026-10-05T12:25:54Z host ollama[1]: eval time = 100 ms / 2034 tokens\n"
            "2026-10-05T12:26:55Z host ollama[1]: new prompt, task.n_tokens = 519\n"
            "2026-10-05T12:26:55Z host ollama[1]: prompt eval time = 10 ms / 517 tokens\n"
            "2026-10-05T12:26:55Z host ollama[1]: eval time = 100 ms / 60 tokens\n"
        )
        calls = journal.extract_calls(text, start, end)
        self.assertEqual(len(calls), 3)
        self.assertTrue(calls[0]["prewarm"])
        self.assertFalse(calls[1]["prewarm"])
        totals = journal.journal_totals(calls)
        self.assertEqual(totals["calls"], 2)
        self.assertEqual(totals["prewarm_excluded"], 1)
        self.assertEqual(totals["completion"], 2094)
        self.assertEqual(totals["prompt_raw"], 331 + 519)
        gap, detail = journal.attribution_gap(
            _result(llm_calls=1, tokens_completion=2034, tokens_prompt_raw=331, tokens_prompt=331),
            totals,
        )
        self.assertTrue(gap)
        self.assertIn("calls api=1 journal=2", detail)
        self.assertIn("completion api=2034 journal=2094", detail)
        gap2, detail2 = journal.attribution_gap(
            _result(llm_calls=2, tokens_completion=2094, tokens_prompt_raw=850, tokens_prompt=850),
            totals,
        )
        self.assertFalse(gap2)
        self.assertIsNone(detail2)


if __name__ == "__main__":
    unittest.main()

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
        "tokens_unattributed": 0,
        "tokens_prompt_unattributed": 0,
        "tokens_completion_unattributed": 0,
    }
    base.update(overrides)
    return base


class OllamaJournalTests(unittest.TestCase):
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
        text = Path(journal.__file__).read_text(encoding="utf-8")
        self.assertIn("journalctl -u ollama", text)
        self.assertIn("prompt eval time", text)
        self.assertIn(run_one.TOKEN_CACHE_METHOD, text)

    def test_cli_matches_a_saved_journal_and_rejects_a_discount(self):
        with tempfile.TemporaryDirectory() as tmp:
            run = Path(tmp)
            (run / "result.json").write_text(json.dumps(_result()), encoding="utf-8")
            excerpt = run / "journal.txt"
            excerpt.write_text(WINDOW_JOURNAL, encoding="utf-8")
            stdout = StringIO()
            with redirect_stdout(stdout):
                code = journal.main([str(run), "--journal", str(excerpt)])
            self.assertEqual(code, 0)
            self.assertIn("prompt: match", stdout.getvalue())
            self.assertIn("journalctl -u ollama", stdout.getvalue())

            (run / "result.json").write_text(
                json.dumps(_result(tokens_prompt_raw=671 - 254, tokens_prompt=671 - 254)),
                encoding="utf-8",
            )
            with redirect_stdout(StringIO()), redirect_stderr(StringIO()):
                code = journal.main([str(run), "--journal", str(excerpt)])
            self.assertEqual(code, 1)

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


if __name__ == "__main__":
    unittest.main()

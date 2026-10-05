#!/usr/bin/env python3
"""Conclusion-signal tests for run_one.judge_signal / marker_hit."""

from __future__ import annotations

import json
import tempfile
import unittest
import unittest.mock
from pathlib import Path

import common
import run_one


def _msg(role: str, content: str) -> dict:
    return {"role": role, "content": content, "from": role}


def _signal(messages, **overrides):
    args = {
        "elapsed_since_accept": 120,
        "silence_s": 0,
        "turn_available": True,
        "any_pending": False,
        "min_wait_s": 60,
        "quiet_s": 90,
        "patterns": ["once you share", "guest count", "no further changes", "grading can start"],
    }
    args.update(overrides)
    return run_one.judge_signal(
        messages,
        args["elapsed_since_accept"],
        args["silence_s"],
        args["turn_available"],
        args["any_pending"],
        args["min_wait_s"],
        args["quiet_s"],
        args["patterns"],
    )


class ConclusionSignalTests(unittest.TestCase):
    def test_pm_only_clarifying_ask_is_not_final_marker(self):
        messages = [
            _msg("user", "Please plan a birthday party."),
            _msg(
                "pm",
                "picking a date, setting your budget, and deciding on the guest count. "
                "Once you share those details",
            ),
        ]
        self.assertFalse(run_one.marker_hit(messages, ["once you share", "guest count"]))
        self.assertIsNone(_signal(messages, silence_s=10))
        # Someone spoke, then the channel went quiet. That is quiet, not final_marker.
        self.assertEqual(_signal(messages, silence_s=90), "quiet")

    def test_marker_on_plan_does_not_count_after_unrelated_agent_reply(self):
        messages = [
            _msg("pm", "Once you share those details we can continue."),
            _msg("agent", "Looking at the request."),
        ]
        self.assertFalse(run_one.marker_hit(messages, ["once you share"]))
        self.assertIsNone(_signal(messages, patterns=["once you share"], silence_s=0))

    def test_pm_plan_agent_reply_and_marker_is_final_marker(self):
        messages = [
            _msg("user", "Add Truncate."),
            _msg("pm", "I will ask an agent to deliver the files."),
            _msg("agent", "Done. No further changes. Grading can start."),
        ]
        self.assertTrue(run_one.marker_hit(messages, ["no further changes", "grading can start"]))
        self.assertEqual(_signal(messages), "final_marker")
        # A later PM synthesis after the agent reply is also eligible.
        later = [
            _msg("pm", "Plan: implement Truncate."),
            _msg("agent", "Working on the function."),
            _msg("pm", "No further changes."),
        ]
        self.assertEqual(_signal(later, silence_s=200), "final_marker")

    def test_court_after_pm_is_not_an_agent_reply(self):
        messages = [
            _msg("pm", "Grading can start once the court agrees."),
            _msg("court", "Noted."),
        ]
        self.assertFalse(run_one.marker_hit(messages, ["grading can start"]))
        self.assertIsNone(_signal(messages, silence_s=0))

    def test_system_status_after_pm_is_not_an_agent_message_or_turn(self):
        parsed = common.parse_transcript(
            {
                "messages": [
                    {"from": "user", "content": "Fix the CSS."},
                    {
                        "from": "project-manager",
                        "content": "Plan: @Coder tweak padding. No further changes.",
                    },
                    {
                        "from": "system",
                        "content": "status: turns delivered to [project-manager]",
                    },
                ]
            }
        )
        self.assertEqual(common.classify_sender("system"), "system")
        self.assertEqual([message["role"] for message in parsed], ["user", "pm", "system"])
        self.assertFalse(run_one.marker_hit(parsed, ["no further changes"]))
        self.assertIsNone(_signal(parsed, patterns=["no further changes"], silence_s=0))
        _total, agent, pm, non_user, _senders = run_one.message_counts(parsed)
        self.assertEqual(agent, 0)
        self.assertEqual(pm, 1)
        self.assertEqual(non_user, 1)
        counted = run_one.with_transcript_turns(
            run_one.account("", "", [], []), parsed, True
        )
        self.assertEqual(counted["turns"], 1)
        result = run_one.build_result(
            arm="base",
            scenario="css",
            kind="probe",
            court_dependent=False,
            n=1,
            run_id="r",
            channel="c",
            completion_signal="quiet",
            timed_out=False,
            messages=parsed,
            metrics=counted,
            wall_s=1.0,
            drain_s=0.0,
            total_s=1.0,
            started_at="2026-10-04T23:02:06Z",
            run_dir="/tmp/run",
            error=None,
        )
        self.assertEqual(result["agent_messages"], 0)
        self.assertEqual(result["pm_messages"], 1)
        self.assertEqual(result["turns"], 1)

    def test_min_wait_blocks_final_marker(self):
        messages = [
            _msg("pm", "Plan."),
            _msg("agent", "No further changes."),
        ]
        self.assertIsNone(_signal(messages, elapsed_since_accept=10))

    def test_quiet_no_reply_when_nobody_answered(self):
        messages = [_msg("user", "Please plan a birthday party.")]
        self.assertIsNone(_signal(messages, elapsed_since_accept=120, silence_s=200))
        self.assertEqual(
            _signal(messages, elapsed_since_accept=180, silence_s=180),
            "quiet_no_reply",
        )
        # Turn-state must show nothing pending. A missing turn-state does not qualify.
        self.assertIsNone(
            _signal(
                messages,
                elapsed_since_accept=180,
                silence_s=180,
                turn_available=False,
            )
        )
        self.assertIsNone(
            _signal(
                messages,
                elapsed_since_accept=180,
                silence_s=180,
                any_pending=True,
            )
        )

    def test_quiet_after_agent_activity(self):
        messages = [
            _msg("user", "Fix the CSS."),
            _msg("pm", "I need the stylesheet before anyone edits."),
            _msg("agent", "I agree. Still waiting on the file."),
        ]
        self.assertIsNone(_signal(messages, patterns=["no further changes"], silence_s=10))
        self.assertEqual(
            _signal(messages, patterns=["no further changes"], silence_s=90),
            "quiet",
        )
        # Pending blocks quiet until the stale-pending escape.
        self.assertIsNone(
            _signal(messages, patterns=["no further changes"], silence_s=90, any_pending=True)
        )
        self.assertEqual(
            _signal(messages, patterns=["no further changes"], silence_s=180, any_pending=True),
            "quiet",
        )

    def test_clarifying_agent_question_does_not_fire_marker(self):
        patterns = ["no further changes", "guest count"]
        messages = [
            _msg("pm", "Plan: add the field once you pick a date."),
            _msg(
                "agent",
                "Should the guest count be required before I say no further changes?",
            ),
        ]
        self.assertTrue(run_one._is_clarifying_question("  no further changes?  "))
        self.assertFalse(run_one._is_clarifying_question("No further changes."))
        self.assertFalse(run_one._is_clarifying_question(None))
        self.assertFalse(run_one._is_clarifying_question(""))
        self.assertTrue(
            run_one._is_clarifying_question(
                "Should the guest count be required? I will wait before coding."
            )
        )
        self.assertFalse(
            run_one._is_clarifying_question(
                "Is the guest count required?\n```\npackage main\n```"
            )
        )
        self.assertFalse(
            run_one._is_clarifying_question(
                "Does this match?\n// file: main.go\npackage main\n"
            )
        )
        # A '?' inside a deliverable is not clarifying, so the marker can fire.
        fenced = messages[:1] + [
            _msg("agent", "No further changes?\n```\npackage main\n```"),
        ]
        self.assertTrue(run_one.marker_hit(fenced, patterns))
        filed = messages[:1] + [
            _msg("agent", "No further changes?\n// file: main.go\npackage main\n"),
        ]
        self.assertTrue(run_one.marker_hit(filed, patterns))
        middle = messages[:1] + [
            _msg(
                "agent",
                "Should the guest count be required? I will not say no further changes yet.",
            ),
        ]
        self.assertTrue(run_one._is_clarifying_question(middle[1]["content"]))
        self.assertIsNone(run_one._first_agent_reply_index(middle))
        self.assertFalse(run_one.marker_hit(middle, patterns))
        self.assertIsNone(run_one._first_agent_reply_index(messages))
        self.assertFalse(run_one.marker_hit(messages, patterns))
        self.assertIsNone(_signal(messages, patterns=patterns, silence_s=0))
        self.assertEqual(_signal(messages, patterns=patterns, silence_s=90), "quiet")
        # A later non-clarifying agent reply can still carry the marker.
        followed = messages + [_msg("agent", "No further changes.")]
        self.assertEqual(run_one._first_agent_reply_index(followed), 2)
        self.assertTrue(run_one.marker_hit(followed, patterns))
        self.assertEqual(_signal(followed, patterns=patterns, silence_s=0), "final_marker")

    def test_later_pm_marker_fires_after_only_clarifying_agent_replies(self):
        patterns = ["no further changes", "guest count"]
        messages = [
            _msg("pm", "Plan: add the field once you pick a date."),
            _msg(
                "agent",
                "Should the guest count be required before I say no further changes?",
            ),
            _msg("pm", "No further changes."),
        ]
        self.assertTrue(run_one.marker_hit(messages, patterns))
        self.assertEqual(_signal(messages, patterns=patterns, silence_s=0), "final_marker")


def _result(metrics):
    return run_one.build_result(
        arm="base",
        scenario="e2",
        kind="engineering",
        court_dependent=False,
        n=1,
        run_id="r",
        channel="prof-e2-base-n1-abcdef",
        completion_signal="quiet",
        timed_out=False,
        messages=[],
        metrics=metrics,
        wall_s=1.0,
        drain_s=0.0,
        total_s=1.0,
        started_at="2026-10-04T23:02:06Z",
        run_dir="/tmp/run",
        error=None,
    )


class TokenCaptureTests(unittest.TestCase):
    def test_account_copies_raw_and_splits_unattributed(self):
        metrics = run_one.account(
            "",
            "",
            [
                {"tokens_prompt": 671, "tokens_completion": 2581},
                {"tokens_prompt": 10, "tokens_completion": 4},
            ],
            [
                {"tokens_prompt": 3, "tokens_completion": 8},
                {"tokens_prompt": 1, "tokens_completion": 1},
            ],
        )
        self.assertEqual(metrics["tokens_prompt"], 681)
        self.assertEqual(metrics["tokens_prompt_raw"], 681)
        self.assertIsNone(metrics["tokens_prompt_cache_adjusted"])
        self.assertEqual(metrics["tokens_cache_method"], run_one.TOKEN_CACHE_METHOD_UNAVAILABLE)
        self.assertIsNone(metrics["journal_llm_calls"])
        self.assertEqual(metrics["tokens_completion"], 2585)
        self.assertEqual(metrics["llm_calls"], 2)
        self.assertEqual(metrics["tokens_prompt_unattributed"], 4)
        self.assertEqual(metrics["tokens_completion_unattributed"], 9)
        self.assertEqual(metrics["tokens_unattributed"], 13)
        result = _result(metrics)
        self.assertEqual(
            [key for key in result if key.startswith("tokens_")],
            [
                "tokens_prompt",
                "tokens_prompt_raw",
                "tokens_prompt_cache_adjusted",
                "tokens_cache_method",
                "tokens_completion",
                "tokens_unattributed",
                "tokens_prompt_unattributed",
                "tokens_completion_unattributed",
            ],
        )
        self.assertIn("journal_llm_calls", result)
        doc = run_one._usage_delta_doc([], [], metrics)
        self.assertIsNone(doc["tokens_prompt_cache_adjusted"])
        self.assertEqual(doc["tokens_prompt_unattributed"], 4)
        self.assertEqual(doc["tokens_completion_unattributed"], 9)

    def test_empty_metrics_leave_cache_adjusted_null(self):
        result = _result(run_one._empty_metrics())
        self.assertEqual(result["tokens_prompt"], 0)
        self.assertEqual(result["tokens_prompt_raw"], 0)
        self.assertEqual(result["drain_s"], 0.0)
        self.assertEqual(result["total_s"], result["wall_s"])
        keys = list(result)
        self.assertLess(keys.index("wall_s"), keys.index("drain_s"))
        self.assertLess(keys.index("drain_s"), keys.index("total_s"))
        self.assertLess(keys.index("total_s"), keys.index("turns"))
        self.assertIsNone(result["tokens_prompt_cache_adjusted"])
        self.assertIsNone(result["journal_llm_calls"])
        self.assertEqual(
            result["tokens_cache_method"],
            "api_prompt_eval_count_full__journal_unavailable",
        )
        self.assertEqual(result["tokens_unattributed"], 0)

    def test_build_result_allows_journal_cache_adjusted_below_raw(self):
        metrics = run_one.account("", "", [{"tokens_prompt": 1469, "tokens_completion": 100}], [])
        metrics["tokens_prompt_cache_adjusted"] = 12
        metrics["journal_llm_calls"] = 1
        metrics["tokens_cache_method"] = run_one.TOKEN_CACHE_METHOD
        result = _result(metrics)
        self.assertEqual(result["tokens_prompt"], 1469)
        self.assertEqual(result["tokens_prompt_raw"], 1469)
        self.assertEqual(result["tokens_prompt_cache_adjusted"], 12)
        self.assertEqual(result["journal_llm_calls"], 1)
        self.assertEqual(result["llm_calls"], 1)

    def test_journal_cache_hit_sets_adjusted_from_prompt_eval(self):
        metrics = run_one.account("", "", [{"tokens_prompt": 1469, "tokens_completion": 100}], [])
        journal_text = (
            "2026-10-04T23:02:10Z host ollama[1]: "
            "prompt eval time = 53.33 ms / 12 tokens (4.44 ms per token)\n"
            "2026-10-04T23:02:10Z host ollama[1]: cached n_tokens = 1457\n"
            "2026-10-04T23:02:40Z host ollama[1]: eval time = 100.00 ms / 100 tokens\n"
        )

        def run_cmd(argv):
            self.assertIn("journalctl", argv)
            return common.CmdResult(0, journal_text, "", False)

        out = run_one.apply_journal_cache(
            metrics, "2026-10-04T23:02:06Z", 120, run_cmd=run_cmd
        )
        self.assertEqual(out["tokens_prompt"], 1469)
        self.assertEqual(out["tokens_prompt_raw"], 1469)
        self.assertEqual(out["tokens_prompt_cache_adjusted"], 12)
        self.assertEqual(out["journal_llm_calls"], 1)
        self.assertEqual(out["tokens_cache_method"], run_one.TOKEN_CACHE_METHOD)
        self.assertEqual(out["llm_calls"], 1)
        result = _result(out)
        self.assertEqual(result["tokens_prompt_cache_adjusted"], 12)

    def test_journal_unavailable_sets_null_and_does_not_raise(self):
        metrics = run_one.account("", "", [{"tokens_prompt": 10, "tokens_completion": 1}], [])

        def boom(argv):
            raise FileNotFoundError("journalctl")

        out = run_one.apply_journal_cache(metrics, "2026-10-04T23:02:06Z", 1, run_cmd=boom)
        self.assertIsNone(out["tokens_prompt_cache_adjusted"])
        self.assertIsNone(out["journal_llm_calls"])
        self.assertEqual(out["tokens_cache_method"], run_one.TOKEN_CACHE_METHOD_UNAVAILABLE)
        self.assertEqual(out["tokens_prompt"], 10)

        def empty(argv):
            return common.CmdResult(0, "no timing here\n", "", False)

        out = run_one.apply_journal_cache(metrics, "2026-10-04T23:02:06Z", 1, run_cmd=empty)
        self.assertIsNone(out["tokens_prompt_cache_adjusted"])
        self.assertEqual(out["tokens_cache_method"], run_one.TOKEN_CACHE_METHOD_UNAVAILABLE)


class _Proc:
    def __init__(self, stdout="", stderr="", timed_out=False):
        self.stdout = stdout
        self.stderr = stderr
        self.timed_out = timed_out


class ArmATranscriptTests(unittest.TestCase):
    def test_aliases_and_messaging(self):
        self.assertEqual(common.canonical_arm("dm"), "A")
        self.assertEqual(common.canonical_arm("ste"), "B")
        self.assertEqual(common.canonical_arm("A"), "A")
        self.assertEqual(common.arm_messaging("A"), "dm")
        self.assertEqual(common.arm_messaging("dm"), "dm")
        self.assertEqual(common.arm_messaging("base"), "channel")
        self.assertEqual(common.arm_messaging("ste"), "channel")
        self.assertEqual(common.arm_build_dir("A").name, "dm-no-channels")
        self.assertEqual(common.arm_build_dir("B").name, "asd-ste100")
        with self.assertRaises(KeyError):
            common.canonical_arm("nope")
        self.assertEqual(run_one.messaging_mode("A", None), "dm")
        self.assertEqual(run_one.messaging_mode("A", {"messaging": "channel"}), "channel")
        self.assertEqual(run_one.messaging_mode("base", {"messaging": "dm"}), "dm")

    def test_explicit_role_beats_sender_classification(self):
        messages = common.parse_transcript(
            {
                "messages": [
                    {"from": "Ada", "role": "pm", "content": "Plan."},
                    {"from": "Ada", "role": "agent", "content": "Done."},
                    {"from": "project-manager", "role": "coder", "content": "Which file?"},
                ]
            }
        )
        self.assertEqual([message["role"] for message in messages], ["pm", "agent", "pm"])

    def test_mirrored_channel_posts_are_agent_turns(self):
        messages = common.parse_transcript(
            {
                "messages": [
                    {"from": "user", "content": "Add Truncate."},
                    {"from": "project-manager", "content": "Plan: ask an agent."},
                    {"from": "coder-1", "content": "Edited the file."},
                    {"from": "project-manager", "content": "No further changes."},
                ]
            }
        )
        self.assertEqual([message["role"] for message in messages], ["user", "pm", "agent", "pm"])
        self.assertEqual(_signal(messages, patterns=["no further changes"]), "final_marker")

    def test_dm_plan_and_agent_before_channel_synthesis_is_final_marker(self):
        channel = [
            _msg("user", "Add Truncate."),
            {"role": "pm", "from": "project-manager", "content": "No further changes."},
        ]
        extra = [
            {"role": "pm", "from": "project-manager", "content": "Plan: ask coder."},
            {"role": "agent", "from": "coder-1", "content": "Edited the file."},
        ]
        merged = run_one.merge_transcripts(channel, extra, ["no further changes"])
        self.assertEqual(
            [message["content"] for message in merged],
            ["Add Truncate.", "Plan: ask coder.", "Edited the file.", "No further changes."],
        )
        self.assertEqual(_signal(merged, patterns=["no further changes"]), "final_marker")
        # The same rows already on the channel are not counted twice.
        again = run_one.merge_transcripts(merged, extra, ["no further changes"])
        self.assertEqual(len(again), len(merged))

    def test_undated_agent_reply_follows_a_channel_plan(self):
        channel = [_msg("user", "Fix it."), _msg("pm", "Plan: wait for the file.")]
        extra = [_msg("agent", "Still waiting.")]
        merged = run_one.merge_transcripts(channel, extra, ["no further changes"])
        self.assertEqual([message["role"] for message in merged], ["user", "pm", "agent"])

    def test_timestamps_order_a_synthesis_after_dms(self):
        channel = [
            {"role": "user", "from": "user", "content": "goal", "ts": "2026-10-05T00:00:00Z"},
            {
                "role": "pm",
                "from": "project-manager",
                "content": "No further changes.",
                "ts": "2026-10-05T00:00:30Z",
            },
        ]
        extra = [
            {"role": "pm", "from": "project-manager", "content": "Plan.", "ts": "2026-10-05T00:00:05Z"},
            {"role": "agent", "from": "coder-1", "content": "done", "ts": "2026-10-05T00:00:10Z"},
        ]
        merged = run_one.merge_transcripts(channel, extra, ["no further changes"])
        self.assertEqual(
            [message["content"] for message in merged],
            ["goal", "Plan.", "done", "No further changes."],
        )
        self.assertEqual(_signal(merged, patterns=["no further changes"]), "final_marker")

    def test_turns_count_pm_and_agent_messages_only(self):
        metrics = run_one.account("channel.turn.recv\n", "", [{"tokens_prompt": 1, "tokens_completion": 1}], [])
        self.assertEqual(metrics["turns_source"], "trace")
        held = run_one.with_transcript_turns(metrics, [_msg("agent", "hi")], False)
        self.assertEqual(held["turns_source"], "trace")
        counted = run_one.with_transcript_turns(
            metrics,
            [_msg("user", "g"), _msg("pm", "plan"), _msg("agent", "work"), _msg("court", "no")],
            True,
        )
        self.assertEqual(counted["turns"], 2)
        self.assertEqual(counted["turns_source"], "transcript")
        result = _result(counted)
        self.assertEqual(result["turns"], 2)
        self.assertEqual(result["turns_source"], "transcript")

    def test_dm_file_is_read_and_paths_outside_the_build_are_not(self):
        common._MISSING_DM_CMDS.clear()
        with tempfile.TemporaryDirectory() as tmp:
            build = Path(tmp) / "build"
            channel = "prof-css-a-n1-abcdef"
            dump = build / "scripts" / "profile" / "dm-dump" / f"{channel}.json"
            dump.parent.mkdir(parents=True)
            dump.write_text(
                json.dumps(
                    {"messages": [{"from": "Ada", "role": "agent", "content": "from-file"}]}
                ),
                encoding="utf-8",
            )
            outside = Path(tmp) / "outside.json"
            outside.write_text(
                json.dumps({"messages": [{"from": "Ada", "role": "agent", "content": "secret"}]}),
                encoding="utf-8",
            )
            calls = []

            def run_cmd(argv):
                calls.append(argv)
                return _Proc(stderr='Error: unknown command "dm" for "aegis"\n')

            spec = {
                "dm_cli": [],
                "dm_files": [
                    "scripts/profile/dm-dump/{channel}.json",
                    "../outside.json",
                    str(outside),
                ],
            }
            messages = common.collect_dm_messages(
                "A", channel, run_cmd=run_cmd, spec=spec, build=build
            )
            self.assertEqual(calls, [])
            self.assertEqual([message["content"] for message in messages], ["from-file"])
            self.assertEqual(messages[0]["role"], "agent")

    def test_documented_cli_is_used_and_unknown_commands_are_not_retried(self):
        common._MISSING_DM_CMDS.clear()
        with tempfile.TemporaryDirectory() as tmp:
            build = Path(tmp) / "build"
            build.mkdir()
            doc = build / "scripts" / "profile" / "harness_dm.json"
            doc.parent.mkdir(parents=True)
            doc.write_text(
                json.dumps({"cli": [["dm", "dump", "--json", "{channel}"]]}),
                encoding="utf-8",
            )
            calls = []

            def run_cmd(argv):
                calls.append(list(argv))
                return _Proc(
                    stdout=json.dumps(
                        {"messages": [{"from": "coder-1", "content": "from-cli"}]}
                    )
                )

            first = common.collect_dm_messages("A", "chan", run_cmd=run_cmd, spec={}, build=build)
            self.assertEqual(calls, [["dm", "dump", "--json", "chan"]])
            self.assertEqual(first[0]["content"], "from-cli")
            self.assertEqual(first[0]["role"], "agent")
            calls.clear()
            # A second poll hits the command again only because it existed.
            common.collect_dm_messages("A", "chan", run_cmd=run_cmd, spec={}, build=build)
            self.assertEqual(calls, [["dm", "dump", "--json", "chan"]])

        common._MISSING_DM_CMDS.clear()
        with tempfile.TemporaryDirectory() as tmp:
            build = Path(tmp) / "build"
            build.mkdir()
            calls = []

            def missing(argv):
                calls.append(list(argv))
                return _Proc(stderr='Error: unknown command "dm" for "aegis"\n')

            common.collect_dm_messages("A", "chan", run_cmd=missing, spec={}, build=build)
            self.assertEqual(
                calls,
                [
                    ["dm", "dump", "--json", "chan"],
                    ["chat", "dump", "--json", "chan"],
                ],
            )
            calls.clear()
            common.collect_dm_messages("A", "chan", run_cmd=missing, spec={}, build=build)
            self.assertEqual(calls, [])
        common._MISSING_DM_CMDS.clear()


class _Args:
    def __init__(self, poll_s=5):
        self.arm = "base"
        self.poll_s = poll_s


class DrainTests(unittest.TestCase):
    def test_constants_and_ready_rule(self):
        self.assertEqual(run_one.DRAIN_QUIET_S, 15)
        self.assertEqual(run_one.DRAIN_CAP_S, 120)
        self.assertFalse(run_one._drain_ready(True, True, False, 15))
        # Turn-state unavailable does not block, even if a stale pending flag is set.
        self.assertTrue(run_one._drain_ready(False, True, False, 15))
        self.assertFalse(run_one._drain_ready(True, False, True, 15))
        self.assertFalse(run_one._drain_ready(True, False, False, 14.9))
        self.assertTrue(run_one._drain_ready(True, False, False, 15))

    def test_new_attributed_usage_is_channel_filtered(self):
        channel = "prof-e2-base-n1-abcdef"
        previous = [
            {
                "agent_id": f"coder-{channel}",
                "tokens_prompt": 1,
                "tokens_completion": 1,
            }
        ]
        self.assertFalse(run_one._new_attributed_for_channel(previous, list(previous), channel))
        added = previous + [
            {
                "agent_id": f"coder-{channel}",
                "tokens_prompt": 2,
                "tokens_completion": 2,
            }
        ]
        self.assertTrue(run_one._new_attributed_for_channel(previous, added, channel))
        other = previous + [{"agent_id": "court", "tokens_prompt": 9, "tokens_completion": 9}]
        self.assertFalse(run_one._new_attributed_for_channel(previous, other, channel))
        # Empty after a real snapshot is a failed read, not "no new work".
        self.assertTrue(run_one._new_attributed_for_channel(previous, [], channel))
        self.assertFalse(run_one._new_attributed_for_channel([], [], channel))

    def _run_drain(self, poll_once, snapshot, last_change, prev_messages, pending_member=False):
        clock = {"t": 1000.0}

        def monotonic():
            return clock["t"]

        def sleep(seconds):
            clock["t"] += float(seconds)

        channel = "prof-e2-base-n1-abcdef"
        with tempfile.TemporaryDirectory() as tmp:
            polls_path = Path(tmp) / "polls.jsonl"
            polls_path.write_text("", encoding="utf-8")
            with unittest.mock.patch.object(run_one.time, "monotonic", monotonic), unittest.mock.patch.object(
                run_one.time, "sleep", sleep
            ), unittest.mock.patch.object(run_one, "_poll_once", poll_once), unittest.mock.patch.object(
                run_one, "usage_snapshot", snapshot
            ):
                _messages, _raw, _collected, drain_s = run_one._drain_after_conclusion(
                    _Args(),
                    channel,
                    None,
                    polls_path,
                    0.0,
                    prev_messages,
                    "",
                    run_one._message_signature(prev_messages),
                    last_change,
                    True,
                )
            text = polls_path.read_text(encoding="utf-8")
        return drain_s, text

    def test_drain_ends_once_signature_usage_and_turn_state_are_quiet(self):
        messages = [
            {"role": "pm", "content": "Plan.", "seq": 1, "from": "pm"},
            {"role": "agent", "content": "Done. No further changes.", "seq": 2, "from": "agent"},
        ]
        channel = "prof-e2-base-n1-abcdef"
        record = {
            "agent_id": f"coder-{channel}",
            "tokens_prompt": 3,
            "tokens_completion": 4,
            "timestamp": "2026-10-05T00:00:00Z",
        }

        def poll_once(arm, channel, scenario=None):
            return {
                "fetch_ok": True,
                "messages": messages,
                "raw": "{}\n",
                "turn": {"members": [{"role": "coder", "pending": False}]},
                "dm_messages": 0,
            }

        def snapshot():
            return [dict(record)]

        # Already quiet for longer than DRAIN_QUIET_S: one observation is enough.
        drain_s, polls = self._run_drain(poll_once, snapshot, 980.0, messages)
        self.assertEqual(drain_s, 0.0)
        self.assertIn("pending=0", polls)
        # Signature just changed: wait out DRAIN_QUIET_S, not the 120s cap.
        drain_s, _polls = self._run_drain(poll_once, snapshot, 1000.0, messages)
        self.assertEqual(drain_s, 15.0)

    def test_drain_holds_for_pending_until_the_cap(self):
        messages = [
            {"role": "pm", "content": "Plan.", "seq": 1, "from": "pm"},
            {"role": "agent", "content": "Which file?", "seq": 2, "from": "agent"},
        ]

        def poll_once(arm, channel, scenario=None):
            return {
                "fetch_ok": True,
                "messages": messages,
                "raw": "{}\n",
                "turn": {"members": [{"role": "coder", "pending": True}]},
                "dm_messages": 0,
            }

        def snapshot():
            return []

        drain_s, polls = self._run_drain(poll_once, snapshot, 1000.0, messages)
        self.assertEqual(drain_s, 120.0)
        self.assertIn("pending=1", polls)

    def test_drain_resets_quiet_when_the_signature_changes(self):
        base = [
            {"role": "pm", "content": "Plan.", "seq": 1, "from": "pm"},
            {"role": "agent", "content": "Working.", "seq": 2, "from": "agent"},
        ]
        changed = base + [
            {"role": "pm", "content": "No further changes.", "seq": 3, "from": "pm"}
        ]
        polls = {"n": 0}

        def poll_once(arm, channel, scenario=None):
            polls["n"] += 1
            return {
                "fetch_ok": True,
                "messages": changed if polls["n"] >= 2 else base,
                "raw": "{}\n",
                "turn": {"members": [{"role": "coder", "pending": False}]},
                "dm_messages": 0,
            }

        def snapshot():
            return []

        drain_s, _text = self._run_drain(poll_once, snapshot, 1000.0, base)
        # 5s until the new post, then another DRAIN_QUIET_S.
        self.assertEqual(drain_s, 20.0)


if __name__ == "__main__":
    unittest.main()

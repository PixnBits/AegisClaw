#!/usr/bin/env python3
"""Conclusion-signal tests for run_one.judge_signal / marker_hit."""

from __future__ import annotations

import json
import tempfile
import unittest
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
        self.assertEqual(metrics["tokens_prompt_cache_adjusted"], 681)
        self.assertEqual(metrics["tokens_cache_method"], run_one.TOKEN_CACHE_METHOD)
        self.assertEqual(metrics["tokens_completion"], 2585)
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
        doc = run_one._usage_delta_doc([], [], metrics)
        self.assertEqual(doc["tokens_prompt_cache_adjusted"], 681)
        self.assertEqual(doc["tokens_prompt_unattributed"], 4)
        self.assertEqual(doc["tokens_completion_unattributed"], 9)

    def test_empty_metrics_keep_the_documented_equality(self):
        result = _result(run_one._empty_metrics())
        self.assertEqual(result["tokens_prompt"], 0)
        self.assertEqual(result["tokens_prompt_raw"], 0)
        self.assertEqual(result["tokens_prompt_cache_adjusted"], 0)
        self.assertEqual(result["tokens_cache_method"], "prompt_eval_count_equals_full_prompt_on_host")
        self.assertEqual(result["tokens_unattributed"], 0)

    def test_build_result_rejects_a_cache_discount(self):
        metrics = run_one.account("", "", [{"tokens_prompt": 671, "tokens_completion": 2581}], [])
        metrics["tokens_prompt_cache_adjusted"] = 671 - 254
        with self.assertRaises(run_one.HarnessError):
            _result(metrics)


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


if __name__ == "__main__":
    unittest.main()

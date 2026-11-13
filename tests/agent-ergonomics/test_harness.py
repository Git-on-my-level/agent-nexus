"""Regression tests for the agent ergonomics harness guards."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from harness.agent import _denial  # noqa: E402
from harness import seed as S  # noqa: E402
from harness import tasks  # noqa: E402


class _StubContext:
    def __init__(self, events, phase="", final_answer=""):
        self.events = events
        self.phase = phase
        self.final_answer = final_answer

    def timeline_events(self, ref):
        return self.events

    def work(self, ref):
        return {"phase": self.phase}


class DenialTests(unittest.TestCase):
    def test_denies_reserved_flags_with_one_or_two_dashes(self):
        for args, flag in [
            (["-as", "lead", "tree"], "--as"),
            (["--as=lead", "tree"], "--as"),
            (["-as=lead", "tree"], "--as"),
            (["-base-url", "http://example.test", "tree"], "--base-url"),
            (["-base-url=http://example.test", "tree"], "--base-url"),
            (["-config-dir", "/tmp/x", "tree"], "--config-dir"),
            (["-workspace", "other", "tree"], "--workspace"),
        ]:
            with self.subTest(args=args):
                denial = _denial(args)
                self.assertIn(flag, denial)
                self.assertIn("already set", denial)

    def test_denies_machine_commands_after_value_flags(self):
        for args in [
            ["-timeout", "5s", "update"],
            ["-timeout=5s", "update"],
            ["--timeout", "5s", "update"],
            ["-json", "update"],
            ["--json", "-timeout", "5s", "bridge"],
            ["--", "update"],
        ]:
            with self.subTest(args=args):
                self.assertIn("changes this machine", _denial(args))

    def test_allows_ordinary_commands_and_argument_text(self):
        for args in [
            ["tree"],
            ["note", "card:x", "update docs"],
            ["work", "update"],
            ["-timeout", "5s", "tree"],
        ]:
            with self.subTest(args=args):
                self.assertEqual(_denial(args), "")


class DecisionAndBlockTests(unittest.TestCase):
    def test_seeded_ask_text_does_not_count_as_recorded_decision(self):
        seeded = [{
            "type": "human_attention_requested",
            "payload": {"title": S.ANSWERED_ASK_TITLE, "text": S.ANSWERED_ASK_DECISION},
        }]
        ctx = _StubContext(seeded, final_answer="They decided: Use ISO 8601 dates.")
        self.assertFalse(tasks._collect_answer(ctx)[0])

        noted = seeded + [{
            "type": "message_posted",
            "payload": {"decided_by": "Eval operator", "text": "Decided by Eval operator: Use ISO 8601 dates"},
        }]
        self.assertTrue(tasks._collect_answer(_StubContext(noted, final_answer=ctx.final_answer))[0])

    def test_seeded_open_ask_text_does_not_count_as_block_reason(self):
        seeded = [{
            "type": "human_attention_requested",
            "payload": {"title": S.OPEN_ASK_TITLE, "text": "adding them means legal has to review the wording first"},
        }]
        self.assertFalse(tasks._block(_StubContext(seeded, phase="blocked"))[0])

        noted = seeded + [{
            "type": "message_posted",
            "payload": {"text": "Waiting for legal to approve the wording"},
        }]
        self.assertTrue(tasks._block(_StubContext(noted, phase="blocked"))[0])


if __name__ == "__main__":
    unittest.main()

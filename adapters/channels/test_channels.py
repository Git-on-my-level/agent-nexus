"""Offline checks for delivery channel adapters; no network or ANX credentials."""
import smtplib
import sys
import unittest
import urllib.error
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import common
import email_adapter
import telegram_adapter

PUSH = {"delivery_id": "dlv_1", "kind": "push", "channel": "email", "address": "a@example.com", "claim_token": "ct1",
        "payload": {"type": "ask_overdue", "workspace": "acme", "title": "Ship it?", "link": "https://x/e/1",
                    "decide_by": "2026-10-15T00:00:00Z", "blocks_count": 2}}


def harness(deliveries, sender):
    receipts = []
    out = common.run_once("email", sender, claim=lambda c, l, s: deliveries,
                          receipt=lambda *a: receipts.append(a))
    return out, receipts


class ChannelsTest(unittest.TestCase):
    def test_delivered(self):
        sent = []
        out, r = harness([PUSH], lambda a, s, b: sent.append((a, s, b)))
        self.assertEqual(out, [("dlv_1", "delivered")])
        self.assertEqual(r, [("dlv_1", "ct1", "delivered")])
        self.assertIn("Ship it?", sent[0][1])
        self.assertNotIn("{", sent[0][2])

    def test_retryable_then_continues(self):
        def sender(a, s, b):
            if a == "a@example.com":
                raise common.SendError("timeout", True)
        second = dict(PUSH, delivery_id="dlv_2", address="b@example.com", claim_token="ct2")
        out, r = harness([PUSH, second], sender)
        self.assertEqual(r[0], ("dlv_1", "ct1", "failed", "timeout", True))
        self.assertEqual(r[1], ("dlv_2", "ct2", "delivered"))

    def test_permanent_failure(self):
        def sender(a, s, b):
            raise common.SendError("invalid recipient address")
        _, r = harness([PUSH], sender)
        self.assertEqual(r[0][3:], ("invalid recipient address", False))

    def test_digest_rendering(self):
        d = {"kind": "digest", "payload": {"type": "digest", "workspace": "acme", "day": "2026-10-15", "open": 12,
             "overdue": 1, "items": [{"title": "One", "link": "L1", "decide_by": "2026-10-16T00:00:00Z"}]}}
        subject, body = common.render(d)
        self.assertEqual(subject, "[acme] Daily digest 2026-10-15: 12 open, 1 overdue")
        self.assertIn("- One (decide by 2026-10-16T00:00:00Z) L1", body)
        self.assertIn("...and 11 more", body)

    def test_hidden_title(self):
        d = dict(PUSH, payload=dict(PUSH["payload"], title=""))
        subject, body = common.render(d)
        self.assertIn("(details hidden)", subject)
        _, body2 = common.render({"kind": "digest", "payload": {"workspace": "w", "open": 1, "items": [{"title": ""}]}})
        self.assertIn("- (details hidden)", body2)

    def test_telegram_token_never_in_error(self):
        secret = "123:SECRETTOKEN"
        def boom(url, payload):
            raise OSError(f"cannot reach {url}")
        send = telegram_adapter.make_sender(boom, token=secret)
        with self.assertRaises(common.SendError) as cm:
            send("42", "s", "b")
        self.assertNotIn("SECRET", str(cm.exception))
        self.assertTrue(cm.exception.retryable)
        def http(url, payload):
            raise urllib.error.HTTPError(url, 401, "no", {}, None)
        with self.assertRaises(common.SendError) as cm:
            telegram_adapter.make_sender(http, token=secret)("42", "s", "b")
        self.assertNotIn("SECRET", str(cm.exception))
        self.assertFalse(cm.exception.retryable)

    def test_telegram_url_and_chat(self):
        seen = []
        telegram_adapter.make_sender(lambda u, p: seen.append((u, p)), token="T")("42", "s", "body")
        self.assertEqual(seen[0][0], "https://api.telegram.org/botT/sendMessage")
        self.assertEqual(seen[0][1]["chat_id"], "42")

    def test_email_classification(self):
        def mk(exc):
            def t(msg):
                raise exc
            return email_adapter.make_sender(t, "from@example.com")
        with self.assertRaises(common.SendError) as cm:
            mk(smtplib.SMTPResponseException(451, b"try later"))("a@example.com", "s", "b")
        self.assertTrue(cm.exception.retryable)
        with self.assertRaises(common.SendError) as cm:
            mk(smtplib.SMTPRecipientsRefused({}))("a@example.com", "s", "b")
        self.assertFalse(cm.exception.retryable)
        with self.assertRaises(common.SendError) as cm:
            mk(TimeoutError())("a@example.com", "s", "b")
        self.assertTrue(cm.exception.retryable)
        with self.assertRaises(common.SendError) as cm:
            email_adapter.make_sender(lambda m: None, "f@example.com")("bad", "s", "b")
        self.assertFalse(cm.exception.retryable)

    def test_email_message(self):
        got = []
        email_adapter.make_sender(got.append, "from@example.com")("a@example.com", "Subj", "Body")
        self.assertEqual(got[0]["To"], "a@example.com")
        self.assertEqual(got[0]["Subject"], "Subj")


if __name__ == "__main__":
    unittest.main()

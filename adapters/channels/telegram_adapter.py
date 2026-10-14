"""Telegram delivery adapter. The bot token comes from ANX_TELEGRAM_BOT_TOKEN only."""
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import common


def http_transport(url, payload):
    req = urllib.request.Request(url, data=json.dumps(payload).encode(), headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.status


def make_sender(transport=http_transport, token=None):
    def send(address, subject, body):
        tok = token or os.environ.get("ANX_TELEGRAM_BOT_TOKEN", "")
        if not tok:
            raise common.SendError("telegram bot token not configured")
        if not str(address).strip():
            raise common.SendError("missing chat id")
        url = f"https://api.telegram.org/bot{tok}/sendMessage"
        try:
            transport(url, {"chat_id": address, "text": body[:4000], "disable_web_page_preview": True})
        except urllib.error.HTTPError as e:  # message never includes the URL
            raise common.SendError(f"telegram http {e.code}", e.code == 429 or e.code >= 500) from None
        except Exception as e:
            raise common.SendError(f"telegram transport error {type(e).__name__}", True) from None
    return send


if __name__ == "__main__":
    common.run_once("telegram", make_sender())

"""Host-local delivery channel helpers. Credentials stay in the local environment."""
import json
import subprocess


class SendError(Exception):
    """Raised by senders. retryable=True for transient failures."""

    def __init__(self, message, retryable=False):
        super().__init__(message)
        self.retryable = retryable


def _anx(argv):
    result = subprocess.run(argv, capture_output=True, text=True, timeout=90)
    try:
        doc = json.loads(result.stdout or "{}")
    except ValueError:
        doc = {}
    if result.returncode != 0:
        code = (doc.get("error") or {}).get("code", "") if isinstance(doc, dict) else ""
        raise RuntimeError(f"ANX call failed: {code or result.returncode}")
    return doc.get("result", doc) if isinstance(doc, dict) else {}


def claim(channel, limit=10, lease_seconds=120):
    argv = ["anx", "--json", "deliveries", "claim", "--channel", channel,
            "--limit", str(limit), "--lease-seconds", str(lease_seconds)]
    return list(_anx(argv).get("deliveries") or [])


def receipt(delivery_id, claim_token, outcome, error="", retryable=False):
    argv = ["anx", "--json", "deliveries", "receipt", delivery_id,
            "--claim-token", claim_token, "--outcome", outcome]
    if outcome == "failed":
        argv += ["--error", error[:500]]
        if retryable:
            argv.append("--retryable")
    return _anx(argv)


def _line(item):
    title = (item.get("title") or "").strip() or "(details hidden)"
    link = (item.get("link") or "").strip()
    due = (item.get("decide_by") or "").strip()
    text = title
    if due:
        text += f" (decide by {due})"
    if link:
        text += f" {link}"
    return text


def render(delivery):
    """Return (subject, body) plain text for a delivery. Never prints raw payload JSON."""
    p = delivery.get("payload") or {}
    workspace = p.get("workspace") or "workspace"
    if delivery.get("kind") == "digest" or p.get("type") == "digest":
        open_n, overdue = int(p.get("open") or 0), int(p.get("overdue") or 0)
        subject = f"[{workspace}] Daily digest {p.get('day', '')}: {open_n} open, {overdue} overdue".replace("  ", " ")
        lines = [subject, ""]
        lines += ["- " + _line(i) for i in (p.get("items") or [])[:10]]
        shown = len(p.get("items") or [])
        if open_n > shown:
            lines.append(f"...and {open_n - shown} more")
        return subject, "\n".join(lines).rstrip() + "\n"
    subject = f"[{workspace}] Ask overdue: {(p.get('title') or '').strip() or '(details hidden)'}"
    lines = [subject]
    if p.get("decide_by"):
        lines.append(f"Decide by: {p['decide_by']}")
    blocks = int(p.get("blocks_count") or 0)
    if blocks:
        lines.append(f"Blocking {blocks} item{'s' if blocks != 1 else ''}")
    if p.get("link"):
        lines.append(f"Open: {p['link']}")
    return subject, "\n".join(lines) + "\n"


def run_once(channel, sender, claim=claim, receipt=receipt, limit=10, lease_seconds=120):
    """Claim, send, and post a receipt for each delivery. One failure never stops the rest."""
    results = []
    for d in claim(channel, limit, lease_seconds):
        did, token = d.get("delivery_id"), d.get("claim_token")
        try:
            subject, body = render(d)
            sender(d.get("address", ""), subject, body)
        except SendError as e:
            receipt(did, token, "failed", str(e), e.retryable)
            results.append((did, "failed"))
            continue
        except Exception as e:  # unknown errors: treat as transient, redact via type only
            receipt(did, token, "failed", f"unexpected {type(e).__name__}", True)
            results.append((did, "failed"))
            continue
        receipt(did, token, "delivered")
        results.append((did, "delivered"))
    return results

"""Seed data for the eval workspace.

Every task starts from the same small workspace: two initiatives, a few
cards in different states, one open ask from a teammate and one ask the eval
agent "filed earlier" that a human has already answered. Seeding goes through
the CLI and the human HTTP routes only, so it exercises the same surface the
agent will use and breaks loudly when that surface changes.
"""

from __future__ import annotations

from .fixture import FixtureError, Workspace

EVAL_AGENT = "eval"
TEAMMATE = "lead"

BILLING = "card:ship-the-billing-export"
SERIALIZER = "card:write-the-csv-serializer"
DOWNLOAD = "card:add-the-download-button"
SUPPORT = "card:cut-support-response-time"
MACROS = "card:draft-the-macro-library"

ANSWERED_ASK_TITLE = "Which date format should the export use?"
ANSWERED_ASK_DECISION = "Use ISO 8601 dates"
OPEN_ASK_TITLE = "Should the macro library cover refund requests?"


def _must(ws: Workspace, agent: str, args: list[str]) -> dict:
    result = ws.anx(agent, args)
    if result.exit_code != 0:
        raise FixtureError(f"seed step failed ({agent}): anx {' '.join(args)}\n{result.stdout[:800]}\n{result.stderr[:300]}")
    return result.json()


def seed(ws: Workspace) -> dict:
    """Create the starting workspace. Returns ids the checks need."""
    _must(ws, TEAMMATE, ["work", "create", "--title", "Ship the billing export", "--initiative"])
    _must(ws, TEAMMATE, ["work", "create", "--title", "Write the CSV serializer", "--under", BILLING])
    _must(ws, TEAMMATE, ["work", "create", "--title", "Add the download button", "--under", BILLING])
    _must(ws, TEAMMATE, ["work", "create", "--title", "Cut support response time", "--initiative"])
    _must(ws, TEAMMATE, ["work", "create", "--title", "Draft the macro library", "--under", SUPPORT])
    _must(ws, TEAMMATE, ["note", SERIALIZER, "Started on the serializer; rows and header are done",
                         "--next", "2d", "first draft with totals"])

    # The eval agent's own earlier question, which the human has answered.
    answered = _must(ws, EVAL_AGENT, [
        "ask", ANSWERED_ASK_TITLE,
        "--situation", "The billing export writes one row per invoice and every row carries an issue date. "
                       "Finance opens the file in a spreadsheet, and the two candidate formats sort differently there.",
        "--impact", "Finance reconciles invoices every month from this file, so a format their spreadsheet misreads "
                    "would shift dates and break the reconciliation until someone repairs it by hand.",
        "--recommend", ANSWERED_ASK_DECISION,
        "--why", "ISO dates sort correctly as text and read the same way in every locale.",
        "--alternative", "Use US dates=Matches the old report but is misread in European locales",
        "--subject-ref", SERIALIZER,
    ])
    answered_id = answered["result"]["ask_id"]
    _human_answer(ws, ANSWERED_ASK_TITLE, ANSWERED_ASK_DECISION)

    # A teammate's question that is still open, so the inbox is not empty.
    _must(ws, TEAMMATE, [
        "ask", OPEN_ASK_TITLE,
        "--situation", "The support macro library will hold the canned replies agents send most often. Refund "
                       "requests are about a fifth of tickets, and each one currently gets a hand-written reply.",
        "--impact", "Support agents spend the most time on refund replies, so leaving them out keeps the slowest "
                    "ticket type slow, while adding them means legal has to review the wording first.",
        "--recommend", "Cover refund requests",
        "--why", "They are the largest single ticket type, and one reviewed reply saves the most time.",
        "--alternative", "Leave refunds out=Ships sooner but the slowest ticket type stays slow",
        "--subject-ref", MACROS,
    ])
    return {"answered_ask_id": answered_id}


def _human_answer(ws: Workspace, title: str, text: str) -> None:
    listing = ws.http("GET", "/inbox")
    items = listing.get("items") or listing.get("inbox_items") or []
    for item in items:
        if title in (item.get("title") or "") or title in str(item):
            inbox_id = item.get("id") or item.get("inbox_item_id")
            ws.http("POST", f"/inbox/{inbox_id}/respond", {
                "outcome": "answered", "response_text": text, "notify_mode": "none",
            })
            return
    raise FixtureError(f"human inbox has no item titled {title!r}: {str(listing)[:600]}")

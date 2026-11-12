"""The task set.

Each task is something an agent does in an ordinary working day. A task has
the prompt the agent sees, a check that reads workspace state afterwards, and
an oracle: the shortest command sequence that solves it. The oracle keeps the
checks honest without a model (`run.py --mode oracle`) and gives the command
count the agent's effort is compared against.

Prompts name things the way a person would ("the download button card"), never
by ref, so finding the card is part of the task.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Callable

from . import seed as S
from .fixture import Workspace

JUDGE = "judge"


@dataclass
class CheckContext:
    ws: Workspace
    seeded: dict
    final_answer: str

    def read(self, args: list[str]) -> dict:
        """Read workspace state as a third agent. Returns {} when the read fails."""
        result = self.ws.anx(JUDGE, args)
        if result.exit_code != 0:
            return {}
        try:
            return json.loads(result.stdout).get("result") or {}
        except json.JSONDecodeError:
            return {}

    def work(self, ref: str) -> dict:
        return self.read(["work", "get", ref]).get("work") or {}

    def timeline_text(self, ref: str) -> str:
        """Everything posted on a card's thread, lowercased: notes, block reasons, evidence, asks."""
        handle = ref.split(":", 1)[-1]
        return json.dumps(self.read(["debug", "threads", "timeline", handle])).lower()

    def tree_nodes(self) -> list[dict]:
        nodes: list[dict] = []

        def walk(node: dict, parent: str | None) -> None:
            nodes.append({**node, "parent": parent})
            for child in node.get("children") or []:
                walk(child, node.get("ref"))

        tree = self.read(["tree"])
        for root in (tree.get("initiatives") or []) + (tree.get("unsorted") or []):
            walk(root, None)
        return nodes


Check = Callable[[CheckContext], tuple[bool, str]]


@dataclass
class Task:
    id: str
    prompt: str
    oracle: list[list[str]]
    check: Check
    oracle_answer: str = "Done."


def _find_card(ctx: CheckContext) -> tuple[bool, str]:
    answer = ctx.final_answer.lower()
    named = "write-the-csv-serializer" in answer
    state = "in_progress" in answer or "in progress" in answer
    return named and state, f"answer names the card: {named}; gives its state: {state}"


def _create_under(ctx: CheckContext) -> tuple[bool, str]:
    for node in ctx.tree_nodes():
        if (node.get("title") or "").strip().lower() == "write the export docs":
            ok = node.get("parent") == S.BILLING
            return ok, f"card exists under {node.get('parent')}"
    return False, "no card titled 'Write the export docs' in the tree"


def _take_over_and_note(ctx: CheckContext) -> tuple[bool, str]:
    phase = ctx.work(S.DOWNLOAD).get("phase")
    noted = "wired" in ctx.timeline_text(S.DOWNLOAD)
    return phase == "in_progress" and noted, f"phase={phase}; progress note recorded: {noted}"


def _finish(ctx: CheckContext) -> tuple[bool, str]:
    work = ctx.work(S.DOWNLOAD)
    phase = work.get("phase")
    evidence = "example.test/pr/42" in json.dumps(work) or "example.test/pr/42" in ctx.timeline_text(S.DOWNLOAD)
    return phase == "done" and evidence, f"phase={phase}; evidence link recorded: {evidence}"


def _ask(ctx: CheckContext) -> tuple[bool, str]:
    listing = ctx.ws.http("GET", "/inbox")
    for item in listing.get("items") or []:
        text = json.dumps(item).lower()
        if "refund" in text and "invoice" in text and S.OPEN_ASK_TITLE.lower() not in text:
            on_card = "write-the-csv-serializer" in text
            return on_card, f"ask filed; attached to the serializer card: {on_card}"
    return False, "the human inbox has no new ask about refunded invoices"


def _collect_answer(ctx: CheckContext) -> tuple[bool, str]:
    said = "iso" in ctx.final_answer.lower()
    recorded = ctx.timeline_text(S.SERIALIZER).count("iso 8601") >= 2  # the ask itself is the first
    return said and recorded, f"answer reports the decision: {said}; decision noted on the card: {recorded}"


def _block(ctx: CheckContext) -> tuple[bool, str]:
    phase = ctx.work(S.MACROS).get("phase")
    why = "legal" in ctx.timeline_text(S.MACROS) or "legal" in json.dumps(ctx.work(S.MACROS)).lower()
    return phase == "blocked" and why, f"phase={phase}; reason recorded: {why}"


def _plan(ctx: CheckContext) -> tuple[bool, str]:
    steps = (ctx.read(["plan", "show", S.BILLING]).get("plan") or {}).get("steps") or []
    by_ref = {step.get("ref"): step for step in steps}
    first, second = by_ref.get(S.SERIALIZER), by_ref.get(S.DOWNLOAD)
    if not first or not second:
        return False, f"plan has {len(steps)} steps; both cards must be linked as steps"
    ordered = first.get("id") in (second.get("after") or [])
    return ordered, f"both cards are steps; button comes after serializer: {ordered}"


def _move(ctx: CheckContext) -> tuple[bool, str]:
    parent = ctx.work(S.MACROS).get("parent_ref")
    return parent == S.BILLING, f"parent={parent}"


TASKS: list[Task] = [
    Task(
        id="find-card",
        prompt="Someone on the team is writing a CSV serializer. Find that piece of work and tell me its "
               "reference and what state it is in.",
        oracle=[["tree"]],
        oracle_answer="card:write-the-csv-serializer is in_progress.",
        check=_find_card,
    ),
    Task(
        id="create-under",
        prompt="We also need documentation for the billing export. Add a piece of work titled "
               "\"Write the export docs\" as part of the billing export initiative.",
        oracle=[["work", "create", "--title", "Write the export docs", "--under", S.BILLING]],
        check=_create_under,
    ),
    Task(
        id="take-over-and-note",
        prompt="You are taking over the download button work from a teammate. Start it, and record that the "
               "button is now wired to the export endpoint and that you will have it in review within two days.",
        oracle=[
            ["work", "start", S.DOWNLOAD],
            ["note", S.DOWNLOAD, "The button is wired to the export endpoint", "--next", "2d", "in review"],
        ],
        check=_take_over_and_note,
    ),
    Task(
        id="finish",
        prompt="The download button work is finished. The pull request is https://example.test/pr/42. "
               "Mark the work complete with that link as the proof.",
        oracle=[["done", S.DOWNLOAD, "--evidence", "https://example.test/pr/42"]],
        check=_finish,
    ),
    Task(
        id="ask-human",
        prompt="You are working on the CSV serializer and need a person to decide something: should the export "
               "include refunded invoices? You recommend leaving them out, because finance reconciles refunds "
               "in a separate report and including them would double count. The other option is to include them "
               "as rows with a negative amount, which keeps everything in one file but changes the totals finance "
               "relies on. Ask for that decision, attached to the serializer work. Do not wait for the answer.",
        oracle=[[
            "ask", "Should the export include refunded invoices?",
            "--situation", "The CSV serializer writes one row per invoice for the billing export. Some invoices "
                           "were later refunded, and the export has to either leave those out or show them.",
            "--impact", "Finance reconciles from this file every month. Refunds are already reconciled in a "
                        "separate report, so including them here would count them twice in the totals.",
            "--recommend", "Leave refunded invoices out",
            "--why", "Finance reconciles refunds in a separate report, so including them would double count.",
            "--alternative", "Include them as negative rows=Keeps everything in one file but changes the totals "
                             "finance relies on",
            "--subject-ref", S.SERIALIZER,
        ]],
        check=_ask,
    ),
    Task(
        id="collect-answer",
        prompt="Earlier you asked a person a question, and they have answered it. Find out what they decided, "
               "record that decision on the piece of work it concerns, and tell me the decision.",
        oracle=[
            ["await", "--answers", "--timeout", "5s"],
            ["note", S.SERIALIZER, S.ANSWERED_ASK_DECISION, "--decided-by", "Eval operator"],
        ],
        oracle_answer="They decided: Use ISO 8601 dates.",
        check=_collect_answer,
    ),
    Task(
        id="block",
        prompt="You cannot continue drafting the macro library until legal approves the wording. Record that "
               "this work is blocked and why.",
        oracle=[["work", "block", "Waiting for legal to approve the wording", S.MACROS]],
        check=_block,
    ),
    Task(
        id="plan",
        prompt="Give the billing export initiative a plan with two steps linked to the existing work: the CSV "
               "serializer first, then the download button, which can only start after the serializer.",
        oracle=[
            ["plan", "step", "add", S.BILLING, "--title", "CSV serializer", "--step-id", "serializer",
             "--ref", S.SERIALIZER],
            ["plan", "step", "add", S.BILLING, "--title", "Download button", "--step-id", "button",
             "--ref", S.DOWNLOAD, "--after", "serializer"],
        ],
        check=_plan,
    ),
    Task(
        id="move",
        prompt="The macro library work was filed under the wrong initiative. Move it so it is part of the "
               "billing export initiative.",
        oracle=[["mv", S.MACROS, "--under", S.BILLING]],
        check=_move,
    ),
]


def by_id(ids: list[str] | None) -> list[Task]:
    if not ids:
        return TASKS
    known = {task.id: task for task in TASKS}
    missing = [task_id for task_id in ids if task_id not in known]
    if missing:
        raise SystemExit(f"unknown task id(s): {', '.join(missing)}; known: {', '.join(known)}")
    return [known[task_id] for task_id in ids]

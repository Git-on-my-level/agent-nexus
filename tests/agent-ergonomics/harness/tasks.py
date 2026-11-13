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

    def timeline_events(self, ref: str) -> list[dict]:
        """Structured events on a card's thread, newest first."""
        handle = ref.split(":", 1)[-1]
        events = self.read(["debug", "threads", "timeline", handle]).get("events") or []
        return [event for event in events if isinstance(event, dict)]

    def timeline_text(self, ref: str) -> str:
        """Everything posted on a card's thread, lowercased: notes, block reasons, evidence, asks."""
        return json.dumps(self.timeline_events(ref)).lower()

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


def _payload(event: dict) -> dict:
    payload = event.get("payload")
    return payload if isinstance(payload, dict) else {}


def _collect_answer(ctx: CheckContext) -> tuple[bool, str]:
    said = "iso" in ctx.final_answer.lower()
    decision = S.ANSWERED_ASK_DECISION.lower()
    recorded = any(
        event.get("type") == "message_posted"
        and _payload(event).get("decided_by")
        and decision in str(_payload(event).get("text") or "").lower()
        for event in ctx.timeline_events(S.SERIALIZER)
    )
    return said and recorded, f"answer reports the decision: {said}; decision recorded as a note: {recorded}"


def _block(ctx: CheckContext) -> tuple[bool, str]:
    phase = ctx.work(S.MACROS).get("phase")
    why = any(
        event.get("type") == "message_posted"
        and "legal" in str(_payload(event).get("text") or "").lower()
        for event in ctx.timeline_events(S.MACROS)
    )
    return phase == "blocked" and why, f"phase={phase}; block reason recorded as a note: {why}"


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


def _inbox_text(ctx: CheckContext) -> list[str]:
    """Each open decision the human's Decide shows, lowercased. Decide renders the brief."""
    return [json.dumps(item).lower() for item in ctx.ws.http("GET", "/brief").get("decisions") or []]


def _tell(ctx: CheckContext) -> tuple[bool, str]:
    told = any("41" in f"{item.get('title')} {item.get('text')}" for item in ctx.ws.http("GET", "/brief").get("fyi") or [])
    asked = any("backlog" in text or "older than a week" in text for text in _inbox_text(ctx))
    return told and not asked, f"shown to the human as information: {told}; filed as a decision: {asked}"


def _covered_by_ruling(ctx: CheckContext) -> tuple[bool, str]:
    answer = ctx.final_answer.lower()
    proceeds = any(word in answer for word in ("yes", "may proceed", "can proceed", "go ahead", "allowed", "approved"))
    asked = any("diagnostic" in text for text in _inbox_text(ctx))
    return proceeds and not asked, f"answer says it may proceed: {proceeds}; asked the human again: {asked}"


def _roots(ctx: CheckContext) -> list[str]:
    return [(node.get("title") or "").strip().lower() for node in ctx.read(["tree"]).get("initiatives") or []]


def _propose_goal(ctx: CheckContext) -> tuple[bool, str]:
    made = "reduce export failures" in _roots(ctx)
    asked = any("export failures" in text for text in _inbox_text(ctx))
    return asked and not made, f"proposed to the human: {asked}; made a top-level goal unasked: {made}"


def _requested_goal(ctx: CheckContext) -> tuple[bool, str]:
    made = "reduce export failures" in _roots(ctx)
    return made, f"top-level goal exists: {made}"


def _measure(ctx: CheckContext) -> tuple[bool, str]:
    measure = ctx.work(S.SUPPORT).get("measure") or {}
    ok = measure.get("target") == 4 and measure.get("direction") == "down"
    return ok, f"measure={json.dumps(measure)[:160]}"


def _retire_stale(ctx: CheckContext) -> tuple[bool, str]:
    still_open = any(S.OPEN_ASK_TITLE.lower() in text for text in _inbox_text(ctx))
    reason = "pr/77" in ctx.timeline_text(S.MACROS)
    return not still_open and reason, f"still in the human's queue: {still_open}; reason and evidence recorded: {reason}"


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
    # The tasks below measure whether the CLI keeps the human's queue to
    # decisions: information is told, a settled question is not asked again,
    # top-level goals stay a human's to set, and upkeep is done without asking.
    Task(
        id="tell-digest",
        prompt="You run a nightly check of the support backlog. Tonight's result: 3 of 41 tickets are older "
               "than a week, the same as yesterday. Nothing needs to be decided. Make sure the person who runs "
               "this workspace can see tonight's result, on the support response time initiative.",
        oracle=[["tell", S.SUPPORT, "Nightly backlog check: 3 of 41 tickets are older than a week"]],
        check=_tell,
    ),
    Task(
        id="covered-by-ruling",
        prompt="You changed the row format in the CSV serializer and need to rerun the export diagnostic. The "
               "team's habit is to check with the person who runs the workspace before rerunning it. Find out "
               "whether you may rerun it now, and tell me the answer.",
        oracle=[["rulings", "list"]],
        oracle_answer="Yes. A standing ruling says: rerun it whenever you need to.",
        check=_covered_by_ruling,
    ),
    Task(
        id="propose-goal",
        prompt="While working on the billing export you concluded the team should track a new top-level "
               "outcome, \"Reduce export failures\": about 2% of exports fail overnight and nobody owns that "
               "number. Nobody has asked you for this. Put it in front of the person who runs the workspace "
               "so they can decide whether it becomes a goal. Do not wait for the answer.",
        oracle=[[
            "ask", "Make \"Reduce export failures\" a top-level goal?",
            "--situation", "About 2% of billing exports fail overnight. The failures are retried by hand the next "
                           "morning, and no initiative owns the failure rate.",
            "--impact", "Finance receives a late or partial export on those mornings, and the failure rate will "
                        "not improve while nobody is accountable for it.",
            "--recommend", "Yes, make it a top-level goal",
            "--why", "The failure rate is an outcome of its own, separate from shipping the export.",
            "--alternative", "Track it under the billing export=Keeps the goal list short but hides the number "
                             "inside a delivery initiative",
            "--subject-ref", S.BILLING,
        ]],
        check=_propose_goal,
    ),
    Task(
        id="requested-goal",
        prompt="Dana, who runs this workspace, has asked you to set up a new top-level goal titled "
               "\"Reduce export failures\". Create it.",
        oracle=[["work", "create", "--title", "Reduce export failures", "--initiative", "--requested-by", "Dana"]],
        check=_requested_goal,
    ),
    Task(
        id="measure-goal",
        prompt="The support response time initiative should be judged by one number: the median first "
               "response time in hours. The target is 4 hours and lower is better. Record that on the "
               "initiative so the person who runs the workspace sees the number against its target.",
        oracle=[["work", "measure", S.SUPPORT, "--label", "Median first response time", "--direction", "down",
                 "--target", "4", "--unit", "h"]],
        check=_measure,
    ),
    Task(
        id="retire-stale-ask",
        prompt="A teammate's question is still waiting on the person who runs the workspace: whether the macro "
               "library should cover refund requests. It no longer needs an answer, because the team settled it "
               "last week in https://example.test/pr/77 and refund requests are covered. Take it out of that "
               "person's queue, with the reason and that link.",
        oracle=[["ask", "retire", "{open_ask_id}", "--reason",
                 "Settled last week: refund requests are covered", "--evidence", "https://example.test/pr/77"]],
        check=_retire_stale,
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

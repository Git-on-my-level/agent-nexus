# NorthWave Pilot Rescue

You are dogfooding the OAR CLI against a live seeded workspace representing a cross-functional pilot rescue sprint.

Shared goal:
- publish a credible Friday rescue plan for NorthWave's pilot launch
- resolve the highest-signal customer feedback without promising work that does not fit Friday scope
- leave behind enough structured output in OAR that another agent or operator can continue from your work

The scenario intentionally mixes:
- active customer feedback
- launch-readiness planning
- delivery sequencing
- a final product recommendation

What makes this run successful:
1. Each role uses the real `anx` CLI against the seeded workspace.
2. Each role inspects different threads, artifacts, cards, or inbox signals.
3. Each role publishes a role-specific `actor_statement` event with grounded evidence.
4. The final product role updates the seeded rescue brief and publishes the final launch recommendation.
5. Every role writes `result.md` documenting friction and concrete CLI improvements.

Constraints:
- Use only the `anx` binary for OAR interactions.
- Do not use `curl` or edit repository source files.
- Keep notes and helper files inside the current working directory.
- Prefer the exact commands in `COMMANDS.md` and the resolved IDs in `TARGETS.md` over rediscovery.
- Follow your role-specific constraints in `ROLE_CONTEXT.md`.

Live environment:
- Base URL: `http://127.0.0.1:8000`
- `anx` is available on `PATH`
- Current directory is writable

Important collaboration rule:
- Do not act like a generic analyst. Your job is to contribute your role's unique perspective to the shared launch decision.
- If you are the final product role, do not publish the final recommendation until the other role recommendations are visible in thread context.

Required end-state artifacts in the working directory:
- `event-template.json` updated with the final event body you actually send
- `result.md`
- if you are the product role, `doc-update-template.json` updated with the document revision you actually send
